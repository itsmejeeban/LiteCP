#!/usr/bin/env bash
# Non-destructive LiteCP recovery. No package installs or configuration rewrites.
set -uo pipefail
mode=${1:---check}
panel=no
repair=no
case "$mode" in
 --check) ;;
 --repair) repair=yes ;;
 --panel-check) panel=yes ;;
 --panel-repair) panel=yes; repair=yes ;;
 *) echo 'Usage: litecp doctor [--check|--repair]'; exit 2 ;;
esac
[[ $EUID == 0 ]] || { echo 'Run with sudo or as root.'; exit 2; }
for tool in systemctl timeout python3; do command -v "$tool" >/dev/null || { echo "Missing required tool: $tool"; exit 2; }; done
if ! timeout 10 systemctl show -p Version --value >/dev/null 2>&1; then
 echo 'Could not connect to systemd to inspect panel services.'
 exit 2
fi
issues=0; fixed=0
fail(){ printf '[ATTENTION] %s\n' "$*"; issues=$((issues+1)); }
ok(){ printf '[OK] %s\n' "$*"; }
check_service(){
 local unit=$1 required=$2 state enabled can_start=yes
 state=$(timeout 10 systemctl show "$unit" -p LoadState --value 2>/dev/null)
 if [[ $state != loaded ]]; then
  if [[ $required == yes ]]; then
   fail "$unit cannot be loaded. Inspect: systemctl status $unit --no-pager"
  elif [[ -n $state && $state != not-found && $state != masked ]]; then
   fail "$unit failed to load. Inspect: systemctl status $unit --no-pager"
  fi
  return
 fi
 if timeout 10 systemctl is-active --quiet "$unit"; then ok "$unit is running"; return; fi
 if [[ $panel == yes ]]; then
  # Do not change boot-time policy or start the services that carry this job.
  case "$unit" in
   litecp.service|litecp-broker.service|litecp-broker.socket) can_start=no ;;
   *)
    enabled=$(timeout 10 systemctl is-enabled "$unit" 2>/dev/null)
    case "$enabled" in
     enabled|enabled-runtime) ;;
     *) can_start=no ;;
    esac
    ;;
  esac
  if [[ $required != yes && $can_start == no ]]; then
   printf '[SKIP] %s is inactive and is not enabled.\n' "$unit"
   return
  fi
 elif [[ $unit == litecp-ftps.service ]] && ! timeout 10 systemctl is-enabled --quiet "$unit"; then
  can_start=no
 elif [[ $required != yes ]] && ! timeout 10 systemctl is-enabled --quiet "$unit"; then
  printf '[SKIP] %s is disabled; preserving administrator choice.\n' "$unit"
  return
 fi
 if [[ $repair == yes && $can_start == yes ]]; then
  printf '[REPAIR] Starting %s...\n' "$unit"
  if [[ $panel == yes ]]; then
   if timeout 45 systemctl --job-mode=fail start "$unit" >/dev/null 2>&1 && timeout 10 systemctl is-active --quiet "$unit"; then
    fixed=$((fixed+1)); ok "$unit started"; return
   fi
  elif timeout 90 systemctl start "$unit" && timeout 10 systemctl is-active --quiet "$unit"; then
   fixed=$((fixed+1)); ok "$unit started"; return
  fi
 fi
 fail "$unit is not running. Inspect: journalctl -u $unit -n 50 --no-pager"
}

check_ftps(){
 local root="${LITECP_DATA_DIR:-/var/lib/litecp}/ftp"
 local state
 state=$(timeout 10 systemctl show litecp-ftps.service -p LoadState --value 2>/dev/null)
 if [[ ! -f "$root/installed" || $state == not-found || -z $state ]] ||
    ! command -v pure-ftpd >/dev/null || ! command -v pure-pw >/dev/null; then
  fail 'LiteCP FTPS is missing or its installation is incomplete. Review port 21 ownership, then run scripts/install-ftp.sh as root from the matching LiteCP source checkout.'
  return
 fi
 if [[ ! -r "$root/users.passwd" || ! -r "$root/users.pdb" || ! -s "$root/tls.pem" ]]; then
  fail 'LiteCP FTPS is missing its credential files or TLS bundle. Review the FTPS files and installer log before repair.'
  return
 fi
 check_service litecp-ftps.service yes
}
printf 'LiteCP Doctor - %s\n' "$mode"
printf '[1/4] Checking stored panel configuration and disk space...\n'
if ! timeout 15 python3 - <<'PY'
import json,os,shutil
root=os.environ.get('LITECP_DATA_DIR','/var/lib/litecp')
try:
 with open(os.path.join(root,'config.json')) as f:
  if not isinstance(json.load(f), dict): raise ValueError('Expected a configuration object')
 print('[OK] Panel configuration is valid JSON (credentials not displayed).')
 d=shutil.disk_usage(root)
 print('[INFO] Data filesystem: %.1f GiB free of %.1f GiB.'%(d.free/2**30,d.total/2**30))
