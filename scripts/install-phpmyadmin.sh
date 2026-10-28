#!/usr/bin/env bash
set -euo pipefail
[[ $(id -u) == 0 ]] || { echo 'Run as root.' >&2; exit 1; }
# Install the verified official stable release; retain downloads for review.
PMA_VERSION=5.2.3
ROOT=/usr/share/litecp-phpmyadmin
STATE=/var/lib/litecp-phpmyadmin
DOWNLOAD=/var/cache/litecp-phpmyadmin
. /etc/os-release
if [[ ${ID} == ubuntu ]]; then
 PHP_BIN=/usr/bin/php8.3; PHP_UNIT=php8.3-fpm; POOL=/etc/php/8.3/fpm/pool.d/litecp-phpmyadmin.conf
 SOCKET=/run/php/litecp-phpmyadmin.sock; WEB_USER=www-data
 NGINX=/etc/nginx/sites-enabled/litecp-phpmyadmin.conf
else
 PHP_BIN=/opt/remi/php83/root/usr/bin/php; PHP_UNIT=php83-php-fpm; POOL=/etc/opt/remi/php83/php-fpm.d/litecp-phpmyadmin.conf
 SOCKET=/var/opt/remi/php83/run/php-fpm/litecp-phpmyadmin.sock; WEB_USER=nginx
 NGINX=/etc/nginx/conf.d/litecp-phpmyadmin.conf
fi
[[ -x ${PHP_BIN} ]] || { echo 'PHP 8.3 must be installed first.' >&2; exit 1; }
"${PHP_BIN}" -r 'foreach (["mysqli","mbstring","session","xml","ctype","json"] as $m) {if (!extension_loaded($m)) {fwrite(STDERR,"Missing PHP extension: ".$m."\n");exit(1);}}'
mkdir -p "${DOWNLOAD}" "${ROOT}" "${STATE}/sessions" "${STATE}/tmp"
chmod 700 "${STATE}" "${STATE}/sessions" "${STATE}/tmp"
chown -R "${WEB_USER}:${WEB_USER}" "${STATE}"
if [[ ! -f ${ROOT}/index.php ]]; then
 ARCHIVE="phpMyAdmin-${PMA_VERSION}-all-languages.tar.gz"
 curl --fail --location --proto '=https' --tlsv1.2 "https://files.phpmyadmin.net/phpMyAdmin/${PMA_VERSION}/${ARCHIVE}" -o "${DOWNLOAD}/${ARCHIVE}"
 curl --fail --location --proto '=https' --tlsv1.2 "https://files.phpmyadmin.net/phpMyAdmin/${PMA_VERSION}/${ARCHIVE}.sha256" -o "${DOWNLOAD}/${ARCHIVE}.sha256"
 (cd "${DOWNLOAD}" && sha256sum -c "${ARCHIVE}.sha256")
 tar -xzf "${DOWNLOAD}/${ARCHIVE}" --strip-components=1 -C "${ROOT}"
fi
# Installation may run with umask 077. Allow the web service to traverse the
# application root while keeping configuration and session data restricted.
chown "root:${WEB_USER}" "${ROOT}"
chmod 750 "${ROOT}"
if [[ ! -f ${ROOT}/config.inc.php ]]; then
 SECRET=$(openssl rand -hex 16)
 cat > "${ROOT}/config.inc.php" <<'PHP'
<?php
$cfg['blowfish_secret'] = '@SECRET@';
$cfg['PmaAbsoluteUri'] = ($_SERVER['HTTP_X_FORWARDED_PROTO'] === 'https' ? 'https' : 'http') . '://' . $_SERVER['HTTP_X_FORWARDED_HOST'] . '/api/phpmyadmin/';
$cfg['Servers'][1]['auth_type'] = 'cookie';
$cfg['Servers'][1]['host'] = 'localhost';
$cfg['Servers'][1]['AllowRoot'] = false;
$cfg['Servers'][1]['AllowNoPassword'] = false;
$cfg['AllowArbitraryServer'] = false;
$cfg['AllowUserDropDatabase'] = false;
$cfg['TempDir'] = '/var/lib/litecp-phpmyadmin/tmp';
$cfg['LoginCookieValidity'] = 1800;
$cfg['CookieSameSite'] = 'Strict';
$cfg['VersionCheck'] = false;
$cfg['SendErrorReports'] = 'never';
PHP
 sed -i "s/@SECRET@/${SECRET}/" "${ROOT}/config.inc.php"
 chown "root:${WEB_USER}" "${ROOT}/config.inc.php"
 chmod 640 "${ROOT}/config.inc.php"
