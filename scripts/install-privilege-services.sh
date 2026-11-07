#!/usr/bin/env bash
# Install the unprivileged listener and local broker; never start a new root HTTP listener.
set -Eeuo pipefail
[[ $(id -u) == 0 ]] || { echo 'Run as root.' >&2; exit 1; }
source_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
units=(litecp.service litecp-broker.service litecp-broker.socket litecp-captcha.service)
if [[ ${1:-} == --restore ]]; then
  recovery=${2:?Recovery directory required}
  [[ -d $recovery && ! -L $recovery ]] || exit 1
  for unit in "${units[@]}"; do
    if [[ -f $recovery/$unit ]]; then install -m 0644 "$recovery/$unit" "/etc/systemd/system/$unit"; fi
  done
  systemctl daemon-reload
  exit 0
fi
prepare_only=0
if [[ ${1:-} == --prepare-only && $# == 1 ]]; then prepare_only=1
elif [[ $# != 0 ]]; then echo 'Usage: install-privilege-services.sh [--prepare-only | --restore recovery-directory]' >&2; exit 1
fi
for unit in "${units[@]}"; do [[ -f $source_dir/deploy/$unit ]] || exit 1; done
if ! id litecp-web >/dev/null 2>&1; then
  useradd --system --user-group --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin litecp-web
fi
[[ $(id -u litecp-web) != 0 && $(id -gn litecp-web) == litecp-web ]] || { echo 'Unsafe litecp-web account identity.' >&2; exit 1; }
[[ $(id -Gn litecp-web) == litecp-web ]] || { echo 'Remove supplementary groups from litecp-web before installing; no administrator/container groups are permitted.' >&2; exit 1; }
[[ $(getent passwd litecp-web | cut -d: -f6-7) == /nonexistent:/usr/sbin/nologin ]] || { echo 'The dedicated web account must have /nonexistent as its home and /usr/sbin/nologin as its shell.' >&2; exit 1; }
install -d -m 0755 /etc/litecp
# systemd applies optional writable paths when the broker starts. Reserve
# the DNS directory now, even when BIND is installed later.
if [[ -L /etc/litecp/dns || ( -e /etc/litecp/dns && ! -d /etc/litecp/dns ) ]]; then
  echo 'LiteCP DNS storage must be a real directory.' >&2
  exit 1
fi
if [[ ! -d /etc/litecp/dns ]]; then
  install -d -m 0755 -o root -g root /etc/litecp/dns
fi
if ! id litecp-captcha >/dev/null 2>&1; then
  useradd --system --user-group --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin litecp-captcha
fi
[[ $(id -u litecp-captcha) != 0 && $(id -Gn litecp-captcha) == litecp-captcha ]] || { echo 'Unsafe CAPTCHA service identity.' >&2; exit 1; }
[[ $(getent passwd litecp-captcha | cut -d: -f6-7) == /nonexistent:/usr/sbin/nologin ]] || { echo 'The CAPTCHA account must use /nonexistent and /usr/sbin/nologin.' >&2; exit 1; }
install -d -m 0750 -o root -g litecp-captcha /var/lib/litecp-captcha
if [[ ! -e /etc/litecp/frontend.env ]]; then
  # Copy only the bind setting, never administrator/database secrets.
  bind=$(awk -F= '/^LITECP_BIND=/{value=substr($0,index($0,"=")+1)} END {print value}' /etc/litecp/litecp.env 2>/dev/null || true)
  [[ -n $bind ]] || bind=127.0.0.1:9090
  printf 'LITECP_BIND=%s\n' "$bind" > /etc/litecp/frontend.env
  chmod 0600 /etc/litecp/frontend.env
fi
echo 'Checking the candidate unprivileged web sandbox before replacing service units...'
bash "$source_dir/scripts/check-web-sandbox.sh" "$source_dir/deploy/litecp.service"
[[ $prepare_only == 0 ]] || exit 0
recovery=${LITECP_SERVICE_BACKUP_DIR:-}
if [[ -z $recovery ]]; then
  install -d -m 0700 /var/backups/litecp
  recovery=$(mktemp -d /var/backups/litecp/service-split-XXXXXXXX)
else
  [[ ! -e $recovery ]] || { echo 'Service recovery directory already exists; refusing to overwrite it.' >&2; exit 1; }
  install -d -m 0700 "$recovery"
fi
for unit in "${units[@]}"; do
  if [[ -f /etc/systemd/system/$unit ]]; then cp -p "/etc/systemd/system/$unit" "$recovery/$unit"; fi
done
for unit in "${units[@]}"; do install -m 0644 "$source_dir/deploy/$unit" "/etc/systemd/system/$unit"; done
systemctl daemon-reload
[[ $(systemctl show litecp.service -p User --value) == litecp-web ]] || { echo 'A service override prevents unprivileged operation. Previous units are retained for recovery.' >&2; exit 1; }
systemctl enable litecp-captcha.service
bash "$source_dir/scripts/install-application-boot-order.sh"
echo "Privilege-separated service units installed. Previous units retained in $recovery"
