package main

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"
)

const savePostfixHostname = `set -eu
umask 077
current=$(postconf -h myhostname)
test "$current" = "$1"
config=$(postconf -h config_directory)
test -f "$config/main.cf"
backup=$(mktemp /var/lib/litecp/postfix-main.cf.XXXXXXXX)
cp -- "$config/main.cf" "$backup"
if postconf -e "myhostname = $2" && postfix check && postfix reload &&
   test "$(postconf -h myhostname)" = "$2"; then
  exit 0
fi
if cat "$backup" > "$config/main.cf" && postfix reload; then
  echo "Postfix change rolled back. Backup: $backup" >&2
else
  echo "Postfix rollback failed. Backup: $backup" >&2
fi
exit 1
`

func readPostfixHostname(ctx context.Context) (string, error) {
	value, err := runCommand(ctx, 3*time.Second, "postconf", "-h", "myhostname")
	return strings.TrimSpace(value), err
}

func (a *App) postfixHostname(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if validExternalAPIGrant(r) || a.requestOperator(r) != nil {
		writeError(w, 403, "Postfix hostname settings require the primary administrator.")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPut {
		writeError(w, 405, "Use GET or PUT.")
		return
	}
	if a.system.demo {
		writeError(w, 409, "Postfix hostname settings require an installed server.")
		return
	}
	current, err := readPostfixHostname(r.Context())
	if err != nil || current == "" {
		writeError(w, 503, "Could not read the hostname from Postfix.")
		return
	}
	if r.Method == http.MethodPut {
		var input struct {
			Current  string `json:"current"`
			Hostname string `json:"hostname"`
		}
		if decodeJSONWithLimit(r, &input, 4096) != nil {
			writeError(w, 400, "Invalid hostname request.")
			return
		}
		host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(input.Hostname), "."))
		if !validPanelSSLHostname(host) || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".localdomain") {
			writeError(w, 400, "Enter a fully qualified mail hostname, such as mail.example.com.")
			return
		}
		if input.Current != current {
			writeError(w, 409, "The Postfix hostname changed. Refresh the card and try again.")
			return
		}
		if host != current {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			if _, err := runHostCommand(ctx, 40*time.Second, "/bin/bash", "-c", savePostfixHostname, "litecp-postfix-hostname", current, host); err != nil {
				log.Printf("Postfix hostname update failed: %v", err)
				writeError(w, 500, "Could not verify the Postfix hostname change. Refresh and check the broker log for recovery details.")
				return
			}
			_ = a.store.AddActivity("Postfix hostname updated", current+" → "+host, "info")
			current = host
		}
	}
	writeJSON(w, 200, map[string]string{"hostname": current})
}
