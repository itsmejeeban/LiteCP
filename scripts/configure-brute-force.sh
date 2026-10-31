#!/usr/bin/env bash
set -Eeuo pipefail
command -v fail2ban-client >/dev/null || { echo 'Fail2ban is not installed.' >&2; exit 1; }
case "${1:-}" in
 enable) fail2ban-client -t; systemctl enable --now fail2ban; systemctl is-active --quiet fail2ban ;;
 disable) systemctl disable --now fail2ban; ! systemctl is-active --quiet fail2ban ;;
 rules)
 jail="$2"; retries="$3"; window="$4"; duration="$5"
 case "$jail" in sshd|postfix|postfix-sasl|dovecot) ;; *) exit 2 ;; esac
 [[ "$retries" =~ ^[0-9]+$ && "$window" =~ ^[0-9]+$ && "$duration" =~ ^[0-9]+$ ]] || exit 2
 (( retries >= 1 && retries <= 100 && window >= 60 && window <= 86400 && duration >= 60 && duration <= 2592000 )) || exit 2
 root=$(mktemp -d /var/lib/litecp/brute-force-change.XXXXXXXX)
 target="/etc/fail2ban/jail.d/zzzz-litecp-${jail}-rules.local"
 if [[ -f "$target" ]]; then cp -a "$target" "$root/previous.local"; fi
 printf '[%s]\nmaxretry = %s\nfindtime = %s\nbantime = %s\n' "$jail" "$retries" "$window" "$duration" > "$target"
 if ! fail2ban-client -t || { systemctl is-active --quiet fail2ban && ! fail2ban-client reload; }; then
  mv "$target" "$root/rejected.local"
  if [[ -f "$root/previous.local" ]]; then cp -a "$root/previous.local" "$target"; fi
  if systemctl is-active --quiet fail2ban; then fail2ban-client reload; fi
  echo 'Rule update failed; previous configuration restored.' >&2; exit 1
 fi
 ;;
 *) exit 2 ;;
esac
