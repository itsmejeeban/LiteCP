#!/usr/bin/env bash
# Exercise the actual service namespace before changing an installed service.
set -Eeuo pipefail
[[ $EUID == 0 ]] || { echo 'Run as root.' >&2; exit 1; }
unit=${1:?Path to the candidate litecp.service is required}
python3 - "$unit" <<'PY'
import os,subprocess,sys,time
section=''; properties=[]; settings={}
for raw in open(sys.argv[1]):
    line=raw.strip()
    if not line or line.startswith(('#',';')): continue
    if line.startswith('['): section=line;continue
    if section!='[Service]' or '=' not in line: continue
    key,value=line.split('=',1);settings[key]=value
    if key.startswith('Exec') or key in {'Type','Restart','RestartSec','TimeoutStopSec'}: continue
    properties.append('--property='+key+'='+value)
for key,value in {'User':'litecp-web','Group':'litecp-web','CapabilityBoundingSet':'','AmbientCapabilities':'','NoNewPrivileges':'true'}.items():
    if settings.get(key)!=value: raise SystemExit('Unsafe candidate web service setting: '+key)
name='litecp-web-preflight-'+str(os.getpid())+'-'+str(int(time.time()))
probe="""
import os
if os.geteuid()==0: raise SystemExit('Web preflight retained root')
for path in ('/etc/shadow','/var/lib/litecp/config.json','/var/lib/litecp/secret.key'):
    try: fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW)
    except (PermissionError,FileNotFoundError): continue
    else:
        os.close(fd);raise SystemExit('Web preflight can read private data: '+path)
fields=dict(line.split(':',1) for line in open('/proc/self/status') if ':' in line)
if any(int(fields[key].strip(),16) for key in ('CapEff','CapPrm','CapInh','CapAmb')):
    raise SystemExit('Web preflight retained capabilities')
if fields['NoNewPrivs'].strip()!='1': raise SystemExit('NoNewPrivileges is missing')
print('Runtime web preflight: UID='+str(os.geteuid())+', private data unreadable, capabilities empty.')
"""
# Do not pass the parent service's journald socket as a pipe to another unit.
# This probe also runs inside the background updater, without an interactive TTY.
command=['systemd-run','--quiet','--wait','--collect','--unit='+name,
         '--property=RuntimeMaxSec=20','--property=TimeoutStartSec=20','--property=Restart=no']+properties+['/usr/bin/python3','-c',probe]
result=subprocess.run(command,timeout=30)
if result.returncode:
    subprocess.run(['journalctl','-u',name,'-n','25','--no-pager'])
    raise SystemExit('The candidate web sandbox cannot start. Installed service units were not replaced.')
print('Unprivileged web sandbox runtime preflight passed.')
PY
