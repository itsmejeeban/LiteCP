package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

var cloudflareFirewallMu sync.Mutex

type cloudflareNetworks struct {
	IPv4 []string `json:"ipv4"`
	IPv6 []string `json:"ipv6"`
}

func parseCloudflareNetworks(text string, ipv6 bool) ([]string, error) {
	values := []string{}
	seen := map[string]bool{}
	for _, line := range strings.Fields(text) {
		prefix, err := netip.ParsePrefix(line)
		if err != nil || prefix.Addr().Is6() != ipv6 || prefix.Bits() == 0 || !prefix.Addr().IsGlobalUnicast() || prefix.Addr().IsPrivate() || prefix.Addr().Is4In6() || prefix != prefix.Masked() {
			return nil, fmt.Errorf("invalid Cloudflare network list")
		}
		if !seen[prefix.String()] {
			values = append(values, prefix.String())
			seen[prefix.String()] = true
		}
	}
	if len(values) < 2 || len(values) > 256 {
		return nil, fmt.Errorf("Cloudflare network list is empty or unexpectedly sized")
	}
	return values, nil
}
func fetchCloudflareNetworks(ctx context.Context) (cloudflareNetworks, error) {
	var result cloudflareNetworks
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return fmt.Errorf("Cloudflare list redirects are not accepted")
	}}
	for i, url := range []string{"https://www.cloudflare.com/ips-v4/", "https://www.cloudflare.com/ips-v6/"} {
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return result, err
		}
		response, err := client.Do(req)
		if err != nil {
			return result, err
		}
		content, readErr := io.ReadAll(io.LimitReader(response.Body, 32769))
		response.Body.Close()
		if readErr != nil || response.StatusCode != 200 || len(content) > 32768 {
			return result, fmt.Errorf("Cloudflare IP list download failed")
		}
		values, err := parseCloudflareNetworks(string(content), i == 1)
		if err != nil {
			return result, err
		}
		if i == 0 {
			result.IPv4 = values
		} else {
			result.IPv6 = values
		}
	}
	return result, nil
}
func cloudflareGuard(rule FirewallRule, networks cloudflareNetworks) string {
	name := fmt.Sprintf("litecp_cf_%d_%s", rule.Port, rule.Protocol)
	return fmt.Sprintf("add table inet %s\ndelete table inet %s\ntable inet %s {\nset cf4 { type ipv4_addr; flags interval; elements = { %s }; }\nset cf6 { type ipv6_addr; flags interval; elements = { %s }; }\nchain input { type filter hook input priority -310; policy accept;\niifname lo accept\n%s dport %d ip saddr @cf4 accept\n%s dport %d ip6 saddr @cf6 accept\n%s dport %d drop\n}\n}\n", name, name, name, strings.Join(networks.IPv4, ", "), strings.Join(networks.IPv6, ", "), rule.Protocol, rule.Port, rule.Protocol, rule.Port, rule.Protocol, rule.Port)
}

