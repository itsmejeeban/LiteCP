#!/usr/bin/env bash
set -Eeuo pipefail
[[ $(id -u) == 0 ]] || exit 1
exec 9>/run/litecp-panel-port.lock
flock -n 9 || { echo 'Another panel port change is running.' >&2; exit 1; }
mode=${1:-}; port=${2:-}; state=/var/lib/litecp/panel-port-pending
config=/etc/nginx/conf.d/litecp-panel.conf
[[ -f /etc/nginx/sites-available/litecp-panel.conf ]] && config=/etc/nginx/sites-available/litecp-panel.conf
[[ -f "$config" ]] || { echo 'Installed panel proxy not found.' >&2; exit 1; }
if [[ "$mode" == rollback ]]; then
  [[ -s "$state" ]] || exit 0
  read -r old new backup < "$state"
  [[ "$backup" == "$port" ]] || exit 0
  cp -p "$backup" "$config"
  nginx -t && systemctl reload nginx
  printf '%s\n' "$old" > /var/lib/litecp/panel-port
  : > "$state"
  exit 0
fi
[[ "$port" =~ ^[0-9]+$ ]] && (( port>=1024 && port<=65535 )) || { echo 'Use a port between 1024 and 65535.' >&2; exit 1; }
if [[ "$mode" == confirm ]]; then
  read -r old new backup < "$state"
  [[ "$port" == "$new" ]] || { echo 'No matching pending change.' >&2; exit 1; }
  python3 - "$config" "$old" <<'PY'
import pathlib,re,sys
p=pathlib.Path(sys.argv[1]);s=p.read_text();s=re.sub(r'^\s*listen\s+(?:\[::\]:)?'+re.escape(sys.argv[2])+r'\s+ssl;\s*$', '',s,flags=re.M);p.write_text(s)
PY
  if ! nginx -t || ! systemctl reload nginx; then
    cp -p "$backup" "$config"; nginx -t && systemctl reload nginx
    echo 'Confirmation failed; original configuration restored.' >&2; exit 1
  fi
  printf '%s\n' "$new" > /var/lib/litecp/panel-port
  : > "$state"
  echo 'Panel port confirmed. Previous HTTPS listener closed; existing firewall rules were preserved.'
  exit 0
fi
[[ "$mode" == apply ]] || exit 1
[[ ! -s "$state" ]] || { echo 'Confirm the pending change or wait three minutes for rollback.' >&2; exit 1; }
old=$(python3 - "$config" <<'PY'
import pathlib,re,sys
s=pathlib.Path(sys.argv[1]).read_text();m=re.search(r'listen\s+(\d+)\s+ssl;',s)
if not m: raise SystemExit('Cannot determine panel HTTPS port')
print(m[1])
PY
)
[[ "$port" != "$old" ]] || { echo 'This is already the panel port.' >&2; exit 1; }
if ss -H -ltn | awk '{print $4}' | grep -Eq ":${port}$"; then echo 'Port is already in use.' >&2; exit 1; fi
if command -v getenforce >/dev/null && [[ $(getenforce) != Disabled ]]; then
  semanage port -l | awk '$1=="http_port_t"{print}' | grep -Eq "(^|[ ,])${port}([ ,]|$)" || semanage port -a -t http_port_t -p tcp "$port"
fi
if systemctl is-active --quiet firewalld; then
  zones=$(firewall-cmd --get-active-zones | awk '/^[^ ]/{print $1}')
  [[ -n "$zones" ]] || zones=$(firewall-cmd --get-default-zone)
  for zone in $zones; do firewall-cmd --zone="$zone" --add-port="$port/tcp"; firewall-cmd --permanent --zone="$zone" --add-port="$port/tcp"; done
elif command -v ufw >/dev/null && ufw status | grep -q 'Status: active'; then ufw allow "$port/tcp"
else echo 'No supported active host firewall was found.' >&2; exit 1
fi
backup=$(mktemp /var/lib/litecp/panel-port-recovery.XXXXXXXX)
cp -p "$config" "$backup"
chmod 600 "$backup"
# Schedule recovery BEFORE changing the listener. The unit has its own copy of
# the helper, so it does not depend on the HTTP request or service staying alive.
helper=$(mktemp /var/lib/litecp/panel-port-helper.XXXXXXXX)
cp "$0" "$helper"; chmod 700 "$helper"
printf '%s %s %s\n' "$old" "$port" "$backup" > "$state"; chmod 600 "$state"
if ! systemd-run --quiet --on-active=180 --timer-property=AccuracySec=1s /bin/bash "$helper" rollback "$backup"; then : > "$state"; exit 1; fi
python3 - "$config" "$old" "$port" <<'PY'
import pathlib,re,sys
p=pathlib.Path(sys.argv[1]);s=p.read_text()
pattern=r'(^\s*listen\s+)(\[::\]:)?'+re.escape(sys.argv[2])+r'(\s+ssl;)'
s,n=re.subn(pattern,lambda m:m[0]+'\n'+m[1]+(m[2] or '')+sys.argv[3]+m[3],s,flags=re.M)
if n not in (1,2): raise SystemExit('Unexpected panel listener layout')
p.write_text(s)
PY
if ! nginx -t || ! systemctl reload nginx || ! curl -ksSf --max-time 10 "https://127.0.0.1:$port/" >/dev/null; then
 cp -p "$backup" "$config"; nginx -t && systemctl reload nginx; : > "$state"
 echo 'New listener failed; original configuration restored.' >&2; exit 1
fi
echo 'New panel port is ready. Open it and confirm within three minutes, or the old configuration will return automatically.'
