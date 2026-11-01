#!/usr/bin/env python3
"""Fixed root-SSH exporter. No uploaded code or source-deletion commands."""
import base64
import copy
import hashlib
import json
import os
import io
import pathlib
import re
import shutil
import subprocess
import sys
import tarfile
import time

def command(args, log=None):
    result = subprocess.run(args, stdout=log or subprocess.PIPE, stderr=log or subprocess.PIPE, universal_newlines=log is None, env={**os.environ, 'LC_ALL':'C'}, timeout=5400)
    if result.returncode:
        raise RuntimeError('Source command failed: ' + pathlib.Path(args[0]).name + '; inspect the retained source export log')
    return result.stdout if log is None else ''

def digest(path):
    value = hashlib.sha256()
    with open(path, 'rb') as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b''):
            value.update(chunk)
    return value.hexdigest()

def describe(path):
    return {'path':str(path), 'size':path.stat().st_size, 'sha256':digest(path)}

def cleanup_cpanel_export(root, account):
    """Remove only the two archives created for this cPanel migration job."""
    removed = []
    for name in ('cpmove-' + account + '.tar.gz', 'cpanel-website.tar.gz'):
        archive = root / name
        if archive.is_symlink():
            raise RuntimeError('Refusing to remove a symbolic-link migration archive')
        if archive.exists():
            if not archive.is_file():
                raise RuntimeError('Refusing to remove an unexpected migration archive path')
            archive.unlink()
            removed.append(name)
    return removed

def cpanel_portable(archive, portable, source_home=""):
    website_files, private_files, skipped = 0, 0, 0
    # cPanel applications commonly keep private source/configuration beside
    # public_html. Preserve application-owned directories, but never copy
    # account mail, credentials, logs, caches or other cPanel-managed areas.
    excluded_home_directories = {
        'public_html', 'public_ftp', 'mail', 'etc', 'logs', 'ssl', 'tmp',
        '.cpanel', '.cphorde', '.htpasswds', '.ssh', '.trash', '.softaculous',
    }
    with tarfile.open(archive,'r:*') as original, tarfile.open(portable,'w:gz') as target:
        for member in original:
            parts = pathlib.PurePosixPath(member.name).parts
            destination = None
            # Match account-level paths only. Never confuse a website's own
            # mail/etc/cron directories with cPanel's account metadata.
            relative_account = parts[1:] if parts and parts[0] not in ('homedir', 'mysql', 'cron') else parts
            if relative_account and relative_account[0] == 'cron' and len(relative_account) == 2:
                destination = 'cpanel_account/cron/' + relative_account[1]
            elif len(relative_account) >= 3 and relative_account[0] == 'homedir' and relative_account[1] in ('mail', 'etc'):
                relative = relative_account[2:]
                if relative_account[1] == 'mail' and len(relative) >= 2 and '.' in relative[0]:
                    destination = 'cpanel_account/mail/' + '/'.join(relative)
                elif relative_account[1] == 'etc' and len(relative) == 2 and relative[1] in ('shadow', 'passwd', 'quota'):
                    destination = 'cpanel_account/etc/' + '/'.join(relative)
            elif 'public_html' in parts:
                destination = '/'.join(parts[parts.index('public_html'):])
            elif 'mysql' in parts and member.name.lower().endswith('.sql') and parts[-1].lower() != 'mysql.sql':
                destination = 'mysql/' + parts[-1]
            elif 'homedir' in parts:
                home_index = parts.index('homedir')
                relative = parts[home_index + 1:]
                if relative and relative[0] not in excluded_home_directories and (not relative[0].startswith('.') or relative[0] == '.env'):
                    destination = 'private_home/' + '/'.join(relative)
            if not destination:
                continue
            if not (member.isfile() or member.isdir()):
                skipped += 1
                continue
            if '..' in pathlib.PurePosixPath(destination).parts:
                raise RuntimeError('Unsafe website path in native cPanel backup; native export retained')
            if destination.startswith("cpanel_account/mail/"):
                destination = destination.replace("%", "%25").replace(":", "%3A")
            info = copy.copy(member)
            info.name = destination
            info.pax_headers = {}
            info.uid = info.gid = 0
            info.uname = info.gname = ''
            if member.isfile():
                with original.extractfile(member) as content:
                    target.addfile(info,content)
            else:
                target.addfile(info)
            if destination.startswith('public_html/') and member.isfile():
                website_files += 1
            elif destination.startswith('private_home/') and member.isfile():
                private_files += 1
        metadata = json.dumps({'version':1, 'sourceHome':source_home}).encode('utf-8')
        info = tarfile.TarInfo('cpanel_account/format.json')
        info.size = len(metadata)
        info.mode = 0o600
        target.addfile(info, io.BytesIO(metadata))
    return website_files, private_files, skipped

