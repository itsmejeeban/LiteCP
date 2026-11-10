set -Eeuo pipefail
tag="$1"
completed_stages=0
data_dir="$2"
[[ "$tag" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+$ ]]
status() {
  python3 - "$data_dir/panel-update.json" "$1" "$2" "$tag" "$completed_stages" <<'PY'
import json,os,sys,tempfile
path,state,message,version,completedStages=sys.argv[1:]
fd,tmp=tempfile.mkstemp(dir=os.path.dirname(path),prefix='.panel-update-')
with os.fdopen(fd,'w') as out: json.dump(dict(state=state,message=message,version=version,completedStages=completedStages),out)
os.replace(tmp,path)
PY
}
panel_stop_started=0
binary_install_started=0
work=''
failure_message='Update failed. The previous panel binary has been retained. Review journalctl -u litecp-panel-update.'
failed() {
  trap - ERR
  # Still attempt to start the panel if a recovery command itself fails.
  set +e
  if [[ "$panel_stop_started" == 1 ]]; then
    if [[ -d "$work/service-recovery" ]]; then
      systemctl stop litecp litecp-broker.service litecp-broker.socket
      bash "$work/source/scripts/install-privilege-services.sh" --restore "$work/service-recovery"
    fi
    if [[ "$binary_install_started" == 1 ]]; then
      cp --preserve=mode "$work/previous-litecp" /usr/local/bin/litecp
      if [[ -f "$work/previous-system-snapshot.py" ]]; then install -m 0700 "$work/previous-system-snapshot.py" /usr/local/lib/litecp/system-snapshot.py; fi
      systemctl try-restart litecp-captcha.service
    fi
    systemctl start litecp
  fi
  printf '%s\n' "$failure_message" >&2
  status failed "$failure_message"
  exit 1
}
check_recovery_space() {
  local destination="$1" available_kib data_kib reserve_kib=1048576
  if ! data_kib=$(du -sk -- "$data_dir" | cut -f 1) ||
     ! available_kib=$(df -Pk -- "$destination" | awk 'NR==2 {print $4}') ||
     ! [[ "$data_kib" =~ ^[0-9]+$ && "$available_kib" =~ ^[0-9]+$ ]]; then
    failure_message="Could not verify available disk space for the panel recovery copy. The running panel was not stopped."
    return 1
  fi
  if (( available_kib < data_kib + reserve_kib )); then
    failure_message="Insufficient disk space for the panel recovery copy. Need $((data_kib + reserve_kib)) KiB free, including a 1 GiB reserve; available: $available_kib KiB. The running panel was not stopped."
    return 1
  fi
}
trap failed ERR
status running "Downloading the official LiteCP release."
for tool in curl python3 go systemctl flock; do command -v "$tool" >/dev/null; done
exec 9>/run/lock/litecp-panel-update.lock
flock -n 9
install -d -m 0700 /usr/local/lib/litecp-updates
check_recovery_space /usr/local/lib/litecp-updates
work="$(mktemp -d /usr/local/lib/litecp-updates/update-XXXXXXXX)"
# Transient system services need not have HOME or GOPATH. Keep build caches
# private to this update rather than relying on an interactive root login.
export GOPATH="$work/go" GOMODCACHE="$work/go/pkg/mod" GOCACHE="$work/go-build"
python3 - "$tag" "$work/source.tar.gz" <<'PY'
import os,re,stat,subprocess,sys
token=''
try:
    fd=os.open('/etc/litecp/github-update-token',os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK|os.O_CLOEXEC)
except FileNotFoundError:
    pass