fi
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cp "${SCRIPT_DIR}/../deploy/phpmyadmin-signon.php" "${ROOT}/sign-on.php"
chmod 644 "${ROOT}/sign-on.php"
if ! grep -q "LiteCPSignon" "${ROOT}/config.inc.php"; then
 cat >> "${ROOT}/config.inc.php" <<'PHP'
$cfg['Servers'][1]['auth_type'] = 'signon';
$cfg['Servers'][1]['SignonSession'] = 'LiteCPSignon';
$cfg['Servers'][1]['SignonCookieParams'] = ['lifetime'=>0,'path'=>'/api/phpmyadmin/','domain'=>'','secure'=>true,'httponly'=>true,'samesite'=>'Strict'];
$cfg['Servers'][1]['SignonURL'] = $cfg['PmaAbsoluteUri'].'sign-on.php';
$cfg['Servers'][1]['LogoutURL'] = '/#databases';
PHP
fi
cat > "${POOL}" <<EOF
[litecp_phpmyadmin]
user = ${WEB_USER}
group = ${WEB_USER}
listen = ${SOCKET}
listen.owner = ${WEB_USER}
listen.group = ${WEB_USER}
listen.mode = 0600
pm = ondemand
pm.max_children = 2
pm.process_idle_timeout = 10s
pm.max_requests = 200
request_terminate_timeout = 2500s
clear_env = yes
security.limit_extensions = .php
php_admin_value[session.save_path] = ${STATE}/sessions
php_admin_value[session.cookie_httponly] = 1
php_admin_value[session.cookie_secure] = 1
php_admin_value[upload_tmp_dir] = ${STATE}/tmp
php_admin_value[upload_max_filesize] = 5120M
php_admin_value[post_max_size] = 5122M
php_admin_value[memory_limit] = 192M
php_admin_value[max_execution_time] = 2400
php_admin_value[max_input_time] = 2700
EOF
cat > "${NGINX}" <<EOF
# Internal only. External requests must pass LiteCP session authentication.
server {
 listen 127.0.0.1:9091;
 server_name localhost;
 root ${ROOT};
 index index.php;
 client_max_body_size 5122m;
 location / { try_files \$uri \$uri/ =404; }
 location ~ ^/(setup|test|tests|doc|examples|libraries|templates|vendor)/ { deny all; }
 location ~ /\. { deny all; }
 location ~ \.php\$ {
  try_files \$uri =404;
  include fastcgi_params;
  fastcgi_param SCRIPT_FILENAME \$document_root\$fastcgi_script_name;
  fastcgi_param HTTP_PROXY "";
  fastcgi_param HTTPS on;
  fastcgi_pass unix:${SOCKET};
  fastcgi_read_timeout 2600s;
 }
}
EOF
PANEL_PROXY=/etc/nginx/conf.d/litecp-panel.conf
[[ $ID != ubuntu ]] || PANEL_PROXY=/etc/nginx/sites-available/litecp-panel.conf
if [[ -f $PANEL_PROXY ]]; then
 bash "${SCRIPT_DIR}/configure-phpmyadmin-uploads.sh" "$PANEL_PROXY"
fi
"${PHP_BIN}" -n -l "${ROOT}/config.inc.php"
nginx -t
systemctl reload "${PHP_UNIT}"
systemctl reload nginx
echo "phpMyAdmin ${PMA_VERSION} installed behind LiteCP authentication. Open a managed database from LiteCP for automatic sign-in."
