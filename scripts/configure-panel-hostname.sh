#!/usr/bin/env bash
# Configure HTTPS for an already-installed panel hostname.
set -Eeuo pipefail
[[ $EUID == 0 && $# -ge 1 && $# -le 3 ]] || { echo 'Usage: sudo bash configure-panel-hostname.sh panel.example.com [port] [email]' >&2; exit 1; }
host=$1
port=${2:-4321}
email=${3:-}
[[ ${#host} -le 253 && $host =~ ^[a-zA-Z0-9]([a-zA-Z0-9.-]*[a-zA-Z0-9])?$ && $host == *.* && $host != *..* ]] || { echo 'A public DNS hostname is required.' >&2; exit 1; }
[[ $port =~ ^[1-9][0-9]{0,4}$ ]] && (( port <= 65535 )) || { echo 'Invalid panel HTTPS port.' >&2; exit 1; }
for tool in certbot nginx python3 curl openssl flock systemctl timeout; do command -v "$tool" >/dev/null || { echo "Required tool unavailable: $tool" >&2; exit 1; }; done
exec 9>/run/litecp-panel-port.lock
flock -n 9 || { echo 'Another panel SSL or port change is running.' >&2; exit 1; }
[[ ! -s /var/lib/litecp/panel-port-pending ]] || { echo 'Finish the pending panel port change first.' >&2; exit 1; }
panel=/etc/nginx/conf.d/litecp-panel.conf
[[ -f $panel ]] || panel=/etc/nginx/sites-available/litecp-panel.conf
[[ -f $panel && ! -L $panel ]] || { echo 'Managed panel configuration not found.' >&2; exit 1; }
http=/etc/nginx/conf.d/litecp-panel-hostname-http.conf
hook=/etc/letsencrypt/renewal-hooks/deploy/litecp-panel-hostname
[[ ! -L $http && ! -L $hook ]] || { echo 'Unexpected linked hostname configuration.' >&2; exit 1; }
digest=$(printf '%s' "$host" | openssl dgst -sha256 -r)
lineage="litecp-panel-${digest%% *}"
mkdir -p -m 0700 /var/backups/litecp
backup=$(mktemp -d /var/backups/litecp/panel-hostname-XXXXXXXX)
chmod 0700 "$backup"
cp -p "$panel" "$backup/panel.conf"
[[ ! -e $http ]] || cp -p "$http" "$backup/http.conf"
[[ ! -e $hook ]] || cp -p "$hook" "$backup/previous-renewal-hook"
probe=''
rollback(){
 trap - EXIT TERM INT
 set +e
 if [[ -n $probe && -f $probe ]]; then mv "$probe" "$backup/http-probe"; fi
 cp -p "$backup/panel.conf" "$panel"
 if [[ -f $backup/http.conf ]]; then cp -p "$backup/http.conf" "$http"; elif [[ -f $http ]]; then mv "$http" "$backup/failed-http.conf"; fi
 if [[ -f $backup/previous-renewal-hook ]]; then cp -p "$backup/previous-renewal-hook" "$hook"; elif [[ -f $hook ]]; then mv "$hook" "$backup/failed-renewal-hook"; fi
 nginx -t && systemctl reload nginx || true
 echo "Hostname configuration failed; retained recovery files: $backup" >&2
 exit 1
}
trap '[[ $? == 0 ]] || rollback' EXIT
trap 'exit 1' TERM INT
echo '[1/4] Preparing HTTP ACME challenge route...'
install -d -m 0755 /var/lib/letsencrypt/.well-known/acme-challenge
cat > "$http" <<EOF
server {
 listen 80;
 listen [::]:80;
 server_name $host;
 location ^~ /.well-known/acme-challenge/ { root /var/lib/letsencrypt; try_files \$uri =404; }
 location / { return 301 https://$host:$port\$request_uri; }
}
EOF
nginx -t
systemctl reload nginx
probe=$(mktemp /var/lib/letsencrypt/.well-known/acme-challenge/litecp-panel-XXXXXXXX)
token=${probe##*/}
printf '%s' "$token" > "$probe"
chmod 0644 "$probe"
# Nginx reload returns before its new workers serve requests. Retry a wrong
# response body as well as connection errors and non-200 responses.
challenge=''
for attempt in {1..10}; do
  if challenge=$(curl --noproxy '*' --silent --max-time 2 --write-out ' %{http_code}' --resolve "$host:80:127.0.0.1" "http://$host/.well-known/acme-challenge/$token") && [[ $challenge == "$token 200" ]]; then
    break
  fi
  sleep 1
done
[[ $challenge == "$token 200" ]] || { echo "The ACME probe did not reach the hostname challenge route (HTTP ${challenge##* }). Check for an existing port 80 virtual host for $host or webroot access restrictions." >&2; exit 1; }
mv "$probe" "$backup/http-probe"
probe=''
echo '[2/4] Requesting trusted certificate...'
contact=()
if [[ -n $email ]]; then contact=(--agree-tos --email "$email"); fi
timeout 180s certbot certonly --non-interactive --keep-until-expiring --cert-name "$lineage" --webroot -w /var/lib/letsencrypt -d "$host" "${contact[@]}"
openssl x509 -in "/etc/letsencrypt/live/$lineage/fullchain.pem" -noout -checkhost "$host"
echo '[3/4] Preserving panel routes and switching certificate...'
python3 - "$panel" "$host" "$lineage" <<'PY'
from pathlib import Path
import re,sys
p=Path(sys.argv[1]); host,lineage=sys.argv[2:]; text=p.read_text()
for pattern,value in [(r'(?m)^\s*server_name\s+[^;]+;',f'    server_name {host} _;'),(r'(?m)^\s*ssl_certificate\s+[^;]+;',f'    ssl_certificate /etc/letsencrypt/live/{lineage}/fullchain.pem;'),(r'(?m)^\s*ssl_certificate_key\s+[^;]+;',f'    ssl_certificate_key /etc/letsencrypt/live/{lineage}/privkey.pem;')]:
 text,count=re.subn(pattern,value,text)
 if count != 1: raise RuntimeError('Unexpected panel configuration; refusing ambiguous rewrite')
p.write_text(text)
PY
nginx -t
systemctl reload nginx
echo '[4/4] Verifying trusted HTTPS and renewal reload hook...'
curl --noproxy '*' --fail --silent --show-error --retry 5 --retry-all-errors --retry-delay 1 --connect-timeout 5 --max-time 10 --resolve "$host:$port:127.0.0.1" "https://$host:$port/api/version" >/dev/null
install -d -m 0755 /etc/letsencrypt/renewal-hooks/deploy
cat > "$hook" <<'EOF'
#!/bin/sh
set -eu
test -n "${RENEWED_LINEAGE:-}" || exit 0
grep -Fq -- "$RENEWED_LINEAGE/fullchain.pem;" /etc/nginx/conf.d/litecp-panel.conf /etc/nginx/sites-available/litecp-panel.conf 2>/dev/null || exit 0
nginx -t
systemctl reload nginx
EOF
chmod 0755 "$hook"
trap - EXIT TERM INT
echo "Trusted panel URL: https://$host:$port/; previous configuration: $backup"
if [[ -f /usr/local/lib/litecp/sync-ftps-tls.py ]] && ! python3 -I /usr/local/lib/litecp/sync-ftps-tls.py; then
  echo 'Warning: Hostname HTTPS is active, but the FTPS certificate could not be synchronized.' >&2
fi
