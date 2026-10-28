set -Eeuo pipefail
tag="$1"
data_dir="$2"
[[ "$tag" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+$ ]]
status() {
  python3 - "$data_dir/panel-update.json" "$1" "$2" "$tag" <<'PY'
import json,os,sys,tempfile
path,state,message,version=sys.argv[1:]
fd,tmp=tempfile.mkstemp(dir=os.path.dirname(path),prefix='.panel-update-')
with os.fdopen(fd,'w') as out: json.dump(dict(state=state,message=message,version=version),out)
os.replace(tmp,path)
PY
}
switched=0
work=''
failed() {
  trap - ERR
  if [[ "$switched" == 1 ]]; then
    systemctl stop litecp || true
    cp --preserve=mode "$work/previous-litecp" /usr/local/bin/litecp
    systemctl start litecp || true
  fi
  status failed "Update failed. The previous panel binary has been retained and restored if necessary. Review journalctl -u litecp-panel-update."
  exit 1
}
trap failed ERR
status running "Downloading the official LiteCP release."
for tool in curl python3 go systemctl; do command -v "$tool" >/dev/null; done
install -d -m 0700 /usr/local/lib/litecp-updates
work="$(mktemp -d /usr/local/lib/litecp-updates/update-XXXXXXXX)"
curl --fail --location --proto '=https' --tlsv1.2 --max-time 180 --max-filesize 52428800 "https://api.github.com/repos/itsmejeeban/LiteCP/tarball/$tag" -o "$work/source.tar.gz"
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
status running "Restarting LiteCP and checking the new version."
systemctl stop litecp
switched=1
cp -a "$data_dir" "$work/panel-data"
install -m 0755 "$work/next-litecp" /usr/local/bin/litecp
systemctl start litecp
for attempt in $(seq 1 45); do
  if curl --fail --silent --max-time 2 http://127.0.0.1:9090/api/version | python3 -c 'import json,sys;sys.exit(json.load(sys.stdin).get("version")!=sys.argv[1])' "${tag#v}"; then
    status success "LiteCP was updated successfully. Sign in again if your session ended."
    exit 0
  fi
  sleep 1
done
false