// A separate early input chain drops direct connections before broad installer
// allow rules. Its persistent boot unit starts before network configuration.
func (s *System) applyCloudflareFirewall(ctx context.Context, rule FirewallRule, remove bool) error {
	cloudflareFirewallMu.Lock()
	defer cloudflareFirewallMu.Unlock()
	if s.demo {
		return nil
	}
	name := fmt.Sprintf("litecp_cf_%d_%s", rule.Port, rule.Protocol)
	if remove {
		script := `set -e
name=$1;port=$2;protocol=$3;backend=$4
if [[ $backend == firewalld ]]; then
 for family in ipv4 ipv6; do
  firewall-cmd --permanent --zone=public --remove-rich-rule="rule family=\"$family\" priority=\"-90\" port port=\"$port\" protocol=\"$protocol\" accept"
 done
 firewall-cmd --reload
else
 ufw --force delete allow "$port/$protocol" comment "$name"
fi
systemctl disable "$name.service"
printf 'add table inet %s\ndelete table inet %s\n' "$name" "$name" | nft -f -
echo 'Cloudflare-only restriction removed; other firewall rules remain unchanged.'
`
		_, err := runCommandInput(ctx, 45*time.Second, script, "systemd-run", "--quiet", "--wait", "--pipe", "--collect", "--service-type=exec", "--", "/bin/bash", "-s", "--", name, fmt.Sprint(rule.Port), rule.Protocol, s.firewallBackend())
		return err
	}
	output, probeErr := runHostCommand(ctx, 10*time.Second, "sshd", "-T")
	if probeErr != nil {
		return fmt.Errorf("cannot verify SSH ports before applying restriction: %w", probeErr)
	}
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == fmt.Sprintf("port %d", rule.Port) {
			return fmt.Errorf("Cloudflare-only access cannot restrict the SSH port")
		}
	}
	networks, err := fetchCloudflareNetworks(ctx)
	if err != nil {
		return fmt.Errorf("Cloudflare rules unchanged: %w", err)
	}
	return s.installCloudflareGuard(ctx, rule, networks)
}
func (s *System) installCloudflareGuard(ctx context.Context, rule FirewallRule, networks cloudflareNetworks) error {
	name := fmt.Sprintf("litecp_cf_%d_%s", rule.Port, rule.Protocol)
	// Pass the validated rules over stdin; no URL, user label or shell command is interpolated.
	script := `set -Eeuo pipefail
name=$1; port=$2; protocol=$3; backend=$4
command -v nft >/dev/null || { echo 'Install the nftables package before using Cloudflare-only access.' >&2; exit 1; }
install -d -m 0700 /etc/litecp-cloudflare
config=/etc/litecp-cloudflare/$name.nft
previous=''
if [[ -f $config ]]; then previous=$(mktemp /etc/litecp-cloudflare/previous.XXXXXX); cp -p "$config" "$previous"; fi
rollback() {
 status=$?; trap - ERR
 if [[ -n $previous ]]; then
  nft -f "$previous" && install -m 0600 "$previous" "$config" || echo 'Previous Cloudflare guard could not be restored; inspect nftables immediately.' >&2
 else
  systemctl disable "$name.service" 2>/dev/null || true
  # Keep the guard in place if removing its new broad allow fails.
  if [[ $backend == firewalld ]]; then
   for family in ipv4 ipv6; do firewall-cmd --permanent --zone=public --remove-rich-rule="rule family=\"$family\" priority=\"-90\" port port=\"$port\" protocol=\"$protocol\" accept" || true; done
   firewall-cmd --reload && printf 'add table inet %s\ndelete table inet %s\n' "$name" "$name" | nft -f -
  else
   ufw --force delete allow "$port/$protocol" comment "$name" && printf 'add table inet %s\ndelete table inet %s\n' "$name" "$name" | nft -f -
  fi
 fi
 exit "$status"
}
trap rollback ERR
candidate=$(mktemp /etc/litecp-cloudflare/candidate.XXXXXX)
cat > "$candidate"
nft -c -f "$candidate"
nft -f "$candidate"
install -m 0600 "$candidate" "$config"
# These accepts only open the selected port in the host firewall. The early
# nftables chain still rejects every non-Cloudflare source on that port.
if [[ $backend == firewalld ]]; then
 for family in ipv4 ipv6; do
  firewall-cmd --permanent --zone=public --add-rich-rule="rule family=\"$family\" priority=\"-90\" port port=\"$port\" protocol=\"$protocol\" accept"
 done
 firewall-cmd --reload
else
 grep -q '^IPV6=yes' /etc/default/ufw || { echo 'Enable IPv6 in UFW before using Cloudflare IPv4 + IPv6 access.' >&2; exit 1; }
 ufw allow "$port/$protocol" comment "$name"
fi
nft -f "$candidate"
nft_path=$(command -v nft)
cat > "/etc/systemd/system/$name.service" <<UNIT
[Unit]
Description=LiteCP Cloudflare-only port $port/$protocol
DefaultDependencies=no
After=local-fs.target
Before=network-pre.target
Wants=network-pre.target
[Service]
Type=oneshot
ExecStart=$nft_path -f $config
RemainAfterExit=yes
[Install]
WantedBy=network-pre.target
UNIT
systemctl daemon-reload
systemctl enable "$name.service"
`
	// Bash reads the script from -c; stdin is reserved for nftables content.
	_, err := runCommandInput(ctx, 90*time.Second, cloudflareGuard(rule, networks), "systemd-run", "--quiet", "--wait", "--pipe", "--collect", "--service-type=exec", "--", "/bin/bash", "-c", script, "litecp-cloudflare", name, fmt.Sprint(rule.Port), rule.Protocol, s.firewallBackend())
	if err != nil {
		return fmt.Errorf("Cloudflare restriction could not be fully configured; check firewall status before retrying: %w", err)
	}
	return nil
}
func (a *App) startCloudflareFirewallUpdates() {
	if a.system.demo {
		return
	}
	go func() {
		// Existing boot guards remain active during a failed refresh. Never replace
		// a working list with a partial, empty or invalid download.
		for {
			rules := a.store.PublicState().FirewallRules
			found := false
			for _, rule := range rules {
				if rule.Source == "cloudflare" {
					found = true
					break
				}
			}
			if found {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				cloudflareFirewallMu.Lock()
				networks, err := fetchCloudflareNetworks(ctx)
				if err == nil {
					for _, rule := range a.store.PublicState().FirewallRules {
						if rule.Source == "cloudflare" {
							name := fmt.Sprintf("litecp_cf_%d_%s.service", rule.Port, rule.Protocol)
							if _, activeErr := runCommand(ctx, 5*time.Second, "systemctl", "is-enabled", name); activeErr != nil {
								continue
							}
							if err = a.system.installCloudflareGuard(ctx, rule, networks); err != nil {
								break
							}
						}
					}
				}
				cloudflareFirewallMu.Unlock()
				cancel()
				if err != nil {
					a.store.AddActivity("Cloudflare firewall refresh failed", "Existing restrictions retained: "+err.Error(), "warning")
				}
			}
			time.Sleep(24 * time.Hour)
		}
	}()
}
