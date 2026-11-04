#!/usr/bin/env bash
# Optional components. No feature is installed unless named on the command line.
set -Eeuo pipefail
[[ $(id -u) -eq 0 ]] || { echo 'Run as root.' >&2; exit 1; }
source /etc/os-release
[[ $ID == ubuntu || $ID == almalinux ]] || exit 1
[[ $# -gt 0 ]] || { echo 'Usage: bash scripts/install-features.sh mail antivirus waf antispam'; exit 1; }
for feature in "$@"; do [[ $feature == mail || $feature == antivirus || $feature == waf || $feature == antispam ]] || { echo "Unknown feature: $feature"; exit 1; }; done
exec 9>/run/litecp-feature-installer.lock
flock -n 9 || { echo 'Another feature installer is running.' >&2; exit 1; }
BACKUP=/var/backups/litecp/feature-install-$(date -u +%Y%m%dT%H%M%SZ)
install -d -m 0700 "$BACKUP"
FEATURE_TMP=$(mktemp -d "$BACKUP/downloads.XXXXXXXX")
trap 'echo "Installation downloads and working files retained at $FEATURE_TMP"' EXIT
for directory in /etc/nginx /etc/postfix /etc/dovecot /etc/opendkim.conf /etc/litecp/mail /etc/rspamd /etc/unbound; do
  [[ ! -e $directory ]] || cp -a --parents "$directory" "$BACKUP/"
done
# Restore just the Nginx files this installer can replace if validation fails.
# Other component backups remain available for an administrator-led rollback.
NGINX_FILES=(/etc/nginx/conf.d/litecp-webmail.conf /usr/share/nginx/modules/mod-modsecurity.conf)
for nginx_file in "${NGINX_FILES[@]}"; do
  [[ ! -e $nginx_file ]] || cp -a --parents "$nginx_file" "$BACKUP/"
done
recover_nginx() {
  local exit_status=$?
  trap - ERR
  for nginx_file in "${NGINX_FILES[@]}"; do
    if [[ -f $BACKUP$nginx_file ]]; then cp -a "$BACKUP$nginx_file" "$nginx_file"
    elif [[ -f $nginx_file ]]; then
      # Retain a newly created invalid entry point instead of deleting it.
      mv -- "$nginx_file" "$BACKUP/failed-$(basename "$nginx_file")"; fi
  done
  echo "Feature installation failed. Previous Nginx entry-point configuration restored; full component backups: $BACKUP" >&2
  exit "$exit_status"
}
trap recover_nginx ERR
download() {
  curl --fail --location --proto '=https' --tlsv1.2 --retry 3 "$1" -o "$FEATURE_TMP/$3"
  printf '%s  %s\n' "$2" "$FEATURE_TMP/$3" | sha256sum --check --status
}
packages() {
  if [[ $ID == ubuntu ]]; then DEBIAN_FRONTEND=noninteractive apt-get install --no-remove --no-install-recommends -y "$@"
  else dnf install --setopt=install_weak_deps=False -y "$@"; fi
}
open_port() {
  if [[ $ID == ubuntu ]]; then ufw allow "$1/tcp"
  else firewall-cmd --permanent --zone=public --add-port="$1/tcp"; firewall-cmd --reload; fi
}

install_antivirus() {
  echo "Installing ClamAV scanner and preparing malware signatures..."
  if [[ $ID == ubuntu ]]; then packages clamav clamav-freshclam
  elif dnf -q repoquery --available clamav-freshclam | grep -q .; then packages clamav clamav-freshclam
  else packages clamav clamav-update; fi
  # Signature updater only. LiteCP invokes clamscan under the website's UID.
  # Do not start clamd: its resident signature database would consume memory.
  if ! find /var/lib/clamav -maxdepth 1 -name 'daily.*' -print -quit | grep -q .; then
    systemctl stop clamav-freshclam.service 2>/dev/null || true
    freshclam || { echo 'Signature download failed. Run freshclam before scanning.' >&2; return 1; }
  fi
  cat > /etc/systemd/system/litecp-clamav-update.service <<'EOF'
[Unit]
Description=LiteCP ClamAV signature update
Wants=network-online.target
After=network-online.target
[Service]
Type=oneshot
ExecStart=/usr/bin/freshclam --quiet
Nice=15
EOF
  cat > /etc/systemd/system/litecp-clamav-update.timer <<'EOF'
[Unit]
Description=Update LiteCP malware signatures twice daily
[Timer]
OnCalendar=*-*-* 03,15:00:00
RandomizedDelaySec=1800
Persistent=true
[Install]
WantedBy=timers.target
EOF
  systemctl disable --now clamav-freshclam.service 2>/dev/null || true
  systemctl daemon-reload
  systemctl enable --now litecp-clamav-update.timer
  clamscan --version
  systemctl is-active --quiet litecp-clamav-update.timer || { echo 'ClamAV signature update timer did not start.' >&2; return 1; }
  echo 'ClamAV is ready. Signatures update twice daily; scans never automatically delete files.'
}

install_mail() {
  local webmail_port=${LITECP_WEBMAIL_PORT:-8444}
  [[ $webmail_port =~ ^[0-9]+$ && $webmail_port -ge 1024 && $webmail_port -le 65535 ]] || { echo "Invalid webmail port." >&2; return 1; }
  local php pool unit webgroup panel host cert key version=1.7.5
  [[ -f /etc/litecp/mail/mailboxes ]] || { echo 'Install the base LiteCP mail stack first.' >&2; return 1; }
  if [[ $ID == ubuntu ]]; then panel=/etc/nginx/sites-available/litecp-panel.conf
  else panel=/etc/nginx/conf.d/litecp-panel.conf; fi
  host=${LITECP_MAIL_HOSTNAME:-$(postconf -h myhostname)}
  [[ $host =~ ^[a-zA-Z0-9]([a-zA-Z0-9.-]*[a-zA-Z0-9])?$ && $host == *.* ]] || { echo 'A public fully qualified mail hostname is required.' >&2; return 1; }
  cert=${LITECP_MAIL_CERT:-$(awk '$1=="ssl_certificate" {gsub(/;/,"",$2);print $2;exit}' "$panel")}
  key=${LITECP_MAIL_KEY:-$(awk '$1=="ssl_certificate_key" {gsub(/;/,"",$2);print $2;exit}' "$panel")}
  [[ $cert == /* && $key == /* && -r $cert && -r $key && $cert != *[$'\r\n\t ;{}']* && $key != *[$'\r\n\t ;{}']* ]] || { echo 'Readable absolute mail certificate/key paths without Nginx delimiters are required.' >&2; return 1; }
  # Reject an invalid certificate before installing packages or changing services.
  openssl x509 -in "$cert" -noout -checkhost "$host" >/dev/null || { echo 'The mail hostname needs a matching TLS certificate before installing webmail.' >&2; return 1; }
  openssl x509 -in "$cert" -noout -checkend 0 >/dev/null || { echo 'The mail certificate has expired.' >&2; return 1; }
  local cert_public key_public
  cert_public=$(openssl x509 -in "$cert" -pubkey -noout | openssl pkey -pubin -outform DER | sha256sum)
  key_public=$(openssl pkey -in "$key" -pubout -outform DER | sha256sum)
  [[ $cert_public == "$key_public" ]] || { echo 'The mail certificate and private key do not match.' >&2; return 1; }
  if [[ $ID == ubuntu ]]; then
    packages opendkim opendkim-tools acl php8.3-fpm php8.3-sqlite3 php8.3-intl php8.3-mbstring php8.3-xml php8.3-curl php8.3-zip
    php=/usr/bin/php8.3; pool=/etc/php/8.3/fpm/pool.d/litecp-webmail.conf; unit=php8.3-fpm; webgroup=www-data
    panel=/etc/nginx/sites-available/litecp-panel.conf
  else
    packages opendkim opendkim-tools acl php83-php-fpm php83-php-pdo php83-php-intl php83-php-mbstring php83-php-xml php83-php-pecl-zip
    php=/opt/remi/php83/root/usr/bin/php; pool=/etc/opt/remi/php83/php-fpm.d/litecp-webmail.conf; unit=php83-php-fpm; webgroup=nginx
    panel=/etc/nginx/conf.d/litecp-panel.conf
  fi
  "$php" -r 'exit(extension_loaded("pdo_sqlite") ? 0 : 1);' || { echo 'PHP 8.3 SQLite is required.' >&2; return 1; }
  # Explicit hostname override aligns SMTP/IMAP with the trusted panel certificate.
  # It does not change public DNS or the server's OS hostname.
  postconf -e "myhostname = $host"
  postconf -e "smtpd_tls_cert_file = $cert"
  postconf -e "smtpd_tls_key_file = $key"
  cat > /etc/dovecot/conf.d/zz-litecp-webmail-tls.conf <<EOF
ssl_cert = <$cert
ssl_key = <$key
EOF
  doveconf -n >/dev/null
  download "https://github.com/roundcube/roundcubemail/releases/download/$version/roundcubemail-$version-complete.tar.gz" 93a66c94e3efff7db93187c1c9d7f35546647a3e1e7b9327cc87911564edf9d1 roundcube.tar.gz
  # Versioned code is root-owned; only private SQLite, sessions and temp are writable.
  id litecp-webmail >/dev/null 2>&1 || useradd --system --home-dir /var/lib/litecp-webmail --shell /usr/sbin/nologin litecp-webmail
  install -d -m 0755 /opt/litecp
  if [[ ! -d /opt/litecp/roundcubemail-$version ]]; then
    tar -xzf "$FEATURE_TMP/roundcube.tar.gz" -C /opt/litecp --no-same-owner
  fi
  [[ ! -e /opt/litecp/webmail || -L /opt/litecp/webmail ]] || { echo '/opt/litecp/webmail is not a managed symlink.' >&2; return 1; }
  ln -sfn "roundcubemail-$version" /opt/litecp/webmail
  install -d -o litecp-webmail -g litecp-webmail -m 0700 /var/lib/litecp-webmail /var/lib/litecp-webmail/temp /var/lib/litecp-webmail/logs
  if [[ ! -f /opt/litecp/webmail/config/config.inc.php ]]; then
    local secret
    secret=$(openssl rand -hex 12)
    cat > /opt/litecp/webmail/config/config.inc.php <<EOF
<?php
\$config = [];
\$config['db_dsnw'] = 'sqlite:////var/lib/litecp-webmail/roundcube.db?mode=0600';
\$config['imap_host'] = 'ssl://127.0.0.1:993';
\$config['smtp_host'] = 'ssl://127.0.0.1:465';
\$config['smtp_user'] = '%u';
\$config['smtp_pass'] = '%p';
\$config['imap_conn_options'] = ['ssl' => ['verify_peer' => true, 'verify_peer_name' => true, 'peer_name' => '$host', 'cafile' => '/opt/litecp/webmail/config/mail-ca.pem']];
\$config['smtp_conn_options'] = \$config['imap_conn_options'];
\$config['des_key'] = '$secret';
\$config['product_name'] = 'LiteCP - By Hostingtom | Webmail';
\$config['temp_dir'] = '/var/lib/litecp-webmail/temp';
\$config['log_dir'] = '/var/lib/litecp-webmail/logs';
\$config['enable_installer'] = false;
\$config['identities_level'] = 4;
\$config['session_samesite'] = 'Strict';
\$config['session_lifetime'] = 30;
\$config['force_https'] = true;
\$config['plugins'] = ['archive', 'zipdownload'];
\$config['skin'] = 'elastic';
EOF
  fi
  local bundle=/etc/ssl/certs/ca-certificates.crt
  [[ $ID != almalinux ]] || bundle=/etc/pki/tls/certs/ca-bundle.crt
  cp "$bundle" /opt/litecp/webmail/config/mail-ca.pem
  chown root:litecp-webmail /opt/litecp/webmail/config/mail-ca.pem
  chmod 0640 /opt/litecp/webmail/config/mail-ca.pem
  chown root:litecp-webmail /opt/litecp/webmail/config/config.inc.php
  chmod 0640 /opt/litecp/webmail/config/config.inc.php
  install -d -m 0755 /run/litecp-webmail
  printf 'd /run/litecp-webmail 0755 root root -\n' > /etc/tmpfiles.d/litecp-webmail.conf
  if command -v semanage >/dev/null && command -v selinuxenabled >/dev/null && selinuxenabled; then
    semanage fcontext -a -t httpd_var_run_t '/run/litecp-webmail(/.*)?' 2>/dev/null || semanage fcontext -m -t httpd_var_run_t '/run/litecp-webmail(/.*)?'
    restorecon -R /run/litecp-webmail
  fi
  cat > "$pool" <<EOF
[litecp-webmail]
user = litecp-webmail
group = litecp-webmail
listen = /run/litecp-webmail/webmail.sock
listen.owner = $webgroup
listen.group = $webgroup
listen.mode = 0660
pm = ondemand
pm.max_children = 3
pm.process_idle_timeout = 10s
pm.max_requests = 500
request_terminate_timeout = 120s
clear_env = yes
security.limit_extensions = .php
php_admin_value[open_basedir] = /opt/litecp:/var/lib/litecp-webmail:/tmp:/etc/ssl/certs:/etc/pki/tls/certs
php_admin_value[upload_tmp_dir] = /var/lib/litecp-webmail/temp
php_admin_value[session.save_path] = /var/lib/litecp-webmail/temp
php_admin_value[memory_limit] = 128M
php_admin_value[upload_max_filesize] = 25M
php_admin_value[post_max_size] = 30M
php_admin_flag[session.cookie_secure] = on
php_admin_flag[session.cookie_httponly] = on
php_admin_flag[display_errors] = off
EOF
  runuser -u litecp-webmail -- "$php" /opt/litecp/webmail/bin/updatedb.sh --dir=/opt/litecp/webmail/SQL --package=roundcube
  cat > /etc/nginx/conf.d/litecp-webmail.conf <<EOF
server {
    listen $webmail_port ssl;
    listen [::]:$webmail_port ssl;
    server_name $host;
    ssl_certificate $cert;
    ssl_certificate_key $key;
    ssl_protocols TLSv1.2 TLSv1.3;
    root /opt/litecp/webmail/public_html;
    index index.php;
    client_max_body_size 30m;
    add_header X-Content-Type-Options nosniff always;
    add_header X-Robots-Tag 'noindex, nofollow' always;
    location /api/ { return 404; }
    location = /installer.php { return 404; }
    location ~ /\. { return 404; }
    location = /favicon.ico { rewrite ^ /static.php/skins/elastic/images/favicon.ico last; }
    location / { try_files \$uri \$uri/ /index.php?\$query_string; }
    location ~ ^/(index|static)\.php(?:/|$) {
        fastcgi_split_path_info ^(.+?\.php)(/.*)$;
        include fastcgi_params;
        fastcgi_param SCRIPT_FILENAME \$document_root\$fastcgi_script_name;
        fastcgi_param PATH_INFO \$fastcgi_path_info;
        fastcgi_param HTTPS on;
        fastcgi_param HTTP_PROXY '';
        fastcgi_pass unix:/run/litecp-webmail/webmail.sock;
    }
    location ~ \.php { return 404; }
}
EOF
  # DKIM keys remain per domain. Existing keys are never regenerated on upgrade.
  install -d -m 0750 -o opendkim -g opendkim /etc/litecp/mail/dkim
  touch /etc/litecp/mail/KeyTable /etc/litecp/mail/SigningTable
  chmod 0644 /etc/litecp/mail/KeyTable /etc/litecp/mail/SigningTable
  setfacl -m u:opendkim:--x /etc/litecp /etc/litecp/mail
  cat > /etc/opendkim.conf <<'EOF'
Syslog yes
UMask 007
Mode s
Canonicalization relaxed/simple
Socket inet:8891@127.0.0.1
UserID opendkim:opendkim
KeyTable refile:/etc/litecp/mail/KeyTable
SigningTable refile:/etc/litecp/mail/SigningTable
InternalHosts 127.0.0.1,::1
EOF
  postconf -e 'smtpd_milters = inet:127.0.0.1:8891'
  postconf -e 'non_smtpd_milters = inet:127.0.0.1:8891'
  postconf -e 'milter_default_action = tempfail'
  postconf -e 'milter_protocol = 6'
  opendkim -n
  /usr/local/bin/litecp sync-mail
  local map_type
  map_type=$(postconf -h virtual_mailbox_maps); map_type=${map_type%%:*}
  postconf -e "smtpd_sender_login_maps = $map_type:/etc/litecp/mail/senders"
  postconf -P 'submission/inet/smtpd_sender_restrictions=reject_authenticated_sender_login_mismatch'
  postconf -P 'smtps/inet/smtpd_sender_restrictions=reject_authenticated_sender_login_mismatch'
  postfix check
  if [[ $ID == almalinux ]]; then
    semanage port -a -t http_port_t -p tcp "$webmail_port" 2>/dev/null || semanage port -m -t http_port_t -p tcp "$webmail_port"
    semanage fcontext -a -t httpd_sys_content_t '/opt/litecp(/.*)?' 2>/dev/null || semanage fcontext -m -t httpd_sys_content_t '/opt/litecp(/.*)?'
    semanage fcontext -a -t httpd_sys_rw_content_t '/var/lib/litecp-webmail(/.*)?' 2>/dev/null || semanage fcontext -m -t httpd_sys_rw_content_t '/var/lib/litecp-webmail(/.*)?'
    restorecon -R /opt/litecp /var/lib/litecp-webmail
  fi
  nginx -t
  systemctl restart "$unit"
  bash "$(dirname "${BASH_SOURCE[0]}")/install-webmail-signon.sh"
  if [[ ! -e /etc/litecp/mail/disabled ]]; then systemctl enable --now opendkim; systemctl restart postfix dovecot; fi
  systemctl reload nginx
  open_port "$webmail_port"
  printf 'https://%s:%s/\n' "$host" "$webmail_port" > /etc/litecp/mail/webmail-url
  # A renewed certificate is not used by long-running TLS listeners until reload.
  if [[ $cert == /etc/letsencrypt/live/*/fullchain.pem ]]; then
    local lineage hook=/etc/letsencrypt/renewal-hooks/deploy/litecp-mail-reload
    lineage=${cert%/fullchain.pem}
    if [[ -f $hook ]]; then cp -a --parents "$hook" "$BACKUP/"; fi
    install -d -m 0755 /etc/letsencrypt/renewal-hooks/deploy
    cat > "$hook" <<EOF
#!/usr/bin/env bash
set -euo pipefail
[[ \${RENEWED_LINEAGE:-} == '$lineage' ]] || exit 0
nginx -t
doveconf -n >/dev/null
postfix check
systemctl reload nginx
for unit in postfix dovecot; do
  if systemctl is-active --quiet "\$unit"; then systemctl reload "\$unit"; fi
done
EOF
    chmod 0755 "$hook"
  fi
  echo "Webmail ready at https://$host:$webmail_port/; publish the panel's DNS records and test mail before changing MX."
}

install_antispam() {
  local redis_unit unbound_config current
  [[ -f /etc/litecp/mail/mailboxes ]] || { echo 'Install the base LiteCP mail stack first.' >&2; return 1; }
  if [[ $ID == ubuntu ]]; then
    install -d -m 0755 /etc/apt/keyrings
    curl --fail --location --proto '=https' --tlsv1.2 --retry 3 https://rspamd.com/apt-stable/gpg.key -o "$FEATURE_TMP/rspamd.key"
    gpg --dearmor --yes --output /etc/apt/keyrings/rspamd.gpg "$FEATURE_TMP/rspamd.key"
    printf 'deb [signed-by=/etc/apt/keyrings/rspamd.gpg] https://rspamd.com/apt-stable/ %s main\n' "$VERSION_CODENAME" > /etc/apt/sources.list.d/rspamd.list
    apt-get update
    packages rspamd redis-server unbound
    redis_unit=redis-server
    unbound_config=/etc/unbound/unbound.conf.d/litecp-rspamd.conf
  else
    local el_major cache_package
    el_major=$(rpm -E '%{rhel}')
    curl --fail --location --proto '=https' --tlsv1.2 --retry 3 "https://rspamd.com/rpm-stable/centos-$el_major/rspamd.repo" -o /etc/yum.repos.d/rspamd.repo
    cache_package=redis
    if ! dnf -q repoquery --available redis | grep -q .; then
      cache_package=valkey
    fi
    packages rspamd "$cache_package" unbound
    redis_unit="$cache_package"
    unbound_config=/etc/unbound/conf.d/litecp-rspamd.conf
  fi
  install -d -m 0755 "$(dirname "$unbound_config")" /etc/rspamd/local.d
  cat > "$unbound_config" <<'EOF'
server:
  interface: 127.0.0.1
  port: 5353
  access-control: 127.0.0.0/8 allow
  hide-identity: yes
  hide-version: yes
  prefetch: yes
EOF
  cat > /etc/rspamd/local.d/options.inc <<'EOF'
task_timeout = 15s;
dns {
  nameserver = ["127.0.0.1:5353"];
}
EOF
  cat > /etc/rspamd/local.d/redis.conf <<'EOF'
servers = "127.0.0.1:6379";
EOF
  cat > /etc/rspamd/local.d/actions.conf <<'EOF'
reject = 15;
add_header = 6;
greylist = 4;
EOF
  # OpenDKIM is the only outbound signer for LiteCP mail domains.
  # Keep Rspamd's inbound DKIM verification; disable just its signing module.
  cat > /etc/rspamd/local.d/dkim_signing.conf <<'EOF'
  enabled = false;
  EOF
  cat > /etc/rspamd/local.d/worker-proxy.inc <<'EOF'
bind_socket = "127.0.0.1:11332";
milter = yes;
timeout = 120s;
upstream "local" {
  default = yes;
  self_scan = yes;
}
EOF
  cat > /etc/rspamd/local.d/worker-controller.inc <<'EOF'
bind_socket = "127.0.0.1:11334";
secure_ip = "127.0.0.1";
EOF
  if [[ $ID == almalinux ]]; then
    semanage port -a -t dns_port_t -p tcp 5353 2>/dev/null || semanage port -m -t dns_port_t -p tcp 5353
    semanage port -a -t dns_port_t -p udp 5353 2>/dev/null || semanage port -m -t dns_port_t -p udp 5353
  fi
  unbound-checkconf
  systemctl enable --now unbound "$redis_unit"
  rspamadm configtest
  systemctl enable --now rspamd
  current=$(postconf -h smtpd_milters)
  if [[ $current != *'127.0.0.1:11332'* ]]; then
    [[ -z $current ]] || current="$current, "
    postconf -e "smtpd_milters = ${current}inet:127.0.0.1:11332"
  fi
  current=$(postconf -h non_smtpd_milters)
  if [[ $current != *'127.0.0.1:11332'* ]]; then
    [[ -z $current ]] || current="$current, "
    postconf -e "non_smtpd_milters = ${current}inet:127.0.0.1:11332"
  fi
  postconf -e 'milter_default_action = tempfail'
  postconf -e 'milter_protocol = 6'
  postfix check
  systemctl restart postfix
  printf 'Subject: LiteCP Rspamd test\n\nAntispam scanner is ready.\n' | rspamc >/dev/null
  echo 'Rspamd inbound/outbound filtering is active. Its controller is bound to localhost only.'
}

install_waf() {
  if [[ $ID == ubuntu ]]; then
    packages libnginx-mod-http-modsecurity
  else
    # Compile only a dynamic connector against distro-matched Nginx headers.
    # Do not replace the distribution's Nginx executable.
    dnf config-manager --set-enabled crb
    local nginx_build
    nginx_build=$(rpm -q --qf '%{VERSION}-%{RELEASE}.%{ARCH}' nginx)
    packages gcc gcc-c++ make cmake flex bison autoconf automake libtool pkgconf-pkg-config pcre2-devel libxml2-devel libcurl-devel openssl-devel lmdb-devel "nginx-mod-devel-$nginx_build"
    if ! pkg-config --exists yajl; then
      # ModSecurity upstream documents YAJL 2.1.0 as a mandatory dependency
      # and its compilation recipe builds this exact release when no distro
      # development package exists. Keep it within ModSecurity's prefix.
      download https://github.com/lloyd/yajl/archive/refs/tags/2.1.0.tar.gz 3fb73364a5a30efe615046d07e6db9d09fd2b41c763c5f7d3bfb121cd5c5ac5a yajl.tar.gz
      tar -xzf "$FEATURE_TMP/yajl.tar.gz" -C "$FEATURE_TMP"
      cmake -S "$FEATURE_TMP/yajl-2.1.0" -B "$FEATURE_TMP/yajl-build" -DCMAKE_BUILD_TYPE=Release -DCMAKE_INSTALL_PREFIX=/usr/local/modsecurity
      cmake --build "$FEATURE_TMP/yajl-build" --parallel 2
      cmake --install "$FEATURE_TMP/yajl-build"
    fi
    download https://github.com/owasp-modsecurity/ModSecurity/releases/download/v3.0.16/modsecurity-v3.0.16.tar.gz 739be3c71b1939f14e91afe1eeae654acbd440da11bd29790458840bc315b4c0 modsecurity.tar.gz
    download https://github.com/owasp-modsecurity/ModSecurity-nginx/releases/download/v1.0.4/ModSecurity-nginx-v1.0.4.tar.gz 6bdc7570911be884c1e43aaf85046137f9fde0cfa0dd4a55b853c81c45a13313 connector.tar.gz
    tar -xzf "$FEATURE_TMP/modsecurity.tar.gz" -C "$FEATURE_TMP"
    tar -xzf "$FEATURE_TMP/connector.tar.gz" -C "$FEATURE_TMP"
    (cd "$FEATURE_TMP/modsecurity-v3.0.16"; PKG_CONFIG_PATH=/usr/local/modsecurity/share/pkgconfig:${PKG_CONFIG_PATH:-} ./configure --prefix=/usr/local/modsecurity --with-yajl=/usr/local/modsecurity --disable-doxygen-doc; make -j2; make install)
    local nginx_source
    # Consume the complete rpm file list instead of exiting awk after the first
    # match. With pipefail enabled, an early awk exit can SIGPIPE rpm and make a
    # successful source lookup appear to have failed.
    nginx_source=$(rpm -ql nginx-mod-devel | awk '/\/configure$/ && !found {sub(/\/configure$/,"",$0);print;found=1}')
    [[ -n $nginx_source && -d $nginx_source ]] || { echo 'Distro Nginx source tree not found.' >&2; return 1; }
    cp -a "$nginx_source" "$FEATURE_TMP/nginx"
    (cd "$FEATURE_TMP/nginx"; MODSECURITY_INC=/usr/local/modsecurity/include MODSECURITY_LIB=/usr/local/modsecurity/lib ./configure --with-compat --with-http_ssl_module --add-dynamic-module="$FEATURE_TMP/ModSecurity-nginx-v1.0.4"; make -j2 modules)
    install -d -m 0755 /usr/lib64/nginx/modules
    install -m 0755 "$FEATURE_TMP/nginx/objs/ngx_http_modsecurity_module.so" /usr/lib64/nginx/modules/ngx_http_modsecurity_module.so
    printf '/usr/local/modsecurity/lib\n' > /etc/ld.so.conf.d/litecp-modsecurity.conf
    ldconfig
    printf 'load_module /usr/lib64/nginx/modules/ngx_http_modsecurity_module.so;\n' > /usr/share/nginx/modules/mod-modsecurity.conf
  fi
  download https://github.com/coreruleset/coreruleset/releases/download/v4.29.0/coreruleset-4.29.0-minimal.tar.gz 1aa1c5c8fc29e532d35293bcea36bf72de61db8f6ed4716a0f91ab14552b7fed crs.tar.gz
  install -d -m 0755 /etc/litecp/waf
  mkdir "$FEATURE_TMP/crs"
  tar -xzf "$FEATURE_TMP/crs.tar.gz" -C "$FEATURE_TMP/crs" --strip-components=1
  cp -a --no-preserve=ownership,context "$FEATURE_TMP/crs/rules" /etc/litecp/waf/
  [[ -e /etc/litecp/waf/crs-setup.conf ]] || install -m 0644 "$FEATURE_TMP/crs/crs-setup.conf.example" /etc/litecp/waf/crs-setup.conf
  if [[ ! -e /etc/litecp/waf/modsecurity.conf ]]; then
    cat > /etc/litecp/waf/modsecurity.conf <<'EOF'
SecRuleEngine DetectionOnly
SecRequestBodyAccess On
SecRequestBodyLimit 536870912
SecRequestBodyNoFilesLimit 1048576
SecRequestBodyLimitAction Reject
SecResponseBodyAccess Off
SecTmpDir /tmp
SecDataDir /tmp
SecAuditEngine RelevantOnly
SecAuditLogRelevantStatus "^(?:5|4(?!04))"
SecAuditLogParts AEFHZ
SecAuditLogType Serial
SecArgumentSeparator &
SecCookieFormat 0
SecRule REQUEST_HEADERS:Content-Type "@rx ^application/(?:[a-z0-9.+-]+\+)?json" "id:200001,phase:1,pass,nolog,ctl:requestBodyProcessor=JSON"
SecRule REQUEST_HEADERS:Content-Type "@rx (?:application/(?:soap\+)?xml|text/xml)" "id:200002,phase:1,pass,nolog,ctl:requestBodyProcessor=XML"
SecRule REQBODY_ERROR "!@eq 0" "id:200003,phase:2,deny,status:400,log,msg:'Request body parsing failed'"
EOF
  fi
  # The distribution Nginx policy already rotates /var/log/nginx/*.log.
  # Remove the older overlapping policy, which makes logrotate reject both.
  rm -f /etc/logrotate.d/litecp-waf
  bash "$(dirname -- "${BASH_SOURCE[0]}")/repair-waf-labels.sh"
  nginx -t
  systemctl reload nginx
  echo 'WAF engine installed. Enable Detect per website, tune false positives, then choose Block. Rebuild the AlmaLinux connector after a Nginx ABI upgrade.'
}

for feature in "$@"; do "install_$feature"; done
echo "Optional feature installation finished. Configuration backup: $BACKUP"
