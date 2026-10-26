#!/usr/bin/env python3
"""Fixed root-SSH exporter. No uploaded code or source-deletion commands."""
import base64
import copy
import hashlib
import json
import os
import pathlib
import re
import shutil
import subprocess
import sys
import tarfile
import time

def command(args, log=None):
    result = subprocess.run(args, stdout=log or subprocess.PIPE, stderr=log or subprocess.PIPE, text=log is None, env={**os.environ, 'LC_ALL':'C'}, timeout=5400)
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

def cpanel_portable(archive, portable):
    website_files, skipped = 0, 0
    with tarfile.open(archive,'r:*') as original, tarfile.open(portable,'w:gz') as target:
        for member in original:
            parts = pathlib.PurePosixPath(member.name).parts
            destination = None
            if 'public_html' in parts:
                destination = '/'.join(parts[parts.index('public_html'):])
            elif 'mysql' in parts and member.name.lower().endswith('.sql') and parts[-1].lower() != 'mysql.sql':
                destination = 'mysql/' + parts[-1]
            if not destination:
                continue
            if not (member.isfile() or member.isdir()):
                skipped += 1
                continue
            if '..' in pathlib.PurePosixPath(destination).parts:
                raise RuntimeError('Unsafe website path in native cPanel backup; native export retained')
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
    return website_files, skipped

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
    else:
        raise RuntimeError('Unsupported source platform')
    if mode == 'discover':
        return {'accounts':accounts, 'platform':platform, 'python':sys.version.split()[0]}
    if mode != 'capture' or request.get('account') not in [x['id'] for x in accounts]:
        raise RuntimeError('Select an account discovered on this source')
    account, job = request['account'], request['job']
    if not re.fullmatch(r'[A-Za-z0-9_-]{16,64}',job):
        raise RuntimeError('Invalid job identifier')
    if not re.fullmatch(r'[a-zA-Z0-9_.-]{1,253}', account) or account.startswith('-'):
        raise RuntimeError('Invalid account identifier')
    root = pathlib.Path('/root') / ('litecp-export-' + job)
    root.mkdir(mode=0o700, exist_ok=False)
    log = open(root/'export.log','x')
    os.chmod(root/'export.log',0o600)
    if platform == 'cpanel':
        command(['/usr/local/cpanel/scripts/pkgacct',account,str(root)],log)
        archive = root / ('cpmove-' + account + '.tar.gz')
        if not archive.is_file() or archive.is_symlink():
            raise RuntimeError('The cPanel native archive was not created at its expected location')
        result = {'native':describe(archive), 'portable':None, 'sourceExport':str(root), 'warnings':['The native archive retains account data. Website import does not activate mail, DNS, SSL, package settings or arbitrary cron jobs.']}
        portable = root/'cpanel-website.tar.gz'
        website_files, skipped = cpanel_portable(archive, portable)
        os.chmod(portable,0o600)
        if website_files:
            result['portable'] = describe(portable)
        else:
            result['warnings'].append('Primary public_html files were not found in a compatible native layout. The complete native archive is retained for manual review.')
        if skipped:
            result['warnings'].append(str(skipped) + ' website links/special entries require review; native account data remains retained.')
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
