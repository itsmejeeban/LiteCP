import os, pathlib, re, sys, tarfile, shutil, subprocess
archive, target, configuration, uid, gid = sys.argv[1:]
uid, gid = int(uid), int(gid)
target = pathlib.Path(target).resolve()
config = pathlib.Path(configuration)
text = config.read_text()
names = ('templates_compiledir', 'attachments_dir', 'downloads_dir', 'crons_dir')
paths = {}
for name in names:
    match = re.search(r'\$' + name + r'''\s*=\s*(['"])(/home/[^'"]+)\1\s*;''', text)
    if not match:
        continue
    old = pathlib.PurePosixPath(match.group(2))
    if len(old.parts) < 4 or '..' in old.parts:
        raise ValueError('Unsafe private application path')
    relative = pathlib.PurePosixPath(*old.parts[3:])
    if not str((target / relative).resolve()).startswith(str(target) + os.sep):
        raise ValueError('Private path escapes website home')
    if relative.parts[0] == 'public_html':
        destination = target / relative
    else:
        destination = target / relative
        paths[str(relative)] = destination
    text = text[:match.start(2)] + str(destination) + text[match.end(2):]
if paths:
    with tarfile.open(archive, 'r:*') as source:
        for member in source:
            parts = pathlib.PurePosixPath(member.name).parts
            if 'homedir' not in parts:
                continue
            relative = pathlib.PurePosixPath(*parts[parts.index('homedir')+1:])
            if '..' in relative.parts or relative.is_absolute():
                raise ValueError('Unsafe private archive member')
            if not any(str(relative) == p or str(relative).startswith(p + '/') for p in paths):
                continue
            destination = target / str(relative)
            if not str(destination.resolve()).startswith(str(target) + os.sep):
                raise ValueError('Private archive path escapes website home')
            if not (member.isfile() or member.isdir()):
                raise ValueError('Private application links require manual review')
            destination.parent.mkdir(parents=True, exist_ok=True, mode=0o750)
            if member.isdir():
                destination.mkdir(exist_ok=True, mode=0o750)
            else:
                with source.extractfile(member) as data, open(destination, 'xb') as output:
                    while True:
                        block = data.read(1024*1024)
                        if not block: break
                        output.write(block)
                os.chmod(destination, 0o640)
            os.chown(destination, uid, gid)
    for destination in paths.values():
        if not destination.is_dir():
            raise ValueError('Required private application directory missing from native backup')
    for root, dirs, files in os.walk(target):
        if pathlib.Path(root).name.startswith('.auto-migration'):
            dirs[:] = []
            continue
        if 'whmcsdata' in pathlib.Path(root).parts:
            os.chown(root, uid, gid)
    if shutil.which('semanage'):
        for destination in paths.values():
            rule = re.escape(str(destination)) + '(/.*)?'
            result = subprocess.run(['semanage', 'fcontext', '-a', '-t', 'httpd_sys_rw_content_t', rule], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            if result.returncode:
                subprocess.run(['semanage', 'fcontext', '-m', '-t', 'httpd_sys_rw_content_t', rule], check=True)
            subprocess.run(['restorecon', '-R', str(destination)], check=True)
config.write_text(text)
os.chmod(config, 0o640)
os.chown(config, uid, gid)
