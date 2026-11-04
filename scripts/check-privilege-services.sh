#!/usr/bin/env bash
# Read-only post-install/update acceptance check. No service or data changes.
set -Eeuo pipefail
version=${1:?Expected version required}
[[ $version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || exit 1
for unit in litecp.service litecp-broker.service litecp-broker.socket; do systemctl is-active --quiet "$unit"; done
[[ $(systemctl show litecp.service -p User --value) == litecp-web ]]
pid=$(systemctl show litecp.service -p MainPID --value)
[[ $pid =~ ^[1-9][0-9]*$ ]]
[[ $(awk '$1=="Uid:" {print $3}' "/proc/$pid/status") == "$(id -u litecp-web)" ]]
[[ $(awk '$1=="CapEff:" {print $2}' "/proc/$pid/status") == 0000000000000000 ]]
[[ $(awk '$1=="NoNewPrivs:" {print $2}' "/proc/$pid/status") == 1 ]]
bind=$(python3 - <<'PY'
import ipaddress, pathlib
value='127.0.0.1:9090'
path=pathlib.Path('/etc/litecp/frontend.env')
if path.exists():
    for line in path.read_text().splitlines():
        if line.startswith('LITECP_BIND='): value=line.split('=',1)[1].strip().strip('"\x27')
host,port=value.rsplit(':',1)
address=ipaddress.ip_address(host.strip('[]'))
if not address.is_loopback or not port.isdecimal() or not 1<=int(port)<=65535:
    raise SystemExit('Invalid frontend loopback bind')
print(('['+str(address)+']' if address.version==6 else str(address))+':'+port)
PY
)
curl --fail --silent --max-time 2 "http://$bind/api/version" | python3 -c 'import json,sys;sys.exit(json.load(sys.stdin).get("version")!=sys.argv[1])' "$version"
