#!/usr/bin/env bash
set -Eeuo pipefail

LITECP_VERSION="0.3.0"
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
SOURCE_DIR="$(cd -- "${SCRIPT_DIR}/.." >/dev/null 2>&1 && pwd)"

info() { printf '\033[1;36m›\033[0m %s\n' "$*"; }
success() { printf '\033[1;32m✓\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31mError:\033[0m %s\n' "$*" >&2; exit 1; }

[[ "$(id -u)" -eq 0 ]] || fail "Run this installer as root."
[[ -r /etc/os-release ]] || fail "Cannot identify this Linux distribution."
# shellcheck disable=SC1091
source /etc/os-release
OS_ID="${ID,,}"
[[ "${OS_ID}" == "ubuntu" || "${OS_ID}" == "almalinux" ]] || fail "LiteCP supports Ubuntu and AlmaLinux. Detected: ${PRETTY_NAME:-$OS_ID}."
[[ -f "${SOURCE_DIR}/go.mod" && -f "${SOURCE_DIR}/deploy/litecp.service" ]] || fail "Run this script from a complete LiteCP source checkout."

DATABASE_ENGINE="${LITECP_DATABASE_ENGINE:-}"
DATABASE_ENGINE="${DATABASE_ENGINE,,}"
if systemctl list-unit-files mariadb.service --no-legend 2>/dev/null | grep -q '^mariadb.service'; then
  EXISTING_DATABASE_ENGINE="mariadb"
elif systemctl list-unit-files mysql.service mysqld.service --no-legend 2>/dev/null | grep -Eq '^(mysql|mysqld)\.service'; then
  EXISTING_DATABASE_ENGINE="mysql"
else
  EXISTING_DATABASE_ENGINE=""
fi
DATABASE_ENGINE="${DATABASE_ENGINE:-${EXISTING_DATABASE_ENGINE:-mariadb}}"
[[ "${DATABASE_ENGINE}" == "mariadb" || "${DATABASE_ENGINE}" == "mysql" ]] || fail "LITECP_DATABASE_ENGINE must be 'mariadb' or 'mysql'."
if [[ -n "${EXISTING_DATABASE_ENGINE}" && "${DATABASE_ENGINE}" != "${EXISTING_DATABASE_ENGINE}" ]]; then
  fail "${EXISTING_DATABASE_ENGINE} is already installed. LiteCP will not replace an existing database engine automatically. Rerun with LITECP_DATABASE_ENGINE=${EXISTING_DATABASE_ENGINE}."
fi

if [[ "${DATABASE_ENGINE}" == "mysql" ]]; then
  DATABASE_PACKAGE="mysql-server"
  [[ "${OS_ID}" == "ubuntu" ]] && DATABASE_UNIT="mysql" || DATABASE_UNIT="mysqld"
else
  DATABASE_PACKAGE="mariadb-server"
  DATABASE_UNIT="mariadb"
fi

info "Installing the lean WHMCS and WordPress runtime with ${DATABASE_ENGINE} for ${PRETTY_NAME}"

REQUESTED_PHP_VERSIONS=("7.3" "7.4" "8.1" "8.2" "8.3" "8.4")
PHP_VERSIONS=()
PHP_UNITS=()

if [[ "${OS_ID}" == "ubuntu" ]]; then
  export DEBIAN_FRONTEND=noninteractive
  apt-get update
  printf 'postfix postfix/mailname string %s\npostfix postfix/main_mailer_type select Internet Site\n' "$(hostname -f)" | debconf-set-selections
  apt-get install -y ca-certificates curl gnupg unzip software-properties-common openssl nginx "${DATABASE_PACKAGE}" certbot python3-certbot-nginx cron golang-go ufw postfix dovecot-core dovecot-imapd dovecot-lmtpd
  add-apt-repository -y ppa:ondrej/php
  apt-get update
  PHP_PACKAGES=()
  for PHP_VERSION in "${REQUESTED_PHP_VERSIONS[@]}"; do
    PHP_VERSIONS+=("${PHP_VERSION}")
    PHP_UNITS+=("php${PHP_VERSION}-fpm")
    for PHP_PACKAGE in fpm cli common mysql curl gd imap mbstring xml intl bcmath gmp soap zip opcache; do
      PHP_PACKAGES+=("php${PHP_VERSION}-${PHP_PACKAGE}")
    done
  done
  apt-get install -y "${PHP_PACKAGES[@]}"
  CRON_UNIT="cron"
