#!/usr/bin/env bash
# Keep the PHP socket outside the LiteCP service's disposable runtime directory.
set -Eeuo pipefail
[[ $EUID == 0 ]] || { echo 'Run as root.' >&2; exit 1; }
install -d -m 0755 /run/litecp-webmail
printf 'd /run/litecp-webmail 0755 root root -\n' > /etc/tmpfiles.d/litecp-webmail.conf
  if command -v semanage >/dev/null && command -v selinuxenabled >/dev/null && selinuxenabled; then
    semanage fcontext -a -t httpd_var_run_t '/run/litecp-webmail(/.*)?' 2>/dev/null || semanage fcontext -m -t httpd_var_run_t '/run/litecp-webmail(/.*)?'
    restorecon -R /run/litecp-webmail
  fi
pool=''; unit=''; fpm=''
if [[ -f /etc/opt/remi/php83/php-fpm.d/litecp-webmail.conf ]]; then
  pool=/etc/opt/remi/php83/php-fpm.d/litecp-webmail.conf; unit=php83-php-fpm; fpm=/opt/remi/php83/root/usr/sbin/php-fpm
elif [[ -f /etc/php/8.3/fpm/pool.d/litecp-webmail.conf ]]; then
  pool=/etc/php/8.3/fpm/pool.d/litecp-webmail.conf; unit=php8.3-fpm; fpm=/usr/sbin/php-fpm8.3
fi
[[ -n $pool && -f /etc/nginx/conf.d/litecp-webmail.conf ]] || exit 0
changed=0
for config in "$pool" /etc/nginx/conf.d/litecp-webmail.conf; do
  if grep -qF '/run/litecp/webmail.sock' "$config"; then
    recovery=$(mktemp "${config}.before-runtime-fix.XXXXXX")
    cp --preserve=mode,ownership,timestamps "$config" "$recovery"
    sed -i 's|/run/litecp/webmail.sock|/run/litecp-webmail/webmail.sock|g' "$config"
    changed=1
    echo "Preserved configuration: $recovery"
  fi
done
if command -v restorecon >/dev/null; then restorecon -R /run/litecp-webmail; fi
"$fpm" -t
nginx -t
if [[ $changed == 1 || ! -S /run/litecp-webmail/webmail.sock ]]; then systemctl restart "$unit"; fi
[[ -S /run/litecp-webmail/webmail.sock ]] || { echo 'Webmail PHP socket is missing after restart.' >&2; exit 1; }
systemctl reload nginx
echo 'Webmail runtime socket verified.'

# Refresh the installed sign-in plugin as part of panel updates, without
# changing mailbox credentials or rewriting the mail-server configuration.
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
plugin_source="$script_dir/../deploy/litecp_signon.php"
plugin_target=/opt/litecp/webmail/plugins/litecp_signon/litecp_signon.php
if [[ -f $plugin_source && -f $plugin_target ]] && ! cmp -s "$plugin_source" "$plugin_target"; then
  if [[ $unit == php83-php-fpm ]]; then php_cli=/opt/remi/php83/root/usr/bin/php; else php_cli=/usr/bin/php8.3; fi
  # Syntax checking needs only the PHP parser. Do not load server extensions
  # (for example ionCube or OPcache), which can crash CLI startup during updates.
  # Keep validation mandatory and leave the installed plugin untouched on failure.
  if "$php_cli" -n -l "$plugin_source"; then
    :
  else
    lint_status=$?
    echo "Webmail plugin syntax validation failed (PHP exit $lint_status). Existing plugin retained. Check the PHP CLI installation before retrying." >&2
    exit "$lint_status"
  fi
  recovery=$(mktemp "${plugin_target}.before-update.XXXXXX")
  cp --preserve=mode,ownership,timestamps "$plugin_target" "$recovery"
  install -m 0644 "$plugin_source" "$plugin_target"
  if command -v restorecon >/dev/null; then restorecon "$plugin_target"; fi
  systemctl reload "$unit"
  echo 'Webmail sign-in plugin updated; previous plugin preserved.'
fi
