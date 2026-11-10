#!/usr/bin/env bash
# Independent root-console binary recovery; no website or database rollback.
set -Eeuo pipefail
[[ $EUID == 0 ]] || { echo 'Run from a root SSH/provider console.' >&2; exit 1; }
python3 - "${1:---list}" <<'PY'
import fcntl,hashlib,ipaddress,json,os,pathlib,re,shutil,stat,subprocess,sys,tempfile,time,urllib.request
base=pathlib.Path('/usr/local/lib/litecp-updates')
def candidate(name):
    if not re.fullmatch(r'update-[A-Za-z0-9]+',name):raise ValueError('Invalid recovery ID')
    root=base/name
    for p in [base,root,root/'recovery-manifest.json',root/'previous-litecp']:
        s=p.lstat()
        if stat.S_ISLNK(s.st_mode) or s.st_uid!=0 or s.st_mode&0o022:raise ValueError('Unsafe recovery ownership or permissions')
    metadata=json.loads((root/'recovery-manifest.json').read_text())
    if not re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+',metadata['version']):raise ValueError('Invalid recorded version')
    if hashlib.sha256((root/'previous-litecp').read_bytes()).hexdigest()!=metadata['sha256']:raise ValueError('Previous binary checksum mismatch')
    return root,metadata
if sys.argv[1]=='--list':
    for root in sorted(base.glob('update-*')):
        try:
            _,m=candidate(root.name);print(root.name,'previous version:',m['version'],'saved:',m.get('savedAt','unknown'))
        except (OSError,ValueError,KeyError):continue
    raise SystemExit(0)
root,m=candidate(sys.argv[1])
bind=m.get('healthBind','127.0.0.1:9090')
host,port=bind.rsplit(':',1)
if not ipaddress.ip_address(host.strip('[]')).is_loopback or not port.isdecimal() or not 1<=int(port)<=65535:
    raise SystemExit('Invalid recovery health-check endpoint')
print('Restore previous panel binary',m['version'],'from',root.name)
print('The current binary is retained. Website/database data will not be restored. Panel downtime is expected.')
with open('/dev/tty','r+') as tty:
    tty.write('Type ROLLBACK to proceed: ');tty.flush()
    if tty.readline().strip()!='ROLLBACK':raise SystemExit('Cancelled without changes')
lock=os.fdopen(os.open('/run/lock/litecp-panel-update.lock',os.O_CREAT|os.O_RDWR|os.O_NOFOLLOW,0o600),'r+')
try:fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
except BlockingIOError:raise SystemExit('A panel update or rollback is already running')
if subprocess.run(['systemctl','is-active','--quiet','litecp-panel-update.service']).returncode==0:
    raise SystemExit('Wait for the panel update to finish')
# The restore point may have been deleted while confirmation was open.
root,_=candidate(sys.argv[1])
recovery=pathlib.Path(tempfile.mkdtemp(prefix='rollback-',dir=base))
shutil.copy2('/usr/local/bin/litecp',recovery/'current-litecp')
config=pathlib.Path('/var/lib/litecp/config.json')
if config.is_file():shutil.copy2(config,recovery/'current-config.json')
def start():subprocess.run(['systemctl','start','litecp'],check=True)
def copy_binary(source):shutil.copy2(source,'/usr/local/bin/litecp');os.chmod('/usr/local/bin/litecp',0o755)
subprocess.run(['systemctl','stop','litecp','litecp-broker.service','litecp-broker.socket'],check=True)
try:
    copy_binary(root/'previous-litecp');start();healthy=False
    for _ in range(45):
        try:
            status=json.load(urllib.request.urlopen('http://'+bind+'/api/version',timeout=2))
            if status.get('version')==m['version']:healthy=True;break
        except Exception:pass
        time.sleep(1)
    if not healthy:raise RuntimeError('Previous binary did not pass health verification')
except Exception:
    subprocess.run(['systemctl','stop','litecp','litecp-broker.service','litecp-broker.socket'])
    copy_binary(recovery/'current-litecp');start();raise
print('Panel binary rollback completed. Current binary retained in',recovery)
print('Review service compatibility and application health before returning to production.')
PY
