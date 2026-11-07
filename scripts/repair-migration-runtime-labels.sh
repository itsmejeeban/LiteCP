#!/usr/bin/env bash
set -euo pipefail

if ! command -v selinuxenabled >/dev/null || ! selinuxenabled; then
  exit 0
fi

# Support split Laravel layouts while keeping application code read-only.
for folder in storage bootstrap/cache; do
  pattern="/home/litecp/[^/]+/[^/]+/$folder(/.*)?"
  semanage fcontext -a -t httpd_sys_rw_content_t "$pattern" 2>/dev/null ||
    semanage fcontext -m -t httpd_sys_rw_content_t "$pattern"
done