else
  EL_MAJOR="$(rpm -E '%{rhel}')"
  dnf install -y epel-release
  dnf install -y ca-certificates curl unzip openssl nginx "${DATABASE_PACKAGE}" certbot python3-certbot-nginx cronie golang policycoreutils-python-utils firewalld postfix dovecot
  if ! rpm -q remi-release >/dev/null 2>&1; then
    dnf install -y "https://rpms.remirepo.net/enterprise/remi-release-${EL_MAJOR}.rpm"
  fi
  PHP_PACKAGES=()
  for PHP_VERSION in "${REQUESTED_PHP_VERSIONS[@]}"; do
    PHP_COMPACT="${PHP_VERSION/.}"
    if ! dnf repoquery --available "php${PHP_COMPACT}-php-fpm" 2>/dev/null | grep -q .; then
      info "PHP ${PHP_VERSION} is unavailable as a native package on ${PRETTY_NAME}; skipping it."
      continue
    fi
    PHP_VERSIONS+=("${PHP_VERSION}")
    PHP_UNITS+=("php${PHP_COMPACT}-php-fpm")
    for PHP_PACKAGE in fpm cli common mysqlnd gd mbstring xml intl bcmath gmp soap opcache process; do
      PHP_PACKAGES+=("php${PHP_COMPACT}-php-${PHP_PACKAGE}")
    done
    PHP_PACKAGES+=("php${PHP_COMPACT}-php-pecl-zip")
    if dnf repoquery --available "php${PHP_COMPACT}-php-imap" 2>/dev/null | grep -q .; then
      PHP_PACKAGES+=("php${PHP_COMPACT}-php-imap")
    else
      PHP_PACKAGES+=("php${PHP_COMPACT}-php-pecl-imap")
    fi
  done
  ((${#PHP_VERSIONS[@]} > 0)) || fail "No supported parallel PHP-FPM packages are available."
  dnf install -y "${PHP_PACKAGES[@]}"
  CRON_UNIT="crond"
fi

info "Installing ionCube Loader and low-memory pools for each PHP runtime"
MACHINE_ARCH="$(uname -m)"
case "${MACHINE_ARCH}" in
  x86_64|amd64) IONCUBE_ARCH="x86-64" ;;
  aarch64|arm64) IONCUBE_ARCH="aarch64" ;;
  *) fail "ionCube automatic installation supports x86_64 and aarch64; detected ${MACHINE_ARCH}." ;;
