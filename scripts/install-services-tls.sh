#!/usr/bin/env bash
set -u
[[ $EUID == 0 && $# == 2 ]] || exit 1
exec 9>/run/litecp-panel-port.lock
flock -n 9 || { echo 'Another panel SSL or port change is running.' >&2; exit 1; }
failed=0
if [[ $1 == sync ]]; then
  python3 -I /usr/local/lib/litecp/sync-ftps-tls.py || failed=1
fi
if [[ $2 == sync ]]; then
  python3 -I /usr/local/lib/litecp/sync-mail-tls.py || failed=1
fi
exit "$failed"
