package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func parseSystemResolvers(content string) []string {
	values := []string{}
	seen := map[string]bool{}
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		fields := strings.Fields(strings.SplitN(scanner.Text(), "#", 2)[0])
		if len(fields) < 2 || fields[0] != "nameserver" {
			continue
		}
		ip := net.ParseIP(fields[1])
		if ip != nil && !seen[ip.String()] {
			values = append(values, ip.String())
			seen[ip.String()] = true
		}
	}
	return values
}

func currentSystemResolvers() []string {
	values, err := systemResolversFromFile(context.Background(), "/etc/resolv.conf")
	if err != nil {
		return []string{}
	}
	return values
}

func systemResolversFromFile(ctx context.Context, path string) ([]string, error) {
	content, err := os.ReadFile(path)
	if err != nil { return nil, err }
	values := parseSystemResolvers(string(content))
	result, seen := []string{}, map[string]bool{}
	var upstream []string
	for _, value := range values {
		actual := []string{value}
		if value == "127.0.0.53" || value == "127.0.0.54" {
			if upstream == nil { upstream, err = effectiveResolvedResolvers(ctx); if err != nil { return nil, err } }
			actual = upstream
		}
		for _, server := range actual { if !seen[server] { result = append(result, server); seen[server] = true } }
	}
	return result, nil
}

// resolvectl exposes live daemon state. Its all-known resolv.conf export does
// not describe which DNS scopes actually handle default/unmatched queries.
type resolvedDNSState struct { dns, domains map[string][]string; order []string }
var resolvedScopeLine = regexp.MustCompile(`^(Global|Link ([0-9]+) \([^)]*\)):\s*(.*)$`)

func parseResolvedScopes(content string) (map[string][]string, []string, error) {
	values, order := map[string][]string{}, []string{}
	scanner, current := bufio.NewScanner(strings.NewReader(content)), ""
	for scanner.Scan() {
		line := scanner.Text(); if strings.TrimSpace(line) == "" { continue }
		if match := resolvedScopeLine.FindStringSubmatch(strings.TrimSpace(line)); match != nil {
			current = "Global"; if match[2] != "" { current = "Link " + match[2] }
			if _, exists := values[current]; exists { return nil, nil, errors.New("duplicate resolver scope") }
			values[current] = strings.Fields(match[3]); order = append(order, current)
		} else if current != "" && (line[0] == ' ' || line[0] == '\t') {
			values[current] = append(values[current], strings.Fields(line)...)
		} else { return nil, nil, errors.New("unrecognized resolvectl output") }
	}
	if err := scanner.Err(); err != nil { return nil, nil, err }
	if len(order) == 0 { return nil, nil, errors.New("no resolver scopes reported") }
	return values, order, nil
}

func resolvedServerAddress(value string) (string, error) {
	address := strings.SplitN(value, "#", 2)[0] // DNS-over-TLS server name is not an address.
	zone := ""
	if index := strings.IndexByte(address, '%'); index >= 0 {
		end := strings.IndexByte(address[index:], ']')
		if end >= 0 { end += index; zone, address = address[index+1:end], address[:index]+address[end:] } else { zone, address = address[index+1:], address[:index] }
		if zone == "" { return "", errors.New("empty resolver zone") }
	}
	port := ""
	if host, service, err := net.SplitHostPort(address); err == nil { address, port = host, service }
	address = strings.TrimPrefix(strings.TrimSuffix(address, "]"), "[")
	ip := net.ParseIP(address); if ip == nil { return "", fmt.Errorf("invalid live DNS address %q", value) }
	address = ip.String(); if zone != "" && ip.IsLinkLocalUnicast() { address += "%"+zone }
	if port != "" { number, err := strconv.Atoi(port); if err != nil || number < 1 || number > 65535 { return "", errors.New("invalid live DNS port") }; if number != 53 { address = net.JoinHostPort(address, strconv.Itoa(number)) } }
	return address, nil
}

