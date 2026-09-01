#!/usr/bin/env bash
set -Eeuo pipefail

LITECP_VERSION="0.2.0"
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

info "Installing the lean WHMCS and WordPress runtime for ${PRETTY_NAME}"

REQUESTED_PHP_VERSIONS=("7.3" "7.4" "8.1" "8.2" "8.3" "8.4")
PHP_VERSIONS=()
PHP_UNITS=()

if [[ "${OS_ID}" == "ubuntu" ]]; then
  export DEBIAN_FRONTEND=noninteractive
  apt-get update
  apt-get install -y ca-certificates curl gnupg unzip software-properties-common openssl nginx mariadb-server certbot python3-certbot-nginx cron golang-go ufw
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
  dnf install -y ca-certificates curl unzip openssl nginx mariadb-server certbot python3-certbot-nginx cronie golang policycoreutils-python-utils firewalld
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
install -d -m 0750 /etc/litecp /etc/litecp/tls

if [[ "${OS_ID}" == "ubuntu" ]]; then
  MARIADB_CONFIG=/etc/mysql/mariadb.conf.d/99-litecp.cnf
else
  MARIADB_CONFIG=/etc/my.cnf.d/litecp.cnf
fi
cat > "${MARIADB_CONFIG}" <<'EOF'
# LiteCP only connects to MariaDB locally. Never expose WHMCS data publicly.
[mariadb]
bind-address=127.0.0.1
skip-name-resolve
EOF
chmod 0644 "${MARIADB_CONFIG}"
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
        proxy_read_timeout 90s;
        client_max_body_size 1m;
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

nginx -t
systemctl daemon-reload
systemctl enable --now nginx mariadb "${PHP_UNITS[@]}" "${CRON_UNIT}" litecp
systemctl restart "${PHP_UNITS[@]}" litecp
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
  ufw --force enable >/dev/null
elif systemctl is-active --quiet firewalld; then
  firewall-cmd --permanent --zone=public --remove-service=cockpit >/dev/null 2>&1 || true
  firewall-cmd --permanent --zone=public --add-service=ssh >/dev/null
  firewall-cmd --permanent --zone=public --add-service=http >/dev/null
  firewall-cmd --permanent --zone=public --add-service=https >/dev/null
  firewall-cmd --permanent --zone=public --add-port=8443/tcp >/dev/null
  firewall-cmd --reload >/dev/null
else
  firewall-offline-cmd --zone=public --remove-service=cockpit >/dev/null 2>&1 || true
  firewall-offline-cmd --zone=public --add-service=ssh >/dev/null
  firewall-offline-cmd --zone=public --add-service=http >/dev/null
  firewall-offline-cmd --zone=public --add-service=https >/dev/null
  firewall-offline-cmd --zone=public --add-port=8443/tcp >/dev/null
  systemctl enable --now firewalld
fi

SERVER_IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
success "LiteCP - By Hostingtom is installed."
printf '\nOpen: \033[1mhttps://%s:8443\033[0m\nUser: \033[1m%s\033[0m\n\n' "${SERVER_IP:-SERVER_IP}" "${ADMIN_USERNAME}"
printf 'The first visit uses a self-signed panel certificate. Verify its fingerprint or replace it with a trusted certificate before regular use.\n'
printf 'If an external cloud firewall is enabled, allow TCP ports 80, 443 and 8443.\n'
