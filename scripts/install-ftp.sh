#!/usr/bin/env bash
set -euo pipefail
[[ $(id -u) == 0 ]] || exit 1
. /etc/os-release
if [[ ${ID} == ubuntu ]]; then
 apt-get update
 DEBIAN_FRONTEND=noninteractive apt-get install --no-remove -y pure-ftpd
elif [[ ${ID} == almalinux ]]; then
 dnf install --setopt=install_weak_deps=False -y pure-ftpd
else echo 'Unsupported operating system' >&2; exit 1; fi
ROOT=/var/lib/litecp/ftp
mkdir -p "$ROOT"
chmod 700 "$ROOT"
[[ -f "$ROOT/users.passwd" ]] || (umask 077; touch "$ROOT/users.passwd")
pure-pw mkdb "$ROOT/users.pdb" -f "$ROOT/users.passwd"
chmod 600 "$ROOT/users.pdb"
# Dedicated listener; do not modify the distribution FTP service configuration.
if ! systemctl is-active --quiet litecp-ftps && ss -ltn | awk '{print $4}' | grep -qE ':21$'; then echo 'Port 21 already in use; keep existing FTP server unchanged.' >&2; exit 1; fi
if [[ ! -f "$ROOT/tls.pem" ]]; then
 cat /etc/litecp/tls/litecp.key /etc/litecp/tls/litecp.crt > "$ROOT/tls.pem"
 chmod 600 "$ROOT/tls.pem"
fi
BINARY=$(command -v pure-ftpd)
cat > /etc/systemd/system/litecp-ftps.service <<EOF
[Unit]
Description=LiteCP website FTPS (virtual accounts only)
After=network.target
[Service]
Type=simple
ExecStart=$BINARY -A -E -H -j -l puredb:$ROOT/users.pdb -Y 3 -2 $ROOT/tls.pem -p 50000:50049 -U 027:027 -u 1000 -c 30 -C 5
Restart=on-failure
UMask=0077
[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable litecp-ftps
systemctl restart litecp-ftps
systemctl is-active --quiet litecp-ftps
if command -v firewall-cmd >/dev/null && firewall-cmd --state >/dev/null 2>&1; then
 firewall-cmd --permanent --add-port=21/tcp
 firewall-cmd --permanent --add-port=50000-50049/tcp
 firewall-cmd --reload
elif command -v ufw >/dev/null; then
 ufw allow 21/tcp
 ufw allow 50000:50049/tcp
fi
touch "$ROOT/installed"
chmod 600 "$ROOT/installed"
echo 'FTPS ready. Allow TCP 21 and 50000-50049 in any cloud firewall. Replace the inherited panel certificate with a trusted FTP hostname certificate before production use.'