esac
IONCUBE_TMP="$(mktemp -d)"
trap 'rm -rf -- "${IONCUBE_TMP}"' EXIT
curl --fail --location --proto '=https' --tlsv1.2 --retry 3 "https://downloads.ioncube.com/loader_downloads/ioncube_loaders_lin_${IONCUBE_ARCH}.tar.gz" --output "${IONCUBE_TMP}/ioncube.tar.gz"
tar -xzf "${IONCUBE_TMP}/ioncube.tar.gz" -C "${IONCUBE_TMP}"
for PHP_VERSION in "${PHP_VERSIONS[@]}"; do
  PHP_COMPACT="${PHP_VERSION/.}"
  IONCUBE_LOADER="${IONCUBE_TMP}/ioncube/ioncube_loader_lin_${PHP_VERSION}.so"
  [[ -f "${IONCUBE_LOADER}" ]] || fail "ionCube Loader does not include PHP ${PHP_VERSION} support for ${MACHINE_ARCH}."
  if [[ "${OS_ID}" == "ubuntu" ]]; then
    PHP_BINARY="/usr/bin/php${PHP_VERSION}"
    PHP_EXTENSION_DIR="$(${PHP_BINARY} -r 'echo ini_get("extension_dir");')"
    install -m 0644 "${IONCUBE_LOADER}" "${PHP_EXTENSION_DIR}/ioncube_loader_lin_${PHP_VERSION}.so"
    printf 'zend_extension=%s/ioncube_loader_lin_%s.so\n' "${PHP_EXTENSION_DIR}" "${PHP_VERSION}" > "/etc/php/${PHP_VERSION}/mods-available/ioncube.ini"
    phpenmod -v "${PHP_VERSION}" ioncube
    install -m 0644 "${SOURCE_DIR}/deploy/php-litecp.ini" "/etc/php/${PHP_VERSION}/mods-available/litecp.ini"
    phpenmod -v "${PHP_VERSION}" litecp
    PHP_SOCKET="/run/php/php${PHP_VERSION}-fpm.sock"
    sed "s#@PHP_SOCKET@#${PHP_SOCKET}#g" "${SOURCE_DIR}/deploy/fpm-ubuntu.conf" > "/etc/php/${PHP_VERSION}/fpm/pool.d/zz-litecp.conf"
  else
    PHP_BINARY="/opt/remi/php${PHP_COMPACT}/root/usr/bin/php"
    PHP_EXTENSION_DIR="$(${PHP_BINARY} -r 'echo ini_get("extension_dir");')"
    install -m 0644 "${IONCUBE_LOADER}" "${PHP_EXTENSION_DIR}/ioncube_loader_lin_${PHP_VERSION}.so"
    printf 'zend_extension=%s/ioncube_loader_lin_%s.so\n' "${PHP_EXTENSION_DIR}" "${PHP_VERSION}" > "/etc/opt/remi/php${PHP_COMPACT}/php.d/00-ioncube.ini"
    install -m 0644 "${SOURCE_DIR}/deploy/php-litecp.ini" "/etc/opt/remi/php${PHP_COMPACT}/php.d/99-litecp.ini"
    PHP_SOCKET="/var/opt/remi/php${PHP_COMPACT}/run/php-fpm/www.sock"
    sed "s#@PHP_SOCKET@#${PHP_SOCKET}#g" "${SOURCE_DIR}/deploy/fpm-almalinux.conf" > "/etc/opt/remi/php${PHP_COMPACT}/php-fpm.d/zz-litecp.conf"
  fi
  chmod 0644 "${PHP_EXTENSION_DIR}/ioncube_loader_lin_${PHP_VERSION}.so"
  "${PHP_BINARY}" -v | grep -qi ioncube || fail "ionCube Loader did not activate for PHP ${PHP_VERSION}."
done

info "Building the LiteCP single-binary service"
install -d -m 0700 /var/lib/litecp /var/backups/litecp
install -d -m 0755 /var/www/litecp
install -d -m 0750 /etc/litecp /etc/litecp/tls /etc/litecp/mail

if [[ "${OS_ID}" == "ubuntu" && "${DATABASE_ENGINE}" == "mariadb" ]]; then
  DATABASE_CONFIG=/etc/mysql/mariadb.conf.d/99-litecp.cnf
elif [[ "${OS_ID}" == "ubuntu" ]]; then
  DATABASE_CONFIG=/etc/mysql/mysql.conf.d/99-litecp.cnf
else
  DATABASE_CONFIG=/etc/my.cnf.d/litecp.cnf
fi
DATABASE_SECTION="mariadb"
[[ "${DATABASE_ENGINE}" == "mysql" ]] && DATABASE_SECTION="mysqld"
cat > "${DATABASE_CONFIG}" <<EOF
# LiteCP only connects to the database locally. Never expose application data publicly.
[${DATABASE_SECTION}]
bind-address=127.0.0.1
skip-name-resolve
EOF
chmod 0644 "${DATABASE_CONFIG}"
cd "${SOURCE_DIR}"
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${LITECP_VERSION}" -o /usr/local/bin/litecp .
chmod 0755 /usr/local/bin/litecp
command -v restorecon >/dev/null 2>&1 && restorecon /usr/local/bin/litecp || true

