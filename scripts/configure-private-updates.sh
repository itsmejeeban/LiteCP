#!/usr/bin/env bash
# Run locally over SSH as root. Tokens are never echoed or accepted in argv.
set -Eeuo pipefail
[[ $EUID == 0 ]] || { echo 'Run as root.' >&2; exit 1; }
[[ -d /etc/litecp && ! -L /etc/litecp ]] || { echo 'Install LiteCP first.' >&2; exit 1; }
[[ ! -e /etc/litecp/github-update-token && ! -L /etc/litecp/github-update-token ]] || { echo 'An update credential already exists; it was preserved.' >&2; exit 1; }
echo 'Use a fine-grained token restricted to LiteCP with Contents: read-only.'
IFS= read -r -s -p 'GitHub update token (hidden): ' token </dev/tty
printf '\n'
[[ $token =~ ^[A-Za-z0-9_]{20,1024}$ ]] || { echo 'Invalid token format.' >&2; exit 1; }
printf '%s' "$token" | python3 -c '
import os,sys
path="/etc/litecp/github-update-token"
fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
with os.fdopen(fd,"wb") as out:out.write(sys.stdin.buffer.read())
'
unset token
echo 'Private update credential saved with root-only access. Check for updates in LiteCP.'