func resolvedCommand(ctx context.Context, action string) (string, error) {
	command := exec.CommandContext(ctx, "resolvectl", "--no-pager", action)
	command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C", "SYSTEMD_COLORS=0", "SYSTEMD_PAGER=cat", "COLUMNS=4096")
	output, err := command.Output(); if err != nil { return "", fmt.Errorf("read live resolver %s: %w", action, err) }; return string(output), nil
}

func readResolvedDNSState(ctx context.Context) (resolvedDNSState, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second); defer cancel()
	state := resolvedDNSState{}
	content, err := resolvedCommand(ctx, "dns"); if err != nil { return state, err }
	state.dns, state.order, err = parseResolvedScopes(content); if err != nil { return state, err }
	if _, exists := state.dns["Global"]; !exists { return state, errors.New("global DNS scope missing") }
	for key, servers := range state.dns { normalized := []string{}; for _, server := range servers { address, err := resolvedServerAddress(server); if err != nil { return state, err }; normalized = append(normalized, address) }; state.dns[key] = normalized }
	content, err = resolvedCommand(ctx, "domain"); if err != nil { return state, err }
	state.domains, _, err = parseResolvedScopes(content); if err != nil { return state, err }
	for scope := range state.dns { if _, exists := state.domains[scope]; !exists { return state, errors.New("DNS routing scope missing") } }
	for scope := range state.domains { if _, exists := state.dns[scope]; !exists { return state, errors.New("DNS server scope missing") } }
	return state, nil
}

func resolvedRootRoute(domains []string) bool { for _, domain := range domains { if domain == "~." || domain == "." { return true } }; return false }

func effectiveResolvedResolvers(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second); defer cancel()
	state, err := readResolvedDNSState(ctx); if err != nil { return nil, err }
	scopes := []string{}
	for _, scope := range state.order { if resolvedRootRoute(state.domains[scope]) && len(state.dns[scope]) > 0 { scopes = append(scopes, scope) } }
	if len(scopes) == 0 {
		scopes = append(scopes, "Global")
		if len(state.order) > 1 {
			content, err := resolvedCommand(ctx, "default-route"); if err != nil { return nil, err }
			defaults, _, err := parseResolvedScopes(content); if err != nil { return nil, err }
			for _, scope := range state.order { if scope == "Global" { continue }; values := defaults[scope]; if len(values) != 1 || (values[0] != "yes" && values[0] != "no") { return nil, errors.New("DNS default route is unverified") }; if values[0] == "yes" { scopes = append(scopes, scope) } }
		}
	}
	result, seen := []string{}, map[string]bool{}
	for _, scope := range scopes { for _, address := range state.dns[scope] { if !seen[address] { result = append(result, address); seen[address] = true } } }
	if len(result) == 0 { return nil, errors.New("no effective default DNS upstreams reported") }; return result, nil
}

func checkResolvedRootConflicts(ctx context.Context, wanted []string) error {
	state, err := readResolvedDNSState(ctx); if err != nil { return err }
	allowed := map[string]bool{}; for _, address := range wanted { allowed[address] = true }
	for scope, domains := range state.domains { if scope == "Global" || !resolvedRootRoute(domains) { continue }; for _, address := range state.dns[scope] { if !allowed[address] { return fmt.Errorf("%s has a conflicting default DNS route; existing link settings are unchanged", scope) } } }
	return nil
}

