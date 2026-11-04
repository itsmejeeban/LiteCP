#!/usr/bin/env bash
# Install a separate authoritative DNS service. Existing DNS services are never stopped.
set -euo pipefail
[[ $EUID == 0 ]] || { echo 'Run as root.' >&2; exit 1; }
echo '[DNS 1/5] Checking existing DNS services...'
for unit in named bind9 named-chroot; do
  if systemctl is-active --quiet "$unit"; then
    echo "Existing $unit service detected. Configure a separate DNS address before installing LiteCP DNS; no service was changed." >&2
    exit 1
  fi
done
if [[ ! -f /etc/litecp/dns/enabled ]]; then
  if ss -H -lnut 'sport = :53' | awk '{print $5}' | grep -Ev '^(127\.|\[?::1)' | grep -q .; then
    echo 'Port 53 is already occupied on a non-loopback address. No DNS service was changed.' >&2
    exit 1
  fi
fi
echo '[DNS 2/5] Checking BIND prerequisites...'
if command -v dnf >/dev/null; then
  command -v named-checkzone >/dev/null && command -v named >/dev/null || dnf install -y bind bind-utils
  dns_user=named
elif command -v apt-get >/dev/null; then
  # Keep distribution units masked: only litecp-dns owns the DNS listener.
  # Refuse to replace existing unit files or an administrator's enabled service.
  for unit in named.service bind9.service; do
    if systemctl is-enabled --quiet "$unit"; then
      echo "Existing enabled $unit detected. Disable its automatic start before using LiteCP DNS." >&2
      exit 1
    fi
    if [[ -e "/etc/systemd/system/$unit" && ! -L "/etc/systemd/system/$unit" ]]; then
      echo "Custom $unit detected; configure DNS manually. No unit was replaced." >&2
      exit 1
    fi
    if [[ -L "/etc/systemd/system/$unit" && $(readlink "/etc/systemd/system/$unit") != /dev/null ]]; then
      echo "Custom $unit link detected; configure DNS manually." >&2
      exit 1
    fi
  done
  systemctl mask named.service bind9.service
  if ! command -v named >/dev/null || ! command -v named-checkzone >/dev/null; then
    apt-get update
    DEBIAN_FRONTEND=noninteractive apt-get install -y bind9 bind9-utils
  fi
  dns_user=bind
else
  echo 'Ubuntu or AlmaLinux is required.' >&2; exit 1
fi
echo '[DNS 3/5] Preparing isolated configuration...'
if ! command -v setfacl >/dev/null; then
  if command -v dnf >/dev/null; then dnf install -y acl; else apt-get install -y acl; fi
fi
install -d -m 0750 /etc/litecp
# Traverse only: the DNS account cannot list the panel's private configuration.
setfacl -m "u:$dns_user:--x" /etc/litecp
install -d -m 0755 /etc/litecp/dns
if [[ ! -f /etc/litecp/dns/listen.conf ]]; then
  python3 - <<'PY'
import ipaddress, subprocess
ips = [ipaddress.ip_address(x) for x in subprocess.check_output(['hostname','-I'],text=True).split()]
v4 = ['127.0.0.1'] + [str(x) for x in ips if x.version == 4 and not x.is_loopback]
v6 = [str(x) for x in ips if x.version == 6 and not x.is_loopback and not x.is_link_local]
with open('/etc/litecp/dns/listen.conf','x') as f:
    f.write('listen-on { '+ '; '.join(dict.fromkeys(v4)) + '; };\n')
    f.write('listen-on-v6 { '+ ('; '.join(v6)+';' if v6 else 'none;') + ' };\n')
PY
fi
if [[ ! -f /etc/litecp/dns/named.conf ]]; then
  cat > /etc/litecp/dns/named.conf <<'EOF'