ADMIN_USERNAME="${LITECP_ADMIN_USERNAME:-admin}"
ADMIN_PASSWORD="${LITECP_ADMIN_PASSWORD:-}"
if [[ ! -f /var/lib/litecp/config.json ]]; then
  if [[ -z "${ADMIN_PASSWORD}" ]]; then
    [[ -t 0 ]] || fail "Set LITECP_ADMIN_PASSWORD (12+ characters) for a non-interactive install."
    read -r -p "LiteCP administrator username [admin]: " INPUT_USERNAME
    ADMIN_USERNAME="${INPUT_USERNAME:-admin}"
    while true; do
      read -r -s -p "LiteCP administrator password (12+ characters): " ADMIN_PASSWORD
      printf '\n'
      [[ ${#ADMIN_PASSWORD} -ge 12 ]] || { info "The password must contain at least 12 characters."; continue; }
      read -r -s -p "Confirm administrator password: " ADMIN_PASSWORD_CONFIRM
      printf '\n'
      [[ "${ADMIN_PASSWORD}" == "${ADMIN_PASSWORD_CONFIRM}" ]] && break
      info "Passwords did not match; try again."
    done
  fi
  [[ ${#ADMIN_PASSWORD} -ge 12 ]] || fail "The administrator password must contain at least 12 characters."
  LITECP_DATA_DIR=/var/lib/litecp LITECP_ADMIN_PASSWORD="${ADMIN_PASSWORD}" /usr/local/bin/litecp init --username "${ADMIN_USERNAME}"
else
  ADMIN_USERNAME="existing administrator"
  info "Existing LiteCP configuration found; administrator credentials were preserved."
fi
unset ADMIN_PASSWORD LITECP_ADMIN_PASSWORD

cat > /etc/litecp/litecp.env <<'EOF'
LITECP_DATA_DIR=/var/lib/litecp
LITECP_BIND=127.0.0.1:9090
EOF
chmod 0600 /etc/litecp/litecp.env
install -m 0644 "${SOURCE_DIR}/deploy/litecp.service" /etc/systemd/system/litecp.service

if [[ ! -f /etc/litecp/tls/litecp.key ]]; then
  openssl req -x509 -nodes -newkey rsa:3072 -days 825 -keyout /etc/litecp/tls/litecp.key -out /etc/litecp/tls/litecp.crt -subj "/CN=LiteCP" >/dev/null 2>&1
  chmod 0600 /etc/litecp/tls/litecp.key
fi

PANEL_NGINX=$(cat <<'EOF'
# LiteCP panel HTTPS reverse proxy
server {
    listen 8443 ssl;
    listen [::]:8443 ssl;
    server_name _;

    ssl_certificate /etc/litecp/tls/litecp.crt;
    ssl_certificate_key /etc/litecp/tls/litecp.key;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_session_cache shared:LiteCPSSL:5m;
    ssl_session_timeout 1d;
    location / {
        proxy_pass http://127.0.0.1:9090;
        proxy_http_version 1.1;
        proxy_set_header Host $host:$server_port;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
        proxy_read_timeout 1200s;
        proxy_send_timeout 1200s;
        client_max_body_size 512m;
    }
}
EOF
)

if [[ "${OS_ID}" == "ubuntu" ]]; then
  PANEL_CONFIG=/etc/nginx/sites-available/litecp-panel.conf
  if [[ ! -f "${PANEL_CONFIG}" ]]; then
    printf '%s\n' "${PANEL_NGINX}" > "${PANEL_CONFIG}"
  fi
  ln -sfn /etc/nginx/sites-available/litecp-panel.conf /etc/nginx/sites-enabled/litecp-panel.conf
else
  PANEL_CONFIG=/etc/nginx/conf.d/litecp-panel.conf
  if [[ ! -f "${PANEL_CONFIG}" ]]; then
    printf '%s\n' "${PANEL_NGINX}" > "${PANEL_CONFIG}"
  fi
  setsebool -P httpd_can_network_connect 1 >/dev/null 2>&1 || true
  semanage port -a -t http_port_t -p tcp 8443 >/dev/null 2>&1 || semanage port -m -t http_port_t -p tcp 8443 >/dev/null 2>&1 || true
  semanage fcontext -a -t httpd_sys_rw_content_t '/var/www/litecp(/.*)?' >/dev/null 2>&1 || semanage fcontext -m -t httpd_sys_rw_content_t '/var/www/litecp(/.*)?' >/dev/null 2>&1
  semanage fcontext -a -t cert_t '/etc/litecp/tls(/.*)?' >/dev/null 2>&1 || semanage fcontext -m -t cert_t '/etc/litecp/tls(/.*)?' >/dev/null 2>&1
  restorecon -R /var/www/litecp /etc/litecp/tls
fi

# Preserve an existing trusted panel certificate while raising upload/time limits for the file manager.
if grep -q 'client_max_body_size' "${PANEL_CONFIG}"; then
  sed -i -E 's/[[:space:]]*client_max_body_size[[:space:]]+[^;]+;/        client_max_body_size 512m;/' "${PANEL_CONFIG}"
else
  sed -i '/proxy_pass/a\        client_max_body_size 512m;' "${PANEL_CONFIG}"
fi
if grep -q 'proxy_read_timeout' "${PANEL_CONFIG}"; then
  sed -i -E 's/[[:space:]]*proxy_read_timeout[[:space:]]+[^;]+;/        proxy_read_timeout 1200s;/' "${PANEL_CONFIG}"
else
  sed -i '/proxy_pass/a\        proxy_read_timeout 1200s;' "${PANEL_CONFIG}"
fi
if grep -q 'proxy_send_timeout' "${PANEL_CONFIG}"; then
  sed -i -E 's/[[:space:]]*proxy_send_timeout[[:space:]]+[^;]+;/        proxy_send_timeout 1200s;/' "${PANEL_CONFIG}"
else
  sed -i '/proxy_pass/a\        proxy_send_timeout 1200s;' "${PANEL_CONFIG}"
fi

info "Configuring lightweight Postfix and Dovecot email hosting"
getent group vmail >/dev/null 2>&1 || groupadd --system vmail
id -u vmail >/dev/null 2>&1 || useradd --system --gid vmail --home-dir /var/vmail --create-home --shell /sbin/nologin vmail
install -d -o vmail -g vmail -m 0750 /var/vmail
for MAIL_MAP in domains mailboxes aliases users; do
  touch "/etc/litecp/mail/${MAIL_MAP}"
done
chmod 0600 /etc/litecp/mail/users
chmod 0644 /etc/litecp/mail/domains /etc/litecp/mail/mailboxes /etc/litecp/mail/aliases
chown root:dovecot /etc/litecp /etc/litecp/mail
chmod 0750 /etc/litecp /etc/litecp/mail
chown dovecot:dovecot /etc/litecp/mail/users
if postconf -m | grep -qx hash; then
  MAIL_MAP_TYPE=hash
elif postconf -m | grep -qx lmdb; then
  MAIL_MAP_TYPE=lmdb
else
  fail "Postfix provides neither hash nor LMDB lookup maps."
fi
postmap "${MAIL_MAP_TYPE}:/etc/litecp/mail/domains"
postmap "${MAIL_MAP_TYPE}:/etc/litecp/mail/mailboxes"
postmap "${MAIL_MAP_TYPE}:/etc/litecp/mail/aliases"
DETECTED_PANEL_NAME="$(awk '$1 == "server_name" && $2 != "_;" {gsub(/;/, "", $2); print $2; exit}' "${PANEL_CONFIG}")"
MAIL_HOSTNAME="${LITECP_MAIL_HOSTNAME:-${DETECTED_PANEL_NAME:-$(hostname -f)}}"
[[ "${MAIL_HOSTNAME}" =~ ^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$ ]] || fail "LITECP_MAIL_HOSTNAME must be a valid hostname."
MAIL_TLS_CERT="$(awk '$1 == "ssl_certificate" {gsub(/;/, "", $2); print $2; exit}' "${PANEL_CONFIG}")"
MAIL_TLS_KEY="$(awk '$1 == "ssl_certificate_key" {gsub(/;/, "", $2); print $2; exit}' "${PANEL_CONFIG}")"
[[ -r "${MAIL_TLS_CERT}" && -r "${MAIL_TLS_KEY}" ]] || { MAIL_TLS_CERT=/etc/litecp/tls/litecp.crt; MAIL_TLS_KEY=/etc/litecp/tls/litecp.key; }
postconf -e "myhostname = ${MAIL_HOSTNAME}"
postconf -e 'mydestination = localhost, localhost.localdomain'
postconf -e 'inet_interfaces = all'
postconf -e 'virtual_mailbox_base = /var/vmail'
postconf -e "virtual_mailbox_domains = ${MAIL_MAP_TYPE}:/etc/litecp/mail/domains"
postconf -e "virtual_mailbox_maps = ${MAIL_MAP_TYPE}:/etc/litecp/mail/mailboxes"
postconf -e "virtual_alias_maps = ${MAIL_MAP_TYPE}:/etc/litecp/mail/aliases"
postconf -e 'virtual_transport = lmtp:unix:private/dovecot-lmtp'
postconf -e 'smtpd_sasl_type = dovecot'
postconf -e 'smtpd_sasl_path = private/auth'
postconf -e 'smtpd_sasl_auth_enable = yes'
postconf -e 'smtpd_recipient_restrictions = permit_mynetworks, permit_sasl_authenticated, reject_unauth_destination'
postconf -e 'smtpd_relay_restrictions = permit_mynetworks, permit_sasl_authenticated, reject_unauth_destination'
postconf -e 'smtpd_tls_security_level = may'
postconf -e 'smtpd_tls_auth_only = yes'
postconf -e 'smtp_tls_security_level = may'
postconf -e "smtpd_tls_cert_file = ${MAIL_TLS_CERT}"
postconf -e "smtpd_tls_key_file = ${MAIL_TLS_KEY}"
postconf -e 'message_size_limit = 52428800'
postconf -M 'submission/inet=submission inet n - y - - smtpd'
postconf -P 'submission/inet/syslog_name=postfix/submission'
postconf -P 'submission/inet/smtpd_tls_security_level=encrypt'
postconf -P 'submission/inet/smtpd_sasl_auth_enable=yes'
postconf -P 'submission/inet/smtpd_recipient_restrictions=permit_sasl_authenticated,reject'
postconf -M 'smtps/inet=smtps inet n - y - - smtpd'
postconf -P 'smtps/inet/syslog_name=postfix/smtps'
postconf -P 'smtps/inet/smtpd_tls_wrappermode=yes'
postconf -P 'smtps/inet/smtpd_sasl_auth_enable=yes'
postconf -P 'smtps/inet/smtpd_recipient_restrictions=permit_sasl_authenticated,reject'
sed -e "s#@VMAIL_UID@#$(id -u vmail)#g" -e "s#@TLS_CERT@#${MAIL_TLS_CERT}#g" -e "s#@TLS_KEY@#${MAIL_TLS_KEY}#g" "${SOURCE_DIR}/deploy/dovecot-litecp.conf" > /etc/dovecot/conf.d/99-litecp.conf
chmod 0644 /etc/dovecot/conf.d/99-litecp.conf
postfix check
doveconf -n >/dev/null

if [[ "${OS_ID}" == "almalinux" ]]; then
  semanage fcontext -a -t mail_spool_t '/var/vmail(/.*)?' >/dev/null 2>&1 || semanage fcontext -m -t mail_spool_t '/var/vmail(/.*)?' >/dev/null 2>&1 || true
  restorecon -R /var/vmail
fi

nginx -t
systemctl daemon-reload
systemctl enable --now nginx "${DATABASE_UNIT}" "${PHP_UNITS[@]}" "${CRON_UNIT}" postfix dovecot litecp
systemctl restart "${DATABASE_UNIT}" "${PHP_UNITS[@]}" postfix dovecot litecp
systemctl reload nginx
for CERTBOT_TIMER in certbot.timer certbot-renew.timer; do
  if systemctl list-unit-files "${CERTBOT_TIMER}" --no-legend 2>/dev/null | grep -q "^${CERTBOT_TIMER}"; then
    systemctl enable --now "${CERTBOT_TIMER}"
  fi
done

if [[ "${OS_ID}" == "ubuntu" ]]; then
  ufw allow 22/tcp >/dev/null
  ufw allow 80/tcp >/dev/null
  ufw allow 443/tcp >/dev/null
  ufw allow 8443/tcp >/dev/null
  ufw allow 25/tcp >/dev/null
  ufw allow 465/tcp >/dev/null
  ufw allow 587/tcp >/dev/null
  ufw allow 993/tcp >/dev/null
  ufw --force enable >/dev/null
elif systemctl is-active --quiet firewalld; then
  firewall-cmd --permanent --zone=public --remove-service=cockpit >/dev/null 2>&1 || true
  firewall-cmd --permanent --zone=public --add-service=ssh >/dev/null
  firewall-cmd --permanent --zone=public --add-service=http >/dev/null
  firewall-cmd --permanent --zone=public --add-service=https >/dev/null
  firewall-cmd --permanent --zone=public --add-port=8443/tcp >/dev/null
  firewall-cmd --permanent --zone=public --add-service=smtp >/dev/null
  firewall-cmd --permanent --zone=public --add-port=465/tcp >/dev/null
  firewall-cmd --permanent --zone=public --add-service=smtp-submission >/dev/null 2>&1 || firewall-cmd --permanent --zone=public --add-port=587/tcp >/dev/null
  firewall-cmd --permanent --zone=public --add-service=imaps >/dev/null
  firewall-cmd --reload >/dev/null
else
  firewall-offline-cmd --zone=public --remove-service=cockpit >/dev/null 2>&1 || true
  firewall-offline-cmd --zone=public --add-service=ssh >/dev/null
  firewall-offline-cmd --zone=public --add-service=http >/dev/null
  firewall-offline-cmd --zone=public --add-service=https >/dev/null
  firewall-offline-cmd --zone=public --add-port=8443/tcp >/dev/null
  firewall-offline-cmd --zone=public --add-service=smtp >/dev/null
  firewall-offline-cmd --zone=public --add-port=465/tcp >/dev/null
  firewall-offline-cmd --zone=public --add-port=587/tcp >/dev/null
  firewall-offline-cmd --zone=public --add-service=imaps >/dev/null
  systemctl enable --now firewalld
fi

SERVER_IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
success "LiteCP - By Hostingtom is installed."
printf '\nOpen: \033[1mhttps://%s:8443\033[0m\nUser: \033[1m%s\033[0m\n\n' "${SERVER_IP:-SERVER_IP}" "${ADMIN_USERNAME}"
printf 'The first visit uses a self-signed panel certificate. Verify its fingerprint or replace it with a trusted certificate before regular use.\n'
printf 'If an external cloud firewall is enabled, allow TCP 80, 443, 8443 and mail ports 25, 465, 587, 993 as needed.\n'
printf 'Database engine: %s (choose with LITECP_DATABASE_ENGINE=mariadb|mysql before a fresh install).\n' "${DATABASE_ENGINE}"
