#!/usr/bin/env bash
# Run after application acceptance checks in enforcing mode, not during bootstrap.
set -euo pipefail
[[ "$EUID" -eq 0 ]] || { echo 'Run as root.' >&2; exit 1; }
exec 9>/run/litecp-selinux.lock
flock -n 9 || { echo 'Another SELinux change is running.' >&2; exit 1; }
[[ "$(getenforce)" != Disabled ]] || { echo 'SELinux is disabled; enable and relabel it before using this script.' >&2; exit 1; }
echo '[1/4] Preparing WAF labels and configuration backup...'
bash "$(dirname -- "${BASH_SOURCE[0]}")/repair-waf-labels.sh"
config=/etc/selinux/config
grep -Eq '^SELINUX=(permissive|enforcing)$' "$config" || { echo 'Unrecognized SELinux configuration.' >&2; exit 1; }
backup="/var/backups/litecp/selinux-$(date -u +%Y%m%dT%H%M%S)-$$.config"
install -d -m 0700 /var/backups/litecp
cp -p "$config" "$backup"
previous="$(getenforce)"
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
for unit in litecp litecp-broker nginx mysqld postfix dovecot opendkim litecp-ftps; do
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
if systemctl is-active --quiet nginx; then nginx -t; systemctl reload nginx; fi
curl --fail --silent --show-error --max-time 15 http://127.0.0.1:9090/api/version >/dev/null
for unit in "${active[@]}"; do systemctl is-active --quiet "$unit"; done
echo '[4/4] Saving persistent enforcement configuration...'
sed -i -E 's/^SELINUX=(permissive|enforcing)$/SELINUX=enforcing/' "$config"
grep -qx 'SELINUX=enforcing' "$config"
systemctl stop "$guard.timer"
committed=1
printf 'SELinux is enforcing and configured to remain enforcing after reboot. Previous configuration: %s\n' "$backup"
