#!/usr/bin/env bash
# Copy this launcher beside LiteCP-2.0.6-source.tar.gz and run: bash install-litecp.sh
set -Eeuo pipefail
version=5.0.11
[[ "$(id -u)" -eq 0 ]] || { echo 'Run this installer as root.' >&2; exit 1; }
source /etc/os-release
[[ "$ID" == almalinux || "$ID" == ubuntu ]] || { echo 'Ubuntu or AlmaLinux is required.' >&2; exit 1; }
echo '[1/4] Checking installation tools...'
if ! command -v curl >/dev/null || ! command -v tar >/dev/null; then
  if command -v dnf >/dev/null; then dnf install -y ca-certificates curl tar;
  else apt-get update; apt-get install -y ca-certificates curl tar; fi
fi
directory="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
archive="$directory/LiteCP-$version-source.tar.gz"
if [[ ! -f "$archive" ]]; then
  echo '[2/4] Downloading the source archive (Git is not required)...'
  work="$(mktemp -d /opt/litecp-download.XXXXXXXX)"
  archive="$work/LiteCP-$version-source.tar.gz"
  # Credentials remain in memory and are passed through stdin, not a URL or file.
  token=''
  if curl --fail --location --proto '=https' --proto-redir '=https' --output "$archive" 'https://api.github.com/repos/itsmejeeban/LiteCP/tarball/main'; then
    echo 'Source archive downloaded.'
  else
    [[ -r /dev/tty ]] || { echo 'Private repository: run in an interactive SSH terminal or supply the source tarball.' >&2; exit 1; }
    read -r -s -p 'GitHub read token: ' token </dev/tty; printf '\n'
    [[ -n "$token" ]] || { echo 'A read token is required for this repository.' >&2; exit 1; }
    [[ "$token" != *$'\n'* && "$token" != *$'\r'* && "$token" != *'"'* && "$token" != *'\'* ]] || { echo 'Invalid token format.' >&2; exit 1; }
    printf 'header = "Authorization: Bearer %s"\n' "$token" | curl --config - --fail --location --proto '=https' --proto-redir '=https' --output "$archive" 'https://api.github.com/repos/itsmejeeban/LiteCP/tarball/main'
  fi
  unset token
else
  echo '[2/4] Using the supplied source archive...'
  if [[ -f "$archive.sha256" ]]; then (cd "$directory" && sha256sum --check "$(basename "$archive").sha256"); fi
fi
echo '[3/4] Extracting into a new installation directory...'
work="$(mktemp -d /opt/litecp-install.XXXXXXXX)"
mkdir "$work/source"
tar -xzf "$archive" -C "$work/source" --strip-components=1 --no-same-owner
[[ -f "$work/source/go.mod" && -f "$work/source/scripts/install.sh" ]] || { echo 'Invalid LiteCP installation archive.' >&2; exit 1; }
echo '[4/4] Starting LiteCP installation...'
cd "$work/source"
bash scripts/install.sh
