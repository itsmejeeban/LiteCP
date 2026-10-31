#!/usr/bin/env bash
set -euo pipefail
[[ $(id -u) == 0 ]] || exit 1
WEBMAIL=/opt/litecp/webmail
[[ -f $WEBMAIL/config/config.inc.php ]] || { echo 'Install webmail first.' >&2; exit 1; }
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
install -d -m 0755 "$WEBMAIL/plugins/litecp_signon"
SIGNON_SOURCE="$SCRIPT_DIR/litecp_signon.php"
[[ -f $SIGNON_SOURCE ]] || SIGNON_SOURCE="$SCRIPT_DIR/../deploy/litecp_signon.php"
[[ -f $SIGNON_SOURCE ]] || { echo "LiteCP webmail sign-in plugin is missing from the installer." >&2; exit 1; }
install -m 0644 "$SIGNON_SOURCE" "$WEBMAIL/plugins/litecp_signon/litecp_signon.php"
if ! grep -q "plugins.*litecp_signon" "$WEBMAIL/config/config.inc.php"; then
  printf '\n$config["plugins"][] = "litecp_signon";\n' >> "$WEBMAIL/config/config.inc.php"
fi
# Master authentication is accepted only from the server loopback interface.
# The panel writes a password hash here; its encrypted secret stays root-only.
[[ -e /etc/litecp/mail/webmail-master-users ]] || install -m 0600 /dev/null /etc/litecp/mail/webmail-master-users
chown dovecot:dovecot /etc/litecp/mail/webmail-master-users
cat > /etc/dovecot/conf.d/00-litecp-webmail-signon.conf <<'EOF'
passdb {
  driver = passwd-file
  master = yes
  result_success = continue
  args = /etc/litecp/mail/webmail-master-users
}
EOF
doveconf -n >/dev/null
systemctl reload dovecot