def export(request):
    if os.geteuid() != 0:
        raise RuntimeError('Root SSH access is required')
    platform, mode = request['platform'], request['mode']
    if platform == 'cpanel':
        binary = '/usr/local/cpanel/bin/whmapi1'
        if not pathlib.Path(binary).is_file():
            raise RuntimeError('cPanel WHM API command is not installed on this source')
        response = json.loads(command([binary, '--output=json', 'listaccts']))
        if response.get('metadata', {}).get('result') != 1:
            raise RuntimeError('cPanel account discovery was rejected')
        accounts = [{'id':x['user'], 'domain':x.get('domain','')} for x in response.get('data',{}).get('acct',[])]
    elif platform == 'plesk':
        binary = shutil.which('plesk')
        if not binary:
            raise RuntimeError('Plesk CLI is not installed on this source')
        accounts = [{'id':value.strip(), 'domain':value.strip()} for value in command([binary,'bin','subscription','--list']).splitlines() if value.strip()]
    elif platform == 'litecp':
        with open('/var/lib/litecp/config.json') as config_file:
            config = json.load(config_file)
        accounts = [{'id': x['id'], 'domain': x['domain']} for x in config.get('sites', [])]
    else:
        raise RuntimeError('Unsupported source platform')
    if mode == 'discover':
        return {'accounts':accounts, 'platform':platform, 'python':sys.version.split()[0]}
    if mode not in ('capture', 'cleanup') or request.get('account') not in [x['id'] for x in accounts]:
        raise RuntimeError('Select an account discovered on this source')
    account, job = request['account'], request['job']
    if not re.fullmatch(r'[A-Za-z0-9_-]{16,64}',job):
        raise RuntimeError('Invalid job identifier')
    if not re.fullmatch(r'[a-zA-Z0-9_.-]{1,253}', account) or account.startswith('-'):
        raise RuntimeError('Invalid account identifier')
    root = pathlib.Path('/root') / ('litecp-export-' + job)
    if mode == 'cleanup':
        if platform != 'cpanel':
            raise RuntimeError('Source archive cleanup is supported only for cPanel migrations')
        if not root.is_dir() or root.is_symlink():
            raise RuntimeError('The cPanel migration export directory is missing or unsafe')
        removed = cleanup_cpanel_export(root, account)
        return {'sourceExport':str(root), 'warnings':['Removed verified source archive(s): ' + ', '.join(removed)] if removed else []}
    root.mkdir(mode=0o700, exist_ok=False)
    log = open(root/'export.log','x')
    os.chmod(root/'export.log',0o600)
    if platform == 'litecp':
        site = next(x for x in config['sites'] if x['id'] == account)
        source = pathlib.Path(site['root'])
        resolved = source.resolve()
        if source.is_symlink() or not source.is_dir() or not any(str(resolved).startswith(prefix) for prefix in ('/home/litecp/', '/var/www/litecp/')):
            raise RuntimeError('Unsafe LiteCP website root')
        archive = root / 'litecp-website.tar.gz'
        skipped = []
        def filter_member(info):
            if not (info.isfile() or info.isdir()):
                skipped.append(info.name)
                return None
            info.uid = info.gid = 0
            info.uname = info.gname = ''
            return info
        databases = [item['name'] for item in config.get('databases', []) if item.get('siteId') == account]
        if site.get('database') and site['database'] not in databases:
            databases.append(site['database'])
        dumps = []
        for index, database in enumerate(databases):
            if not re.fullmatch(r'[A-Za-z0-9_][A-Za-z0-9_$-]{0,63}', database) or database.lower() in ('mysql','sys','information_schema','performance_schema'):
                raise RuntimeError('Unsafe database name')
            dump = root / ('database-' + str(index) + '.sql')
            with open(dump, 'xb') as destination:
                result = subprocess.run(['mysqldump','--protocol=socket','--user=root','--single-transaction','--routines','--triggers',database],stdout=destination,stderr=log,timeout=3600)
            os.chmod(dump, 0o600)
            if result.returncode:
                raise RuntimeError('LiteCP database export failed; source export log retained')
            dumps.append((dump, 'databases/' + database + '.sql'))
        with tarfile.open(archive, 'w:gz', dereference=False) as target:
            target.add(source, arcname='public_html', filter=filter_member)
            for dump, name in dumps:
                target.add(dump, arcname=name, filter=filter_member)
        os.chmod(archive, 0o600)
        return {'native':describe(archive), 'portable':describe(archive), 'sourceExport':str(root), 'warnings':['LiteCP website files and registered databases captured. Mail, DNS, SSL, cron and application credentials require separate configuration.', str(len(skipped)) + ' links or special entries omitted.']}
    if platform == 'cpanel':
        command(['/usr/local/cpanel/scripts/pkgacct',account,str(root)],log)
        archive = root / ('cpmove-' + account + '.tar.gz')
        if not archive.is_file() or archive.is_symlink():
            raise RuntimeError('The cPanel native archive was not created at its expected location')
        result = {'native':describe(archive), 'portable':None, 'sourceExport':str(root), 'warnings':['cPanel mailboxes, Maildir messages and compatible account cron jobs are imported with the website. DNS, SSL, forwarders and package settings require separate review.']}
        portable = root/'cpanel-website.tar.gz'
        import pwd
        website_files, private_files, skipped = cpanel_portable(archive, portable, pwd.getpwnam(account).pw_dir)
        os.chmod(portable,0o600)
        if website_files:
            result['portable'] = describe(portable)
        else:
            result['warnings'].append('Primary public_html files were not found in a compatible native layout. The complete native archive is retained for manual review.')
        if skipped:
            result['warnings'].append(str(skipped) + ' website links/special entries require review; native account data remains retained.')
        if private_files:
            result['warnings'].append(str(private_files) + ' private application files beside public_html were captured for automatic restore.')
        return result
    # Preserve a complete native subscription backup, including Plesk's local
    # backup copy. The portable supplement is separate from that native backup.
    archive = root/'plesk-native.tar'
    command([binary,'bin','pleskbackup','--domains-name',account,'-output-file',str(archive),'-keep-local-backup'],log)
    if not archive.is_file() or archive.is_symlink():
        raise RuntimeError('The Plesk native archive was not created')
    output = {'native':describe(archive), 'portable':None, 'sourceExport':str(root), 'warnings':['Complete native Plesk backup retained. Mail, DNS, SSL and platform configuration require separate compatibility review before cutover.']}
    # This documented read-only join discovers real roots rather than assuming
    # that every domain uses /var/www/vhosts/<domain>/httpdocs.
    query = "SELECT name,login,www_root FROM domains JOIN hosting ON domains.id=hosting.dom_id JOIN sys_users ON hosting.sys_user_id=sys_users.id WHERE name='" + account + "'"
    rows = command([binary,'db','-N','-B','-e',query]).strip().splitlines()
    if len(rows) != 1 or len(rows[0].split('\t')) != 3:
        output['warnings'].append('Portable website export unavailable: document-root discovery returned an unsupported layout. Native backup is intact.')
        return output
    name, login, document_root = rows[0].split('\t')
    source = pathlib.Path(document_root)
    if not source.is_absolute() or not source.is_dir() or source.is_symlink() or str(source) in ('/','/root','/etc','/var','/var/www'):
        output['warnings'].append('Portable website export unavailable: unsafe or missing document root. Native backup is intact.')
        return output
    portable = root/'plesk-website.tar.gz'
    # Explicit directory traversal includes hidden files but never dereferences
    # symlinks or hard-links. Unsupported entries are reported, not hidden.
    skipped = []
    dumps = []
    database_names = command([binary,'db','-N','-B','-e',"SELECT data_bases.name FROM data_bases JOIN domains ON data_bases.dom_id=domains.id WHERE domains.name='" + account + "'"]).splitlines()
    for index, database in enumerate(database_names):
        database = database.strip()
        if not re.fullmatch(r'[A-Za-z0-9_][A-Za-z0-9_$-]{0,63}',database) or database.lower() in ('psa','mysql','information_schema','performance_schema','sys'):
            output['warnings'].append('A database name requires manual export; its native backup remains retained.')
            continue
        dump = root / ('database-' + str(index + 1) + '.sql')
        with open(dump,'xb') as destination:
            result = subprocess.run([binary,'db','dump',database,'--single-transaction','--skip-lock-tables','--routines','--triggers'],stdout=destination,stderr=log,timeout=3600)
        os.chmod(dump,0o600)
        if result.returncode:
            output['warnings'].append('Portable database export failed for ' + database + '; restore it from the verified native backup before cutover.')
        else:
            dumps.append((dump,'databases/' + database + '.sql'))
    def filter_member(info):
        if not (info.isfile() or info.isdir()):
            skipped.append(info.name)
            return None
        info.uid = info.gid = 0
        info.uname = info.gname = ''
        return info
    with tarfile.open(portable,'w:gz',dereference=False) as target:
        target.add(source,arcname='httpdocs',filter=filter_member)
        for dump, name in dumps:
            target.add(dump,arcname=name,filter=filter_member)
    os.chmod(portable,0o600)
    output['portable'] = describe(portable)
    if skipped:
        output['warnings'].append(str(len(skipped)) + ' links/special entries omitted from portable files; retain native backup for review.')
    output['warnings'].append(str(len(dumps)) + ' portable database dump(s) exported. Native backup retains the subscription data. Map and restore every required database before cutover.')
    return output

if __name__ == '__main__':
    try:
        request = json.loads(base64.b64decode(os.environ['LITECP_EXPORT_REQUEST']))
        print(json.dumps(export(request)))
    except Exception as error:
        print(json.dumps({'error':str(error)}))
        sys.exit(1)
