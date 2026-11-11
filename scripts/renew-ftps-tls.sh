#!/bin/sh
set -eu
test -n "${RENEWED_LINEAGE:-}" || exit 0
failed=0
python3 -I /usr/local/lib/litecp/sync-ftps-tls.py || failed=1
LITECP_MAIL_TLS_RELOAD_ONLY=1 python3 -I /usr/local/lib/litecp/sync-mail-tls.py || failed=1
exit "$failed"
