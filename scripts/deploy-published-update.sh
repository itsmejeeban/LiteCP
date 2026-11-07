#!/usr/bin/env bash
# Called only by the user-run update.bat workflow. Keeps source and recovery binary.
set -Eeuo pipefail
version="$1"
revision="$2"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]
[[ "$revision" =~ ^[0-9a-f]{40}$ ]]
for tool in go systemctl install cp curl python3 flock; do command -v "$tool" >/dev/null; done
exec 9>/run/lock/litecp-panel-update.lock
flock -n 9 || { echo 'A panel update or rollback is already running.' >&2; exit 1; }
work="$PWD"
switched=0
snapshot_helper_switched=0
rollback() {
  trap - ERR
  if [[ "$switched" == 1 ]]; then
    echo 'Restoring the previous LiteCP binary...'
    systemctl stop litecp litecp-broker.service litecp-broker.socket || true
    if [[ -d "$work/service-recovery" ]]; then bash "$work/scripts/install-privilege-services.sh" --restore "$work/service-recovery"; fi
    if [[ "$snapshot_helper_switched" == 1 && -f "$work/previous-system-snapshot.py" ]]; then
      install -m 0700 "$work/previous-system-snapshot.py" /usr/local/lib/litecp/system-snapshot.py
    fi
    install -m 0755 "$work/previous-litecp" /usr/local/bin/litecp
    systemctl try-restart litecp-captcha.service || true
    systemctl restart litecp || true
  fi
  echo "UPDATE FAILED. Recovery files retained in $work"
  exit 1
}
trap rollback ERR
echo '[Server 1/4] Running Go tests...'
go test -p=1 ./...
echo '[Server 2/4] Building the published revision...'
CGO_ENABLED=0 go build -p=1 -mod=readonly -trimpath -ldflags "-s -w -X main.version=$version -X main.buildRevision=$revision" -o "$work/next-litecp" .
echo '[Server 3/4] Preserving current binary and restarting LiteCP...'
cp --preserve=mode /usr/local/bin/litecp "$work/previous-litecp"
install -d -m 0700 /usr/local/lib/litecp-updates
recovery="$(mktemp -d /usr/local/lib/litecp-updates/update-XXXXXXXX)"
cp --reflink=auto --preserve=mode "$work/previous-litecp" "$recovery/previous-litecp"
python3 "$work/scripts/write-panel-recovery-manifest.py" "$recovery"
bash "$work/scripts/install-privilege-services.sh" --prepare-only
systemctl stop litecp
switched=1
LITECP_SERVICE_BACKUP_DIR="$work/service-recovery" bash "$work/scripts/install-privilege-services.sh"
install -m 0755 "$work/next-litecp" /usr/local/bin/litecp
systemctl try-restart litecp-captcha.service
install -d -m 0755 /usr/local/lib/litecp
if [[ -f /usr/local/lib/litecp/system-snapshot.py ]]; then
  cp --preserve=mode /usr/local/lib/litecp/system-snapshot.py "$work/previous-system-snapshot.py"
fi
snapshot_helper_switched=1
install -m 0700 "$work/scripts/system-snapshot.py" /usr/local/lib/litecp/system-snapshot.py
cmp "$work/scripts/system-snapshot.py" /usr/local/lib/litecp/system-snapshot.py
install -m 0700 "$work/scripts/configure-private-updates.sh" /usr/local/lib/litecp/configure-private-updates.sh
install -m 0700 "$work/scripts/panel-rollback.sh" /usr/local/lib/litecp/panel-rollback.sh
bash "$work/scripts/repair-waf-labels.sh"
python3 "$work/scripts/repair-dns-runtime.py"
bash "$work/scripts/repair-php73-selinux.sh"
bash "$work/scripts/repair-migration-runtime-labels.sh"
systemctl start litecp
bash "$work/scripts/repair-webmail-runtime.sh"
echo '[Server 4/4] Checking service...'
healthy=0
for attempt in $(seq 1 45); do
  if bash "$work/scripts/check-privilege-services.sh" "$version"; then healthy=1; break; fi
  sleep 1
done
if [[ $healthy != 1 ]]; then
  systemctl status litecp litecp-broker litecp-broker.socket --no-pager || true
  journalctl -u litecp -u litecp-broker -n 50 --no-pager || true
  false
fi
echo "LiteCP $version deployed successfully. Recovery ID: ${recovery##*/}"
