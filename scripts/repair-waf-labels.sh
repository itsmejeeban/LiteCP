#!/usr/bin/env bash
# Normalize only LiteCP-managed vendor rules and SELinux configuration labels.
set -Eeuo pipefail
[[ $EUID == 0 ]] || { echo 'Run as root.' >&2; exit 1; }
[[ -d /etc/litecp/waf ]] || exit 0
[[ ! -L /etc/litecp/waf && ! -L /etc/litecp/waf/rules ]] || { echo 'Refusing a symlinked WAF configuration tree.' >&2; exit 1; }
if [[ -d /etc/litecp/waf/rules ]]; then chown -Rh root:root /etc/litecp/waf/rules; fi
if command -v selinuxenabled >/dev/null && selinuxenabled; then
  command -v semanage >/dev/null || { echo 'SELinux management tools are required to label WAF configuration.' >&2; exit 1; }
  semanage fcontext -a -t httpd_config_t '/etc/litecp/waf(/.*)?' 2>/dev/null || semanage fcontext -m -t httpd_config_t '/etc/litecp/waf(/.*)?'
  restorecon -R /etc/litecp/waf
fi
