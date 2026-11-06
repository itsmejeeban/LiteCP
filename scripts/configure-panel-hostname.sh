#!/usr/bin/env bash
# Configure an already-installed panel hostname using its existing ACME account.
set -Eeuo pipefail
[[ $EUID == 0 && $# == 1 ]] || { echo 'Usage: sudo bash configure-panel-hostname.sh panel.example.com' >&2; exit 1; }
host=$1
[[ $host =~ ^[a-zA-Z0-9]([a-zA-Z0-9.-]*[a-zA-Z0-9])?$ && $host == *.* && $host != *..* ]] || { echo 'A public DNS hostname is required.' >&2; exit 1; }
for tool in certbot nginx python3 curl openssl; do command -v "$tool" >/dev/null; done
panel=/etc/nginx/conf.d/litecp-panel.conf
[[ -f $panel ]] || panel=/etc/nginx/sites-available/litecp-panel.conf
[[ -f $panel && ! -L $panel ]] || { echo 'Managed panel configuration not found.' >&2; exit 1; }
http=/etc/nginx/conf.d/litecp-panel-hostname-http.conf
backup=$(mktemp -d /var/backups/litecp/panel-hostname-XXXXXXXX)
chmod 0700 "$backup"
cp -p "$panel" "$backup/panel.conf"
[[ ! -e $http ]] || cp -p "$http" "$backup/http.conf"
rollback(){
 trap - ERR
 cp -p "$backup/panel.conf" "$panel"
 if [[ -f $backup/http.conf ]]; then cp -p "$backup/http.conf" "$http"; elif [[ -f $http ]]; then mv "$http" "$backup/failed-http.conf"; fi
 nginx -t && systemctl reload nginx || true
 echo "Hostname configuration failed; retained recovery files: $backup" >&2
 exit 1
}
trap rollback ERR
echo '[1/4] Preparing HTTP ACME challenge route...'
install -d -m 0755 /var/lib/letsencrypt/.well-known/acme-challenge
cat > "$http" <<EOF
server {
 listen 80;
 listen [::]:80;
 server_name $host;
 location ^~ /.well-known/acme-challenge/ { root /var/lib/letsencrypt; try_files \$uri =404; }
 location / { return 301 https://$host:4321\$request_uri; }
}
EOF
nginx -t
systemctl reload nginx
echo '[2/4] Requesting trusted certificate using the existing ACME account...'
certbot certonly --non-interactive --keep-until-expiring --cert-name "$host" --webroot -w /var/lib/letsencrypt -d "$host"
openssl x509 -in "/etc/letsencrypt/live/$host/fullchain.pem" -noout -checkhost "$host"
echo '[3/4] Preserving panel routes and switching certificate...'
python3 - "$panel" "$host" <<'PY'
from pathlib import Path
import re,sys
p=Path(sys.argv[1]); host=sys.argv[2]; text=p.read_text()
for pattern,value in [(r'(?m)^\s*server_name\s+[^;]+;',f'    server_name {host} _;'),(r'(?m)^\s*ssl_certificate\s+[^;]+;',f'    ssl_certificate /etc/letsencrypt/live/{host}/fullchain.pem;'),(r'(?m)^\s*ssl_certificate_key\s+[^;]+;',f'    ssl_certificate_key /etc/letsencrypt/live/{host}/privkey.pem;')]:
 text,count=re.subn(pattern,value,text)
 if count != 1: raise RuntimeError('Unexpected panel configuration; refusing ambiguous rewrite')
p.write_text(text)
PY
nginx -t
systemctl reload nginx
echo '[4/4] Verifying trusted HTTPS and renewal reload hook...'
curl --fail --silent --show-error --retry 5 --retry-all-errors --retry-delay 1 --connect-timeout 5 --max-time 10 --resolve "$host:4321:127.0.0.1" "https://$host:4321/api/version"
install -d -m 0755 /etc/letsencrypt/renewal-hooks/deploy
hook=/etc/letsencrypt/renewal-hooks/deploy/litecp-panel-hostname
[[ ! -f $hook ]] || cp -p "$hook" "$backup/previous-renewal-hook"
cat > "$hook" <<'EOF'
#!/bin/sh
set -eu
nginx -t
systemctl reload nginx
EOF
chmod 0755 "$hook"
trap - ERR
echo "Trusted panel URL: https://$host:4321/; previous configuration: $backup"
