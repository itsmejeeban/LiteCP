#!/usr/bin/env bash
set -euo pipefail
[[ $(id -u) == 0 ]] || { echo 'Run as root'; exit 1; }
. /etc/os-release
case "$ID" in
 ubuntu) apt-get update; DEBIAN_FRONTEND=noninteractive apt-get install --no-remove --no-install-recommends -y podman ;;
 almalinux) dnf install --setopt=install_weak_deps=False -y podman ;;
 *) echo 'Unsupported operating system' >&2; exit 1 ;;
esac
podman --version
echo 'Container engine ready. Node.js, Python and Ruby hosting remain disabled until enabled in LiteCP. No runtime images downloaded.'
