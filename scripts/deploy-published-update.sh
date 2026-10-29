#!/usr/bin/env bash
# Called only by the user-run update.bat workflow. Keeps source and recovery binary.
set -Eeuo pipefail
version="$1"
revision="$2"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]
[[ "$revision" =~ ^[0-9a-f]{40}$ ]]
for tool in go systemctl install cp; do command -v "$tool" >/dev/null; done
work="$PWD"
switched=0
rollback() {
  trap - ERR
  if [[ "$switched" == 1 ]]; then
    echo 'Restoring the previous LiteCP binary...'
    install -m 0755 "$work/previous-litecp" /usr/local/bin/litecp
    systemctl restart litecp || true
  fi
  echo "UPDATE FAILED. Recovery files retained in $work"
  exit 1
}
trap rollback ERR
echo '[Server 1/4] Running Go tests...'
go test -p=1 ./...
echo '[Server 2/4] Building the published revision...'
CGO_ENABLED=0 go build -p=1 -mod=readonly -trimpath -ldflags "-s -w -X main.version=$version -X main.buildRevision=$revision" -o "$work/next-litecp" .
echo '[Server 3/4] Preserving current binary and restarting LiteCP...'
cp --preserve=mode /usr/local/bin/litecp "$work/previous-litecp"
systemctl stop litecp
switched=1
install -m 0755 "$work/next-litecp" /usr/local/bin/litecp
systemctl start litecp
echo '[Server 4/4] Checking service...'
systemctl is-active --quiet litecp
echo "LiteCP $version deployed successfully. Recovery binary: $work/previous-litecp"
