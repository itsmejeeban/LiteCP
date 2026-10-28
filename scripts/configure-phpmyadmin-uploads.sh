#!/usr/bin/env bash
# Keep phpMyAdmin's 5 GiB import allowance isolated from other panel routes.
# Reapply after upgrades that reset shared proxy limits.
set -Eeuo pipefail
PANEL_CONFIG=${1:?Pass the LiteCP panel Nginx configuration path.}
[[ -f ${PANEL_CONFIG} ]] || { echo 'Panel Nginx configuration not found.' >&2; exit 1; }
UPLOAD_LOCATION=$(cat <<'EOF'
    location /api/phpmyadmin/ {
        client_max_body_size 5122m;
        client_body_timeout 2700s;
        proxy_pass http://127.0.0.1:9090;
        proxy_request_buffering off;
        proxy_http_version 1.1;
        proxy_set_header Host $host:$server_port;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
        proxy_read_timeout 2700s;
        proxy_send_timeout 2700s;
    }
EOF
)
UPDATED=$(awk -v block="${UPLOAD_LOCATION}" '
function upload_start(line) { return line ~ /^[[:space:]]*location[[:space:]]+\/api\/phpmyadmin\/[[:space:]]*\{[[:space:]]*$/ }
function root_start(line) { return line ~ /^[[:space:]]*location[[:space:]]+\/[[:space:]]*\{[[:space:]]*$/ }
FNR == NR {
    if (upload_start($0)) uploads++
    if (root_start($0)) roots++
    next
}
FNR == 1 {
    if (uploads > 1 || (uploads == 0 && roots != 1)) exit 2
}
{
    if (upload_start($0)) { inside = 1; print; next }
    if (!uploads && root_start($0)) print block
    if (inside && /^[[:space:]]*}[[:space:]]*$/) {
        for (i = 1; i <= 6; i++) if (!seen[key[i]]) print "        " key[i] " " value[i] ";"
        inside = 0
    } else if (inside) {
        # Fail closed for a custom nested block rather than rewriting its scope.
        if ($0 ~ /[{}]/) exit 2
        for (i = 1; i <= 6; i++) if ($1 == key[i]) {
            $0 = "        " key[i] " " value[i] ";"
            seen[key[i]] = 1
        }
    }
    print
}
BEGIN {
    key[1] = "client_max_body_size"; value[1] = "5122m"
    key[2] = "client_body_timeout"; value[2] = "2700s"
    key[3] = "proxy_read_timeout"; value[3] = "2700s"
    key[4] = "proxy_send_timeout"; value[4] = "2700s"
    key[5] = "proxy_request_buffering"; value[5] = "off"
    key[6] = "proxy_http_version"; value[6] = "1.1"
}
END { if (inside) exit 2 }
' "${PANEL_CONFIG}" "${PANEL_CONFIG}") || {
    echo 'Cannot safely identify the standard panel upload location; configuration left unchanged.' >&2
    exit 1
}
printf '%s\n' "${UPDATED}" > "${PANEL_CONFIG}"
