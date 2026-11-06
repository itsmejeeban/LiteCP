#!/usr/bin/env bash
# Container bind mounts must exist before Podman restores managed applications.
set -Eeuo pipefail
[[ $(id -u) == 0 ]] || { echo 'Run as root.' >&2; exit 1; }
command -v podman >/dev/null || exit 0
path=/etc/systemd/system/podman-restart.service.d/20-litecp-storage-order.conf
[[ ! -L $path && ! -L ${path%/*} ]] || { echo 'Refusing symlinked application boot configuration.' >&2; exit 1; }
expected=$'[Unit]\nAfter=mysqld.service mysql.service mariadb.service\nRequiresMountsFor=/home/litecp\n'
if [[ -f $path ]] && [[ $(cat "$path") == "${expected%$'\n'}" ]]; then exit 0; fi
if [[ -e $path ]]; then
  install -d -m 0700 /var/backups/litecp
  backup=$(mktemp -d /var/backups/litecp/application-boot-order.XXXXXXXX)
  cp -p "$path" "$backup/previous.conf"
  echo "Previous application boot order retained in $backup"
fi
install -d -m 0755 "${path%/*}"
printf '%s' "$expected" > "$path"
chmod 0644 "$path"
systemctl daemon-reload
echo 'Application boot waits for database services and website storage.'
