#!/usr/bin/env bash
# Permit the confined Podman transition while retaining NoNewPrivileges.
set -Eeuo pipefail
[[ $EUID == 0 ]] || { echo 'Run as root.' >&2; exit 1; }
command -v selinuxenabled >/dev/null && selinuxenabled || exit 0
systemctl cat litecp-php73-fpm.service >/dev/null 2>&1 || exit 0
for tool in checkmodule semodule_package semodule; do command -v "$tool" >/dev/null || { echo "Missing SELinux tool: $tool" >&2; exit 1; }; done
work=$(mktemp -d /var/tmp/litecp-php73-selinux.XXXXXX)
cat > "$work/litecp_php73_runtime_nnp.te" <<'POLICY'
module litecp_php73_runtime_nnp 1.0;
require { type init_t; type container_runtime_t; class process2 nnp_transition; }
allow init_t container_runtime_t:process2 nnp_transition;
POLICY
checkmodule -M -m -o "$work/litecp_php73_runtime_nnp.mod" "$work/litecp_php73_runtime_nnp.te"
semodule_package -o "$work/litecp_php73_runtime_nnp.pp" -m "$work/litecp_php73_runtime_nnp.mod"
semodule -i "$work/litecp_php73_runtime_nnp.pp"
folder=/etc/systemd/system/litecp-php73-fpm.service.d
install -d -m 0755 "$folder"
target="$folder/90-litecp-approved-selinux.conf"
if [[ -e $target ]]; then
 grep -qx 'SELinuxContext=system_u:system_r:container_runtime_t:s0' "$target" || { echo "Existing PHP 7.3 context retained; review $target" >&2; exit 1; }
else
 printf '[Service]\nSELinuxContext=system_u:system_r:container_runtime_t:s0\n' > "$target"
fi
systemctl daemon-reload
echo "PHP 7.3 SELinux transition prepared. Policy source retained in $work."
