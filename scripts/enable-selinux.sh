#!/usr/bin/env bash
# Run once LiteCP and its hosting services are ready for health checks.
set -euo pipefail
[[ "$EUID" -eq 0 ]] || { echo 'Run as root.' >&2; exit 1; }
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
installed_version="${1:-}"
exec 9>/run/litecp-selinux.lock
flock -n 9 || { echo 'Another SELinux change is running.' >&2; exit 1; }
previous="$(getenforce)"
case "$previous" in
  Permissive|Enforcing) ;;
  *) echo "SELinux must be active at boot before enforcement can be enabled (current mode: $previous)." >&2; exit 1 ;;
esac
config=/etc/selinux/config
grep -Eq '^SELINUX=(permissive|enforcing)$' "$config" || { echo 'Unrecognized SELinux configuration.' >&2; exit 1; }
echo '[1/4] Preparing WAF labels and configuration backup...'
if command -v selinuxenabled >/dev/null && selinuxenabled; then
  command -v semanage >/dev/null || { echo 'SELinux management tools are required to configure LiteCP HTTP ports.' >&2; exit 1; }
  for port in 4321 9091; do
    echo "Configuring LiteCP HTTP port $port for SELinux..."
    semanage port -a -t http_port_t -p tcp "$port" 2>/dev/null || semanage port -m -t http_port_t -p tcp "$port"
  done
fi
bash "$script_dir/repair-waf-labels.sh"
bash "$script_dir/repair-php73-selinux.sh"
backup="/var/backups/litecp/selinux-$(date -u +%Y%m%dT%H%M%S)-$$.config"
install -d -m 0700 /var/backups/litecp
cp -p "$config" "$backup"
committed=0
guard="litecp-selinux-rollback-$(date -u +%Y%m%dT%H%M%S)-$$"
guard_started=0
previous_mode=0
[[ "$previous" != Enforcing ]] || previous_mode=1
rollback() {
  if [[ "$committed" == 0 ]]; then
    cp -p "$backup" "$config"
    setenforce "$previous_mode"
    echo 'Verification failed; previous SELinux configuration restored.' >&2
  fi
  if [[ "$guard_started" == 1 ]]; then systemctl stop "$guard.timer" || true; fi
}
trap rollback EXIT
active=()
for unit in litecp litecp-broker litecp-broker.socket nginx mysql mysqld postfix dovecot opendkim litecp-ftps litecp-php73-fpm named "${@:2}"; do
  if systemctl is-active --quiet "$unit"; then active+=("$unit"); fi
done
# This guard survives SSH loss, a killed shell, or a panel failure. Keep its
# configuration backup; do not rely only on the shell's EXIT trap for recovery.
systemd-run --unit="$guard" --on-active=5m /bin/bash -c \
  'cp -p -- "$1" "$2" && "$3" "$4"' \
  litecp-selinux-rollback "$backup" "$config" "$(command -v setenforce)" "$previous_mode"
guard_started=1
setenforce 1
echo '[2/4] Enforcement enabled with an independent rollback guard.'
[[ "$(getenforce)" == Enforcing ]]
echo '[3/4] Checking Nginx, panel response and active services...'
if systemctl is-active --quiet nginx; then
  nginx -t -e stderr || { echo 'Nginx configuration verification failed.' >&2; exit 1; }
  systemctl reload nginx || { echo 'Nginx reload failed under SELinux enforcement.' >&2; exit 1; }
fi
if [[ -n "$installed_version" ]]; then
  bash "$script_dir/check-privilege-services.sh" "$installed_version" || { echo 'LiteCP service verification failed under SELinux enforcement.' >&2; exit 1; }
else
  curl --fail --silent --show-error --max-time 15 http://127.0.0.1:9090/api/version >/dev/null || { echo 'Local panel health check failed.' >&2; exit 1; }
fi
for unit in "${active[@]}"; do systemctl is-active --quiet "$unit" || { echo "Previously active service $unit failed verification." >&2; exit 1; }; done
echo '[4/4] Saving persistent enforcement configuration...'
sed -i -E 's/^SELINUX=(permissive|enforcing)$/SELINUX=enforcing/' "$config"
grep -qx 'SELINUX=enforcing' "$config"
systemctl stop "$guard.timer"
committed=1
printf 'SELinux is enforcing and configured to remain enforcing after reboot. Previous configuration: %s\n' "$backup"