except Exception as e:
 print('[ATTENTION] Configuration/storage check failed: '+type(e).__name__)
 raise SystemExit(1)
if d.free < 512*2**20:
 print('[ATTENTION] Less than 512 MiB free on the panel data filesystem.')
 raise SystemExit(1)
PY
then fail 'Review configuration or storage. Doctor will not replace configuration or delete data.';fi
printf '[2/4] Validating Nginx configuration...\n'
nginx_valid=no
if command -v nginx >/dev/null; then
 if [[ $panel == yes ]]; then
  timeout 30 nginx -t >/dev/null 2>&1 && nginx_valid=yes
 else
  timeout 30 nginx -t && nginx_valid=yes
 fi
fi
if [[ $nginx_valid == yes ]]; then
 ok 'Nginx configuration validates'
else
 fail 'Nginx is missing or configuration validation failed. Run nginx -t over SSH to see the details.'
fi
printf '[3/4] Checking panel and installed hosting services...\n'
check_service litecp.service yes
if [[ $panel == yes ]] || timeout 10 systemctl cat litecp-broker.service >/dev/null 2>&1; then
 check_service litecp-broker.service yes
fi
if [[ $panel == yes ]]; then check_service litecp-broker.socket yes; fi
if [[ $nginx_valid == yes ]];then check_service nginx.service yes;fi
# Only start optional installed services when enabled by the administrator.
check_ftps
for unit in mysqld.service mysql.service crond.service cron.service postfix.service dovecot.service opendkim.service vsftpd.service fail2ban.service named.service; do check_service "$unit" no;done
if ! php_units=$(timeout 10 systemctl list-unit-files --type=service --no-legend --no-pager 2>/dev/null); then
 fail 'Could not enumerate installed PHP-FPM services.'
fi
while read -r unit _;do
 if [[ $unit =~ ^(php[0-9]+\.[0-9]+-fpm|php[0-9]+-php-fpm|litecp-php[0-9]+-fpm)\.service$ ]]; then
  check_service "$unit" no
 fi
done <<< "$php_units"
printf '[4/4] Checking panel HTTP response...\n'
# Read only the bind value, never source an environment file as shell code.
bind=$(timeout 10 python3 - "$panel" <<'PY'
import ipaddress,os,sys
value=os.environ.get('LITECP_BIND','127.0.0.1:9090')
paths=('/etc/litecp/frontend.env',) if sys.argv[1]=='yes' else ('/etc/litecp/litecp.env','/etc/litecp/frontend.env')
for path in paths:
 try:
  with open(path) as env:
   for line in env:
    if line.startswith('LITECP_BIND='):value=line.split('=',1)[1].strip().strip('\"\x27')
 except FileNotFoundError:pass
 except OSError:raise SystemExit(1)
try:
 host,port=value.rsplit(':',1)
 if host=='localhost':host='127.0.0.1'
 address=ipaddress.ip_address(host.strip('[]'))
 if address.is_unspecified: address=ipaddress.ip_address('::1' if address.version==6 else '127.0.0.1')
 if address.is_loopback and port.isascii() and port.isdecimal() and 0<int(port)<65536:
  print(('['+str(address)+']' if address.version==6 else str(address))+':'+port)
except ValueError:pass
PY
)
if [[ -n $bind ]] && command -v curl >/dev/null;then
 if [[ $panel == yes ]]; then
  if curl --fail --silent --noproxy '*' --max-time 10 "http://$bind/api/version" | timeout 10 python3 -c 'import json,re,sys; data=json.load(sys.stdin); sys.exit(not isinstance(data,dict) or not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+",str(data.get("version",""))))' 2>/dev/null; then
   ok 'Panel version endpoint responds locally'
  else
   fail 'The local panel version endpoint did not respond as expected. Inspect: journalctl -u litecp -n 50 --no-pager'
  fi
 else
  code=$(curl --silent --noproxy '*' --output /dev/null --write-out '%{http_code}' --max-time 15 --retry 3 --retry-connrefused --retry-delay 2 "http://$bind/" || true)
  case "$code" in 200|301|302|303|307|308) ok "Panel responds locally (HTTP $code)";;*) fail "Panel local health check failed (HTTP $code). Inspect journalctl -u litecp -n 50 --no-pager";;esac
 fi
else fail 'Cannot check local HTTP: unsupported bind address or curl missing.';fi
printf '\nRepairs completed: %s. Items needing attention: %s.\n' "$fixed" "$issues"
printf 'No files, databases, credentials, firewall rules or SSH settings were removed or reset.\n'
if (( issues > 0 ));then echo 'STATUS: NEEDS ATTENTION';exit 1;fi
echo 'STATUS: CHECKS PASSED. If remote access still fails, check the panel TLS listener, DNS and provider firewall.'
