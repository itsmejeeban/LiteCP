#!/usr/bin/env bash
set -Eeuo pipefail
job=${1:?Job directory required}
base=$(dirname -- "$job")
status(){
 python3 - "$base/status.json" "$1" "$2" <<'PY'
import json,os,sys,tempfile
path,state,message=sys.argv[1:]
with open(path) as source: value=json.load(source)
value.update(state=state,message=message)
fd,temporary=tempfile.mkstemp(prefix='.status-',dir=os.path.dirname(path))
with os.fdopen(fd,'w') as target: json.dump(value,target)
os.replace(temporary,path)
PY
}
failed(){ status failed 'SELinux enablement did not complete. Previous settings were retained or rollback was triggered; review the log.'; }
trap failed ERR
bash "$job/enable-selinux.sh" > "$job/output.log" 2>&1
status success 'SELinux enforcement and persistent configuration passed the guarded checks.'
