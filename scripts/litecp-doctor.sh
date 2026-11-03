#!/usr/bin/env bash
# Non-destructive LiteCP recovery. No package installs or configuration rewrites.
set -uo pipefail
mode=${1:---check}
[[ $mode == --check || $mode == --repair ]] || { echo 'Usage: litecp doctor [--check|--repair]'; exit 2; }
[[ $EUID == 0 ]] || { echo 'Run with sudo or as root.'; exit 2; }
for tool in systemctl timeout python3; do command -v "$tool" >/dev/null || { echo "Missing required tool: $tool"; exit 2; }; done
issues=0; fixed=0
fail(){ printf '[ATTENTION] %s\n' "$*"; issues=$((issues+1)); }
ok(){ printf '[OK] %s\n' "$*"; }
check_service(){
 local unit=$1 required=$2 state
 state=$(systemctl show "$unit" -p LoadState --value 2>/dev/null)
 if [[ $state != loaded ]]; then
  [[ $required == yes ]] && fail "$unit is not installed; rerun the matching LiteCP installer after reviewing its output."
  return
 fi
 if systemctl is-active --quiet "$unit"; then ok "$unit is running"; return; fi
 if [[ $required != yes ]] && ! systemctl is-enabled --quiet "$unit"; then printf '[SKIP] %s is disabled; preserving administrator choice.\n' "$unit"; return; fi
 if [[ $mode == --repair ]]; then
  printf '[REPAIR] Starting %s...\n' "$unit"
  if timeout 90 systemctl start "$unit" && systemctl is-active --quiet "$unit"; then fixed=$((fixed+1));ok "$unit started";return;fi
 fi
 fail "$unit is not running. Inspect: journalctl -u $unit -n 50 --no-pager"
}
printf 'LiteCP Doctor - %s\n' "$mode"
printf '[1/4] Checking stored panel configuration and disk space...\n'
if ! python3 - <<'PY'
import json,os,shutil
root=os.environ.get('LITECP_DATA_DIR','/var/lib/litecp')
try:
 with open(os.path.join(root,'config.json')) as f: json.load(f)
 print('[OK] Panel configuration is valid JSON (credentials not displayed).')
 d=shutil.disk_usage(root)
 print('[INFO] Data filesystem: %.1f GiB free of %.1f GiB.'%(d.free/2**30,d.total/2**30))
 if d.free < 512*2**20: raise RuntimeError('Less than 512 MiB free; review disk usage. No files were removed.')
except Exception as e:
 print('[ATTENTION] Configuration/storage check failed: '+type(e).__name__)
 raise SystemExit(1)
PY
then fail 'Review configuration or storage. Doctor will not replace configuration or delete data.';fi
printf '[2/4] Validating Nginx configuration...\n'
nginx_valid=no
if command -v nginx >/dev/null && timeout 30 nginx -t;then nginx_valid=yes;ok 'Nginx configuration validates';else fail 'Nginx configuration is invalid or nginx is missing; repair the reported configuration before restart.';fi
printf '[3/4] Checking panel and installed hosting services...\n'
check_service litecp.service yes
if [[ $nginx_valid == yes ]];then check_service nginx.service yes;fi
# Only start optional installed services when enabled by the administrator.
for unit in mysqld.service mysql.service crond.service cron.service postfix.service dovecot.service opendkim.service vsftpd.service fail2ban.service named.service; do check_service "$unit" no;done
while read -r unit _;do
 case "$unit" in php*-fpm.service|php*-php-fpm.service|litecp-php*-fpm.service) check_service "$unit" no;;esac
done < <(systemctl list-unit-files --type=service --no-legend --no-pager 2>/dev/null)
printf '[4/4] Checking panel HTTP response...\n'
# Read only the bind value, never source an environment file as shell code.
bind=$(python3 - <<'PY'
import os,re
value=os.environ.get('LITECP_BIND','127.0.0.1:9090')
try:
 for line in open('/etc/litecp/litecp.env'):
  if line.startswith('LITECP_BIND='):value=line.split('=',1)[1].strip().strip('\"\x27')
except FileNotFoundError:pass
m=re.fullmatch(r'(?:127\.0\.0\.1|0\.0\.0\.0|localhost):([0-9]{1,5})',value)
if m and 0<int(m[1])<65536:print('127.0.0.1:'+m[1])
PY
)
if [[ -n $bind ]] && command -v curl >/dev/null;then
 code=$(curl --silent --output /dev/null --write-out '%{http_code}' --max-time 15 --retry 3 --retry-connrefused --retry-delay 2 "http://$bind/" || true)
 case "$code" in 200|301|302|303|307|308) ok "Panel responds locally (HTTP $code)";;*) fail "Panel local health check failed (HTTP $code). Inspect journalctl -u litecp -n 50 --no-pager";;esac
else fail 'Cannot check local HTTP: unsupported bind address or curl missing.';fi
printf '\nRepairs completed: %s. Items needing attention: %s.\n' "$fixed" "$issues"
printf 'No files, databases, credentials, firewall rules or SSH settings were removed or reset.\n'
if (( issues > 0 ));then echo 'STATUS: NEEDS ATTENTION';exit 1;fi
echo 'STATUS: CHECKS PASSED. If remote access still fails, check the panel TLS listener, DNS and provider firewall.'
