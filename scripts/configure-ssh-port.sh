#!/usr/bin/env bash
set -Eeuo pipefail
port="${1:-369}"
[[ "$port" =~ ^[0-9]+$ ]] && ((port >= 1 && port <= 65535)) || { echo 'Invalid SSH port' >&2; exit 1; }
[[ "$port" != 4321 && "$port" != 9090 && "$port" != 80 && "$port" != 443 ]] || { echo 'Port is reserved by LiteCP' >&2; exit 1; }
command -v /usr/sbin/sshd >/dev/null
unit=sshd.service
systemctl cat "$unit" >/dev/null 2>&1 || unit=ssh.service
systemctl cat "$unit" >/dev/null
/usr/sbin/sshd -t -p "$port"
if command -v getenforce >/dev/null && [[ "$(getenforce)" != Disabled ]]; then
  command -v semanage >/dev/null || { echo 'SELinux port tools are required' >&2; exit 1; }
  semanage port -l | awk '$1=="ssh_port_t" {print $0}' | grep -Eq "(^|[ ,])${port}([ ,]|$)" || semanage port -a -t ssh_port_t -p tcp "$port"
fi
if systemctl is-active --quiet firewalld; then
  zones="$(firewall-cmd --get-active-zones | awk '/^[^ ]/{print $1}')"
  [[ -n "$zones" ]] || zones="$(firewall-cmd --get-default-zone)"
  for zone in $zones; do
    firewall-cmd --zone="$zone" --add-port="$port/tcp"
    firewall-cmd --permanent --zone="$zone" --add-port="$port/tcp"
  done
elif command -v ufw >/dev/null; then
  ufw allow "$port/tcp"
else
  echo 'No supported firewall found; SSH port was not changed' >&2; exit 1
fi
backup="$(mktemp -d /var/lib/litecp/ssh-port-change-XXXXXXXX)"
directory="/etc/systemd/system/$unit.d"
override="$directory/99-litecp-port.conf"
mkdir -p "$directory"
had_override=0
if [[ -f "$override" ]]; then cp -a "$override" "$backup/previous.conf"; had_override=1; fi
socket_active=0
systemctl is-active --quiet ssh.socket && socket_active=1
rollback() {
  trap - ERR
  if [[ "$had_override" == 1 ]]; then cp -a "$backup/previous.conf" "$override"; else printf '# Previous SSH service configuration restored\n' > "$override"; fi
  systemctl daemon-reload
  systemctl restart "$unit" || true
  if [[ "$socket_active" == 1 ]]; then systemctl start ssh.socket || true; fi
  echo "SSH change failed. Previous configuration restored. Recovery: $backup" >&2
  exit 1
}
trap rollback ERR
printf '[Service]\nExecStart=\nExecStart=/usr/sbin/sshd -D -p %s\n' "$port" > "$override"
systemctl daemon-reload
if systemctl cat ssh.socket >/dev/null 2>&1; then systemctl stop ssh.socket; fi
systemctl restart "$unit"
python3 - "$port" <<'PY'
import socket,sys,time
for attempt in range(15):
    try:
        with socket.create_connection(('127.0.0.1',int(sys.argv[1])),timeout=1) as connection:
            if connection.recv(255).startswith(b'SSH-'): break
    except OSError: pass
    time.sleep(1)
else: raise SystemExit('New SSH listener could not be verified')
PY
if systemctl cat ssh.socket >/dev/null 2>&1; then systemctl disable ssh.socket; fi
systemctl enable "$unit"
printf '%s\n' "$port" > /var/lib/litecp/ssh-port
if [[ -d /etc/fail2ban/jail.d ]]; then
  jail=/etc/fail2ban/jail.d/zz-litecp-ssh-port.local
  [[ ! -f "$jail" ]] || cp -a "$jail" "$backup/previous-fail2ban.conf"
  printf '[sshd]\nport = %s\n' "$port" > "$jail"
  if systemctl is-active --quiet fail2ban; then fail2ban-client reload || echo 'Warning: review Fail2ban SSH port settings'; fi
fi
trap - ERR
echo "SSH is listening on TCP $port. Previous configuration: $backup"