else:
    with os.fdopen(fd,'rb') as source:
        info=os.fstat(source.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_uid!=0 or stat.S_IMODE(info.st_mode)!=0o600:
            raise SystemExit('Private update credential must be root-owned with mode 0600')
        data=source.read(4097)
    if len(data)>4096:raise SystemExit('Invalid private update credential')
    try:token=data.decode('ascii').strip()
    except UnicodeError:raise SystemExit('Invalid private update credential')
    if not re.fullmatch(r'[A-Za-z0-9_]{20,1024}',token):raise SystemExit('Invalid private update credential')
# Supply authorization on stdin, never argv/environment. curl strips this
# sensitive header when GitHub redirects to a different download hostname.
config=('header = "Authorization: Bearer '+token+'"\n') if token else ''
subprocess.run(['curl','--config','-','--fail','--location','--proto','=https',
    '--proto-redir','=https','--tlsv1.2','--max-time','180','--max-filesize','52428800',
    'https://api.github.com/repos/itsmejeeban/LiteCP/tarball/'+sys.argv[1],'-o',sys.argv[2]],input=config,text=True,check=True)
PY
mkdir "$work/source"
python3 - "$work/source.tar.gz" "$work/source" <<'PY'
import pathlib,sys,tarfile
archive,dest=sys.argv[1:]
with tarfile.open(archive,'r:gz') as source:
    members=source.getmembers()
    if len(members)>20000 or sum(m.size for m in members)>256*1024*1024: raise ValueError('Archive too large')
    root=None
    for member in members:
        parts=pathlib.PurePosixPath(member.name).parts
        if not parts or member.name.startswith('/') or '..' in parts or not (member.isfile() or member.isdir()): raise ValueError('Unsafe archive member')
        if root is None: root=parts[0]
        if root!=parts[0]: raise ValueError('Unexpected archive root')
        target=pathlib.Path(dest).joinpath(*parts[1:])
        if member.isdir(): target.mkdir(parents=True,exist_ok=True)
        else:
            target.parent.mkdir(parents=True,exist_ok=True)
            with source.extractfile(member) as src,open(target,'xb') as out:
                import shutil
                shutil.copyfileobj(src,out)
PY
completed_stages=1
status running "Building LiteCP. Existing websites remain online."
cd "$work/source"
BUILD_REVISION="$(python3 - "$work/source.tar.gz" <<'PY'
import re,sys,tarfile
with tarfile.open(sys.argv[1],'r:gz') as archive:
    root=archive.getmembers()[0].name.split('/')[0]
    match=re.search(r'-([0-9a-f]{7,40})$',root)
    print(match.group(1) if match else '')
PY
)"
CGO_ENABLED=0 go build -p=1 -mod=readonly -trimpath -ldflags "-s -w -X main.version=${tag#v} -X main.buildRevision=${BUILD_REVISION}" -o "$work/next-litecp" .
cp --preserve=mode /usr/local/bin/litecp "$work/previous-litecp"
if [[ -f /usr/local/lib/litecp/system-snapshot.py ]]; then cp -p /usr/local/lib/litecp/system-snapshot.py "$work/previous-system-snapshot.py"; fi
python3 "$work/source/scripts/write-panel-recovery-manifest.py" "$work"
bash "$work/source/scripts/install-privilege-services.sh" --prepare-only
check_recovery_space "$work"
completed_stages=2
status running "Saving the panel recovery copy. Existing websites remain online."
panel_stop_started=1
systemctl stop litecp
cp -a "$data_dir" "$work/panel-data"
completed_stages=3
status running "Restarting LiteCP and checking the new version."
LITECP_SERVICE_BACKUP_DIR="$work/service-recovery" bash "$work/source/scripts/install-privilege-services.sh"
binary_install_started=1
install -m 0755 "$work/next-litecp" /usr/local/bin/litecp
systemctl try-restart litecp-captcha.service
install -d -m 0755 /usr/local/lib/litecp
install -m 0700 "$work/source/scripts/system-snapshot.py" /usr/local/lib/litecp/system-snapshot.py
install -m 0700 "$work/source/scripts/configure-private-updates.sh" /usr/local/lib/litecp/configure-private-updates.sh
install -m 0700 "$work/source/scripts/panel-rollback.sh" /usr/local/lib/litecp/panel-rollback.sh
bash "$work/source/scripts/repair-waf-labels.sh"
python3 "$work/source/scripts/repair-dns-runtime.py"
bash "$work/source/scripts/repair-php73-selinux.sh"
bash "$work/source/scripts/repair-migration-runtime-labels.sh"
systemctl start litecp
bash "$work/source/scripts/repair-webmail-runtime.sh"
for attempt in $(seq 1 45); do
  if bash "$work/source/scripts/check-privilege-services.sh" "${tag#v}"; then
    completed_stages=4
    status success "LiteCP was updated successfully. Sign in again if your session ended."
    exit 0
  fi
  sleep 1
done
journalctl -u litecp -u litecp-broker -n 50 --no-pager || true
false