// Called only by the root-only host-resolvers CLI in a transient systemd unit.
// Never replace resolv.conf or bounce the network connection (and SSH).
func (s *System) applyHostResolvers(ctx context.Context, values []string) error {
	resolvers, err := validateResolvers(values)
	if err != nil {
		return err
	}
	if s.os.ID == "ubuntu" {
		if !unitFileExists("systemd-resolved") {
			return errors.New("systemd-resolved is required; unmanaged resolver files are left unchanged")
		}
		if err := checkResolvedRootConflicts(ctx, resolvers); err != nil { return fmt.Errorf("cannot safely apply global DNS: %w", err) }
		const target = "/etc/systemd/resolved.conf.d/90-litecp.conf"
		original, readErr := os.ReadFile(target)
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return readErr
		}
		if err := os.MkdirAll("/etc/systemd/resolved.conf.d", 0755); err != nil {
			return err
		}
		if err := writeAtomic(target, []byte("[Resolve]\nDNS="+strings.Join(resolvers, " ")+"\nFallbackDNS=\nDomains=~.\n"), 0644); err != nil {
			return err
		}
		if _, err := runCommand(ctx, 30*time.Second, "systemctl", "restart", "systemd-resolved"); err != nil {
			if readErr == nil {
				_ = writeAtomic(target, original, 0644)
			} else {
				_ = os.Remove(target)
			}
			_, rollbackErr := runCommand(context.Background(), 30*time.Second, "systemctl", "restart", "systemd-resolved")
			return fmt.Errorf("resolver restart failed: %v (rollback restart: %v)", err, rollbackErr)
		}
		return nil
	}
	if s.os.ID != "almalinux" {
		return errors.New("resolver management supports Ubuntu and AlmaLinux")
	}
	route, err := runCommand(ctx, 10*time.Second, "ip", "route", "show", "default")
	if err != nil {
		return err
	}
	fields := strings.Fields(route)
	device := ""
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] == "dev" {
			device = fields[i+1]
			break
		}
	}
	if device == "" {
		return errors.New("default network interface not found; no changes made")
	}
	connection, err := runCommand(ctx, 10*time.Second, "nmcli", "-g", "GENERAL.CON-UUID", "device", "show", device)
	uuid := strings.TrimSpace(connection)
	if err != nil || uuid == "" || uuid == "--" {
		return errors.New("active NetworkManager connection not found; no changes made")
	}
	properties := []string{"ipv4.ignore-auto-dns", "ipv4.dns", "ipv6.ignore-auto-dns", "ipv6.dns"}
	rollbackArgs := []string{"connection", "modify", "uuid", uuid}
	for _, property := range properties {
		value, err := runCommand(ctx, 10*time.Second, "nmcli", "--escape", "no", "-g", property, "connection", "show", "uuid", uuid)
		if err != nil {
			return err
		}
		rollbackArgs = append(rollbackArgs, property, strings.TrimSpace(value))
	}
	v4, v6 := []string{}, []string{}
	for _, value := range resolvers {
		if net.ParseIP(value).To4() != nil {
			v4 = append(v4, value)
		} else {
			v6 = append(v6, value)
		}
	}
	args := []string{"connection", "modify", "uuid", uuid, "ipv4.ignore-auto-dns", "yes", "ipv4.dns", strings.Join(v4, ","), "ipv6.ignore-auto-dns", "yes", "ipv6.dns", strings.Join(v6, ",")}
	if _, err := runCommand(ctx, 20*time.Second, "nmcli", args...); err != nil {
		_, restoreErr := runCommand(context.Background(), 20*time.Second, "nmcli", rollbackArgs...)
		return fmt.Errorf("resolver profile update failed: %v (restore: %v)", err, restoreErr)
	}
	if _, err := runCommand(ctx, 20*time.Second, "nmcli", "device", "reapply", device); err != nil {
		_, restoreErr := runCommand(context.Background(), 20*time.Second, "nmcli", rollbackArgs...)
		_, reapplyErr := runCommand(context.Background(), 20*time.Second, "nmcli", "device", "reapply", device)
		return fmt.Errorf("resolver apply failed: %v (restore: %v; reapply: %v)", err, restoreErr, reapplyErr)
	}
	return nil
}

func defaultResolversMatch(values []string)bool{return len(values)==2&&values[0]=="8.8.8.8"&&values[1]=="8.8.4.4"}

// NetworkManager can return from reapply before its resolv.conf update finishes.
func waitForDefaultResolvers(ctx context.Context, read func() []string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		if defaultResolversMatch(read()) { return nil }
		select {
		case <-ctx.Done(): return fmt.Errorf("default DNS resolver verification failed: %w", ctx.Err())
		case <-time.After(50*time.Millisecond):
		}
	}
}
