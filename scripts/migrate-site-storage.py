#!/usr/bin/env python3
"""Offline, copy-only website-home migration. Originals and recovery files are retained.

Stop LiteCP, Nginx, cron and FTP before --apply; restart with the new LiteCP binary
afterwards so PHP pools and custom cron scripts reconcile against the new roots.
"""
import argparse
import copy
import datetime
import json
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess

BASE = Path('/home/litecp')
DOMAIN = re.compile(r'^[a-zA-Z0-9][a-zA-Z0-9.-]*[a-zA-Z0-9]$')


def check_tree(root):
    """Reject links and special files before copying any site."""
    for current, dirs, files in os.walk(root, followlinks=False):
        for name in dirs + files:
            item = Path(current) / name
            mode = item.lstat().st_mode
            if not (stat.S_ISDIR(mode) or stat.S_ISREG(mode)):
                raise RuntimeError('Unsafe link or special file: ' + str(item))


def copy_tree(source, destination):
    check_tree(source)
    shutil.copytree(source, destination, copy_function=shutil.copy2)
    # copy2 preserves mode and times; preserve isolated ownership as well.
    for current, dirs, files in os.walk(source):
        for item in [Path(current)] + [Path(current) / name for name in files]:
            info = item.stat()
            target = destination / item.relative_to(source)
            os.chown(target, info.st_uid, info.st_gid)