options {
  directory "/etc/litecp/dns";
  recursion no; allow-recursion { none; }; allow-query-cache { none; };
  allow-query { any; }; allow-transfer { none; }; dnssec-validation no;
  include "/etc/litecp/dns/listen.conf";
  managed-keys-directory "/run/litecp-dns";
  pid-file "/run/litecp-dns/named.pid";
  session-keyfile "/run/litecp-dns/session.key";
};
controls {};
EOF
fi
chmod 0644 /etc/litecp/dns/{named,listen}.conf
named-checkconf /etc/litecp/dns/named.conf
echo '[DNS 4/5] Installing dedicated service...'
cat > /etc/systemd/system/litecp-dns.service <<EOF
[Unit]
Description=LiteCP authoritative DNS
After=network-online.target
Wants=network-online.target
[Service]
Type=simple
ExecStart=/usr/sbin/named -g -c /etc/litecp/dns/named.conf -u $dns_user
ExecReload=/bin/kill -HUP \$MAINPID
Restart=on-failure
RuntimeDirectory=litecp-dns
RuntimeDirectoryMode=0755
ExecStartPre=+/usr/bin/chown $dns_user:$dns_user /run/litecp-dns
ExecStartPre=+/bin/sh -c 'if command -v restorecon >/dev/null; then restorecon -R /run/litecp-dns; fi'
ProtectSystem=strict
ReadWritePaths=/run/litecp-dns
ProtectHome=true
PrivateTmp=true
NoNewPrivileges=true
[Install]
WantedBy=multi-user.target
EOF
install -d /etc/systemd/system/litecp-broker.service.d
cat > /etc/systemd/system/litecp-broker.service.d/dns.conf <<'EOF'
[Service]
ReadWritePaths=-/etc/litecp/dns
EOF
if command -v selinuxenabled >/dev/null && selinuxenabled; then
  command -v semanage >/dev/null || { echo 'Install policycoreutils-python-utils for SELinux DNS labels.' >&2; exit 1; }
  semanage fcontext -a -t named_conf_t '/etc/litecp/dns(/.*)?' 2>/dev/null || semanage fcontext -m -t named_conf_t '/etc/litecp/dns(/.*)?'
  semanage fcontext -a -t named_var_run_t '/run/litecp-dns(/.*)?' 2>/dev/null || semanage fcontext -m -t named_var_run_t '/run/litecp-dns(/.*)?'
  restorecon -R /etc/litecp/dns
fi
if [[ -f /etc/apparmor.d/usr.sbin.named ]]; then
  if ! grep -q 'local/usr.sbin.named' /etc/apparmor.d/usr.sbin.named; then
    echo 'BIND AppArmor profile has no local include; add the LiteCP DNS paths before starting DNS.' >&2
    exit 1
  fi
  install -d /etc/apparmor.d/local
  if ! grep -q '# LiteCP authoritative DNS' /etc/apparmor.d/local/usr.sbin.named 2>/dev/null; then
    cat >> /etc/apparmor.d/local/usr.sbin.named <<'EOF'
# LiteCP authoritative DNS
/etc/litecp/dns/ r,
/etc/litecp/dns/** r,
/run/litecp-dns/ rw,
/run/litecp-dns/** rw,
EOF
  fi
  apparmor_parser -r /etc/apparmor.d/usr.sbin.named
fi
systemctl daemon-reload
systemctl enable --now litecp-dns
systemctl is-active --quiet litecp-dns
echo '[DNS 5/5] Allowing authoritative DNS queries...'
if systemctl is-active --quiet firewalld; then
  firewall-cmd --permanent --add-port=53/tcp
  firewall-cmd --permanent --add-port=53/udp
  firewall-cmd --add-port=53/tcp
  firewall-cmd --add-port=53/udp
elif command -v ufw >/dev/null && ufw status | grep -q 'Status: active'; then
  ufw allow 53/tcp
  ufw allow 53/udp
fi
touch /etc/litecp/dns/enabled
echo 'LiteCP DNS is running. Allow TCP/UDP 53 in any provider firewall. Save zones in Website Tools > DNS.'
echo 'On an existing installation, restart LiteCP once to apply its DNS directory sandbox allowance.'