def migration_plan(config):
    plan = []
    for site in config['sites']:
        old = Path(site['root'])
        if old.parent.parent == BASE and old.name == 'public_html':
            continue
        if old.name != 'public' or old.parent.parent != Path('/var/www/litecp'):
            raise RuntimeError('Unsupported source root for ' + site['domain'])
        if not DOMAIN.fullmatch(site['domain']):
            raise RuntimeError('Invalid domain')
        home = BASE / site['domain']
        if home.exists() or home.is_symlink():
            raise RuntimeError('Destination already exists; inspect before retrying: ' + str(home))
        if (site.get('application') or {}).get('enabled'):
            raise RuntimeError('Disable application hosting before migration: ' + site['domain'])
        if old.is_symlink() or old.parent.is_symlink() or not old.is_dir():
            raise RuntimeError('Unsafe source root: ' + str(old))
        check_tree(old)
        plan.append((site, old, home))
    return plan


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--data-dir', default='/var/lib/litecp')
    parser.add_argument('--apply', action='store_true')
    args = parser.parse_args()
    data = Path(args.data_dir)
    config_path = data / 'config.json'
    config = json.loads(config_path.read_text(encoding='utf-8'))
    plan = migration_plan(config)
    for site, old, home in plan:
        print(site['domain'] + ': ' + str(old) + ' -> ' + str(home / 'public_html'))
    if not args.apply or not plan:
        return
    if os.geteuid() != 0:
        raise RuntimeError('Run as root')
    for service in ('litecp', 'nginx', 'crond', 'cron', 'pure-ftpd', 'litecp-ftps'):
        if subprocess.run(['systemctl', 'is-active', '--quiet', service]).returncode == 0:
            raise RuntimeError('Stop ' + service + ' before migrating')
    if BASE.is_symlink() or Path('/home').is_symlink():
        raise RuntimeError('Managed storage parent must not be a symbolic link')
    BASE.mkdir(mode=0o755, exist_ok=True)
    # Count every copied tree plus a safety margin before writing destinations.
    needed = 0
    for site, old, home in plan:
        for root in (old, data / 'website-backups' / site['id'], Path('/var/www/litecp/.trash') / site['id']):
            if root.exists():
                check_tree(root)
                needed += sum(p.stat().st_size for p in root.rglob('*') if p.is_file())
        needed += sum(Path(b['path']).stat().st_size for b in config.get('backups', [])
                      if b.get('siteId') == site['id'] and Path(b.get('path', '')).is_file())
    if shutil.disk_usage(BASE).free < needed + 256 * 1024 * 1024:
        raise RuntimeError('Insufficient free space for a copy and safety margin')
    stamp = datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%S%fZ')
    recovery = data / 'storage-migrations' / stamp
    recovery.mkdir(parents=True, mode=0o700)
    originals = {}

    def preserve(path):
        if path not in originals and path.is_file():
            originals[path] = path.read_bytes()
            target = recovery / str(path).lstrip('/')
            target.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
            shutil.copy2(path, target)
            target.chmod(0o600)

    def write(path, content):
        preserve(path)
        with path.open('wb') as stream:
            stream.write(content)
            stream.flush()
            os.fsync(stream.fileno())

    preserve(config_path)
    updated = copy.deepcopy(config)
    try:
        replacements = {}
        for site, old, home in plan:
            home.mkdir(mode=0o755)
            for name in ('backups', 'backups/manual', 'backups/scheduled', '.trash', 'tmp', 'logs', 'ssl', 'etc'):
                (home / name).mkdir(mode=0o700)
            for name in ('tmp',):
                os.chown(home / name, site['runtimeUid'], site['runtimeGid'])
            public = home / 'public_html'
            copy_tree(old, public)
            for source, target in ((data / 'website-backups' / site['id'], home / 'backups/manual'),
                                   (Path('/var/www/litecp/.trash') / site['id'], home / '.trash')):
                if source.exists():
                    # The destination is empty; retain both source and copied data.
                    for child in source.iterdir():
                        if child.is_dir():
                            copy_tree(child, target / child.name)
                        else:
                            shutil.copy2(child, target / child.name)
            for metadata in (home / '.trash').glob('*/metadata.json'):
                value = json.loads(metadata.read_text())
                value['originalPath'] = 'public_html/' + value['originalPath']
                metadata.write_text(json.dumps(value), encoding='utf-8')
                metadata.chmod(0o600)
            for backup in updated.get('backups', []):
                if backup.get('siteId') == site['id'] and Path(backup.get('path', '')).is_file():
                    source = Path(backup['path'])
                    target = home / 'backups/scheduled' / source.name
                    shutil.copy2(source, target)
                    target.chmod(0o600)
                    backup['path'] = str(target)
            replacements[str(old)] = str(public)
            saved = next(s for s in updated['sites'] if s['id'] == site['id'])
            saved['root'] = str(public)
            app = saved.get('application') or {}
            if app.get('backup') and Path(app['backup']).is_file():
                target = home / 'etc/nginx-before-application.conf'
                target.write_text(Path(app['backup']).read_text().replace(str(old), str(public)))
                target.chmod(0o600)
                app['backup'] = str(target)

        paths = list(Path('/etc/nginx/conf.d').glob('litecp-*.conf'))
        paths += list(Path('/etc/nginx/sites-available').glob('litecp-*.conf'))
        paths += list(Path('/etc/cron.d').glob('litecp-*'))
        paths += list((data / 'website-cron').rglob('*.json'))
        paths += list((data / 'website-cron').rglob('*.sh'))
        for path in paths:
            text = path.read_text()
            changed = text
            for old, new in replacements.items():
                changed = changed.replace(old, new)
                if path.suffix == '.conf' and str(path).startswith('/etc/nginx/') and new in changed:
                    home = str(Path(new).parent)
                    if home + '/logs/access.log' not in changed:
                        changed = changed.replace('root ' + new + ';', 'root ' + new + ';\n    access_log ' + home + '/logs/access.log;\n    error_log ' + home + '/logs/error.log;')
            if changed != text:
                write(path, changed.encode())
        ftp = data / 'ftp/users.passwd'
        if ftp.is_file():
            lines = []
            for line in ftp.read_text().splitlines():
                fields = line.split(':')
                if len(fields) > 5:
                    for old, new in replacements.items():
                        if fields[5].rstrip('/').removesuffix('/.') == old:
                            fields[5] = str(Path(new).parent) + '/./'
                lines.append(':'.join(fields))
            preserve(data / 'ftp/users.pdb')
            write(ftp, ('\n'.join(lines) + '\n').encode())
            subprocess.run(['pure-pw', 'mkdb', str(data / 'ftp/users.pdb'), '-f', str(ftp)], check=True)
        subprocess.run(['nginx', '-t'], check=True)
        write(config_path, (json.dumps(updated, indent=2) + '\n').encode())
        print('Migration complete. Original website directories retained. Recovery: ' + str(recovery))
    except BaseException:
        for path, content in reversed(list(originals.items())):
            path.write_bytes(content)
        print('Configuration restored. Copied homes retained for inspection. Recovery: ' + str(recovery))
        raise


if __name__ == '__main__':
    main()
