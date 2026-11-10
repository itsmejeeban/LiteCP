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
import stat
import subprocess
import sys
import tarfile
import tempfile
import time

class SourceCommandError(RuntimeError):
    def __init__(self, executable, returncode):
        super().__init__('Source command failed: ' + executable + '; inspect the retained source export log')
        self.returncode = returncode

def command(args, log=None):
    result = subprocess.run(args, stdout=log or subprocess.PIPE, stderr=log or subprocess.PIPE, universal_newlines=log is None, env={**os.environ, 'LC_ALL':'C'}, timeout=5400)
    if result.returncode:
        raise SourceCommandError(pathlib.Path(args[0]).name, result.returncode)
    return result.stdout if log is None else ''

def litecp_dump_binary():
    binary = shutil.which('mysqldump') or shutil.which('mariadb-dump')
    if not binary:
        raise RuntimeError('LiteCP database export requires mysqldump or mariadb-dump; source export log retained')
    return binary

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

def cpanel_account_home(account):
    import pwd
    record = pathlib.Path('/var/cpanel/users') / account
    if not record.is_file() or record.is_symlink():
        raise RuntimeError('Selected cPanel account is no longer available on this source')
    try:
        user = pwd.getpwnam(account)
    except KeyError:
        raise RuntimeError('Selected cPanel account has no local system user')
    if user.pw_uid == 0 or not os.path.isabs(user.pw_dir):
        raise RuntimeError('Selected cPanel account has an unsafe system user or home directory')
    return user.pw_dir

def cpanel_storage_exhausted(diagnostic):
    diagnostic = diagnostic.lower()
    return 'no space left' in diagnostic or 'disk quota exceeded' in diagnostic

def cpanel_missing_database_map(diagnostic):
    return re.search(r'\bcpusernotinmap\b|\buser [^\r\n]{1,180}does not exist in (?:the )?database map\b', diagnostic, re.IGNORECASE) is not None

def cpanel_database_map_missing(account):
    # Only a confirmed source map error should stop native capture. Older
    # versions and accounts with unavailable MySQL features still use pkgacct.
    try:
        result = subprocess.run(['/usr/local/cpanel/bin/whmapi1', '--output=json',
                                 'list_mysql_databases_and_users', 'user=' + account],
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                universal_newlines=True, env={**os.environ, 'LC_ALL':'C'}, timeout=15)
    except (OSError, subprocess.TimeoutExpired):
        return False
    try:
        response = json.loads(result.stdout)
    except ValueError:
        response = None
    metadata = response.get('metadata') if isinstance(response, dict) else None
    reason = metadata.get('reason') if isinstance(metadata, dict) else None
    # WHM may emit the map error on stderr while reporting a successful
    # API result. Never scan the database names or other returned data.
    return cpanel_missing_database_map(result.stderr) or (isinstance(reason, str) and cpanel_missing_database_map(reason))

def cpanel_database_map_preview_empty(account):
    try:
        result = subprocess.run(['/usr/local/cpanel/scripts/rebuild_dbmap', account, '--noop'],
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                universal_newlines=True, env={**os.environ, 'LC_ALL':'C'}, timeout=30)
    except (OSError, subprocess.TimeoutExpired):
        return False
    header, separator, proposed_map = result.stdout.partition('\n---\n')
    # An absent map alone says nothing about whether databases exist. Only
    # accept the provider's explicit, successfully generated empty preview.
    return (result.returncode == 0 and not result.stderr.strip() and bool(separator)
            and header.startswith('Operating in no-op mode;')
            and any(line.strip() == 'Done.' for line in header.splitlines())
            and proposed_map.strip().splitlines() in (['MYSQL: {}'], ['MYSQL: {}', 'PGSQL: {}']))

cpanel_blank_settings = {'public_html/.htaccess', 'public_html/.user.ini', 'public_html/php.ini'}

def cpanel_blank_root_candidate(source_home):
    root = pathlib.Path(source_home) / 'public_html'
    try:
        if not stat.S_ISDIR(root.lstat().st_mode):
            return False
        with os.scandir(root) as entries:
            return all(entry.is_dir(follow_symlinks=False) or
                       ('public_html/' + entry.name in cpanel_blank_settings and entry.is_file(follow_symlinks=False))
                       for entry in entries)
    except OSError:
        return False

def cpanel_capture_diagnostic(log_path):
    # Provider logs can contain account details and credentials. Return only
    # fixed descriptions of recognized errors from a bounded portion.
    try:
        with log_path.open('rb') as source:
            end = source.seek(0, os.SEEK_END)
            source.seek(max(0, end - 65536))
            diagnostic = source.read(65536).decode('utf-8', errors='replace').lower()
    except OSError:
        return ''
    if cpanel_missing_database_map(diagnostic):
        return 'the account is missing from the source cPanel database map; repair the map on the source before retrying'
    if cpanel_storage_exhausted(diagnostic):
        return 'the source server is out of disk space or quota'
    if 'read-only file system' in diagnostic:
        return 'the source reported a read-only filesystem'
    if 'access denied for user' in diagnostic:
        return 'the source database rejected authentication or access'
    if re.search(r"(?:can't|cannot|could not) connect to (?:local |remote )?(?:mysql|mariadb)", diagnostic):
        return 'pkgacct could not connect to the source database'
    if 'permission denied' in diagnostic or 'operation not permitted' in diagnostic:
        return 'pkgacct reported a source permission error'
    if 'input/output error' in diagnostic or 'i/o error' in diagnostic:
        return 'pkgacct reported a source storage I/O error'
    return ''

def cpanel_nested_public_root(archive):
    """Recognize a cPanel document root explicitly forwarded to public/."""
    rewrites, entrypoints = set(), set()
    with tarfile.open(archive, 'r:*') as native:
        for member in native:
            parts = pathlib.PurePosixPath(member.name).parts
            prefix = () if parts and parts[0] == 'homedir' else parts[:1]
            relative = parts[len(prefix):]
            if not member.isfile():
                continue
            if relative == ('homedir', 'public_html', '.htaccess') and member.size <= 65536:
                with native.extractfile(member) as rules:
                    text = rules.read().decode('utf-8', errors='replace')
                enabled = re.findall(r'(?mi)^[ \t]*RewriteEngine[ \t]+(On|Off)[ \t]*\r?$', text)
                forwarding = re.search(r'(?mi)^[ \t]*RewriteRule[ \t]+\^\(\.\*\)\$[ \t]+/?public/\$1[ \t]+\[(?:L|END)\][ \t]*\r?$', text)
                if enabled and enabled[-1].lower() == 'on' and forwarding:
                    rewrites.add(prefix)
            elif relative == ('homedir', 'public_html', 'public', 'index.php') and member.size > 0:
                entrypoints.add(prefix)
            if prefix in rewrites and prefix in entrypoints:
                return prefix
    return None


def cpanel_portable(archive, portable, source_home="", supplement=None, blank_only=False):
    website_files, private_files, skipped = 0, 0, 0
    website_root_found = False
    database_dumps = set()
    # cPanel applications commonly keep private source/configuration beside
    # public_html. Preserve application-owned directories, but never copy
    # account mail, credentials, logs, caches or other cPanel-managed areas.
    excluded_home_directories = {
        'public_html', 'public_ftp', 'mail', 'etc', 'logs', 'ssl', 'tmp',
        '.cpanel', '.cphorde', '.htpasswds', '.ssh', '.trash', '.softaculous',
    }
    nested_root = None if blank_only else cpanel_nested_public_root(archive)
    with tarfile.open(archive,'r:*') as original, tarfile.open(portable,'w:gz') as target:
        for member in original:
            parts = pathlib.PurePosixPath(member.name).parts
            destination = None
            # Match account-level paths only. Never confuse a website's own
            # mail/etc/cron directories with cPanel's account metadata.
            relative_account = parts[1:] if parts and parts[0] not in ('homedir', 'mysql', 'cron', 'dnszones') else parts
            if len(relative_account) == 2 and relative_account[0] == 'dnszones' and member.isfile() and member.size <= 2097152:
                destination = 'cpanel_account/dns/' + relative_account[1]
            elif relative_account and relative_account[0] == 'cron' and len(relative_account) == 2:
                destination = 'cpanel_account/cron/' + relative_account[1]
            elif len(relative_account) >= 3 and relative_account[0] == 'homedir' and relative_account[1] in ('mail', 'etc'):
                relative = relative_account[2:]
                if relative_account[1] == 'mail' and relative and ('.' in relative[0] or relative[0] in ('cur', 'new', 'tmp', 'maildirsize', 'subscriptions') or relative[0].startswith('dovecot')):
                    destination = 'cpanel_account/mail/' + '/'.join(relative)
                elif relative_account[1] == 'etc' and len(relative) == 2 and relative[1] in ('shadow', 'passwd', 'quota'):
                    destination = 'cpanel_account/etc/' + '/'.join(relative)
            elif len(relative_account) >= 2 and relative_account[:2] == ('homedir', 'public_html'):
                # WordPress extracts plugin updates here. A stale upgrade
                # workspace can contain conflicting entries and is not used
                # by the installed site. It stays in the native archive.
                if relative_account[2:4] == ('wp-content', 'upgrade'):
                    continue
                outer = relative_account[2:]
                if nested_root is not None and parts[:-len(relative_account)] == nested_root and outer:
                    if outer[0] == 'public':
                        destination = '/'.join(('public_html',) + outer[1:])
                    else:
                        destination = '/'.join(('private_home',) + outer)
                else:
                    destination = '/'.join(relative_account[1:])
            elif len(relative_account) == 2 and relative_account[0] == 'mysql' and member.isfile() and member.name.lower().endswith('.sql') and relative_account[-1].lower() != 'mysql.sql':
                # A zero-byte native entry has no schema or data. Leave it out
                # so the account-scoped database inventory can supply a dump.
                if member.size == 0:
                    continue
                destination = 'mysql/' + relative_account[-1][:-4] + '.sql'
            elif relative_account and relative_account[0] == 'homedir':
                relative = relative_account[1:]
                if relative and relative[0] not in excluded_home_directories and (not relative[0].startswith('.') or relative[0] == '.env'):
                    destination = 'private_home/' + '/'.join(relative)
            if not destination:
                continue
            if blank_only and (destination == 'public_html' or destination.startswith(('public_html/', 'private_home/'))):
                if member.issym() and destination in ('private_home/www', 'private_home/access-logs'):
                    continue  # cPanel aliases are kept in the native archive only.
                settings = member.isfile() and destination in cpanel_blank_settings
                if not (member.isdir() or settings):
                    raise RuntimeError('The source cPanel database map is missing and the captured website is not blank; repair the source map. Native export retained.')
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
                if destination.startswith('mysql/'):
                    database_dumps.add(destination[6:-4])
            else:
                target.addfile(info)
            if destination.startswith('public_html/') and member.isfile():
                website_files += 1
            elif destination == 'public_html' and member.isdir():
                website_root_found = True
            elif destination.startswith('private_home/') and member.isfile():
                private_files += 1
        if supplement is not None:
            supplement(target, database_dumps)
        metadata = json.dumps({'version':1, 'sourceHome':source_home}).encode('utf-8')
        info = tarfile.TarInfo('cpanel_account/format.json')
        info.size = len(metadata)
        info.mode = 0o600
        target.addfile(info, io.BytesIO(metadata))
    return website_files, private_files, skipped, website_root_found


def supplement_cpanel_databases(target, included, account, root, log, warnings):
    try:
        response = json.loads(command(['/usr/local/cpanel/bin/uapi', '--user=' + account,
                                       '--output=json', 'Mysql', 'list_databases']))
        result = response.get('result') if isinstance(response, dict) else None
        if not isinstance(result, dict) or result.get('status') != 1 or not isinstance(result.get('data'), list):
            raise ValueError('database inventory unavailable')
    except (OSError, ValueError, RuntimeError, subprocess.TimeoutExpired):
        warnings.append('Could not verify the cPanel database inventory. Only native database dumps were captured.')
        return False
    missing = {row['database'] for row in result['data']
               if isinstance(row, dict) and isinstance(row.get('database'), str)} - included
    if not missing:
        return not result['data'] and not included
    binary = shutil.which('mysqldump') or shutil.which('mariadb-dump')
    added = 0
    failures = set()
    for database in sorted(missing):
        if not binary:
            failures.add('mysqldump and mariadb-dump are unavailable')
            break
        if not re.fullmatch(r'[A-Za-z0-9_$][A-Za-z0-9_$-]{0,63}', database):
            failures.add('unsupported database name')
            continue
        try:
            with tempfile.TemporaryFile(dir=root) as dump, tempfile.TemporaryFile(dir=root) as errors:
                # Honor the source administrator's MySQL client defaults,
                # including the configured socket or remote database host.
                args = [binary, '--single-transaction', '--quick', '--routines', '--triggers', database]
                for options in (['--no-tablespaces'], []):
                    for stream in (dump, errors):
                        stream.seek(0)
                        stream.truncate()
                    process = subprocess.run(args[:1] + options + args[1:], stdout=dump,
                                             stderr=errors, timeout=3600)
                    errors.seek(0)
                    diagnostic = errors.read(8192).decode('utf-8', errors='replace')
                    lower = diagnostic.lower()
                    if not (options and process.returncode and 'no-tablespaces' in lower
                            and ('unknown option' in lower or 'unknown variable' in lower)):
                        break
                if process.returncode or not dump.tell():
                    if log is not None:
                        log.write(diagnostic)
                        log.flush()
                    failures.add('dump client exit status ' + str(process.returncode) if process.returncode
                                 else 'dump client produced no SQL')
                    for marker, reason in (
                        ('access denied', 'database authentication or permission denied'),
                        ('unknown database', 'database no longer exists'),
                        ("can't connect", 'cannot connect to source MySQL'),
                        ('read-only file system', 'source database export hit a read-only filesystem; repair source storage before retrying capture'),
                        ('no space left', 'source storage exhausted'),
                        ('unknown option', 'dump client rejected an option'),
                        ('unknown variable', 'dump client rejected a setting'),
                    ):
                        if marker in lower:
                            failures.add(reason)
                    continue
                entry = tarfile.TarInfo('mysql/' + database + '.sql')
                entry.size = dump.tell()
                entry.mode = 0o600
                dump.seek(0)
                target.addfile(entry, dump)
                added += 1
        except subprocess.TimeoutExpired:
            failures.add('database dump timed out')
        except OSError as error:
            failures.add('source dump I/O error ' + str(error.errno))
    if added:
        warnings.append(str(added) + ' missing account database dump(s) exported directly from source MySQL.')
    if added < len(missing):
        warnings.append('Some account databases could not be exported. Check the source export log before restoring.')
        if failures:
            warnings.append('Source database export reported: ' + '; '.join(sorted(failures)) + '.')
    return False

def plesk_backup_subscription(binary, account):
    # Plesk native backups are subscription-scoped, not individual subdomains.
    query = ("SELECT owner.name FROM domains site JOIN domains owner ON owner.id="
             "CASE WHEN site.webspace_id > 0 THEN site.webspace_id "
             "WHEN site.parentDomainId > 0 THEN site.parentDomainId ELSE site.id END "
             "WHERE site.name='" + account + "'")
    names = command([binary, 'db', '-N', '-B', '-e', query]).strip().splitlines()
    if len(names) != 1 or not re.fullmatch(r'[a-zA-Z0-9_.-]{1,253}', names[0]) or names[0].startswith('-'):
        raise RuntimeError('Cannot determine the Plesk subscription for the selected website')
    return names[0]


def plesk_website_databases(binary, account, subscription, source):
    query = ("SELECT data_bases.name, domains.name FROM data_bases JOIN domains ON data_bases.dom_id=domains.id "
             "WHERE domains.id IN (SELECT site.id FROM domains site JOIN domains owner ON owner.id="
             "CASE WHEN site.webspace_id > 0 THEN site.webspace_id WHEN site.parentDomainId > 0 "
             "THEN site.parentDomainId ELSE site.id END WHERE owner.name='" + subscription + "')")
    available, selected = set(), set()
    for line in command([binary, 'db', '-N', '-B', '-e', query]).splitlines():
        fields = line.split('\t')
        if len(fields) != 2:
            raise RuntimeError('Plesk database inventory returned an unsupported layout')
        name, owner = fields
        available.add(name)
        if owner == account:
            selected.add(name)
    # Read configuration literals only. Never execute imported application code.
    env_name = re.compile(r"(?m)^\s*(?:export\s+)?(?:DB_DATABASE|DB_NAME)\s*=\s*([^\r\n#]*)")
    php_name = re.compile(r"(?i)(?:\$(?:db_database|db_name|dbname|database|database_name)\s*=|['\"](?:DB_DATABASE|DB_NAME|dbname|database|database_name)['\"]\s*(?:=>|\]\s*=)|define\s*\(\s*['\"]DB_NAME['\"]\s*,)\s*['\"]([A-Za-z0-9_$-]+)['\"]")
    count = 0
    for directory, directories, files in os.walk(str(source), followlinks=False):
        directories[:] = [name for name in directories if name.lower() not in ('vendor', 'node_modules', '.git', 'cache', 'upgrade') and not pathlib.Path(directory, name).is_symlink()]
        for name in files:
            count += 1
            if count > 200000:
                raise RuntimeError('Too many files for Plesk database configuration discovery')
            lower = name.lower()
            if lower != '.env' and not (lower.endswith('.php') and any(word in lower for word in ('config', 'connect', 'database', 'db.php'))):
                continue
            path = pathlib.Path(directory, name)
            if path.is_symlink() or not path.is_file() or path.stat().st_size > 2 * 1024 * 1024:
                continue
            content = path.read_text(encoding='utf-8', errors='replace')
            names = env_name.findall(content) if lower == '.env' else php_name.findall(content)
            for value in names:
                value = value.strip().strip('\"\'')
                if value in available:
                    selected.add(value)
    return sorted(selected)


def export(request):
    if os.geteuid() != 0:
        raise RuntimeError('Root SSH access is required')
    platform, mode = request['platform'], request['mode']
    if mode != 'discover':
        if mode not in ('capture', 'cleanup'):
            raise RuntimeError('Unsupported source export operation')
        account, job = request['account'], request['job']
        if not re.fullmatch(r'[A-Za-z0-9_-]{16,64}',job):
            raise RuntimeError('Invalid job identifier')
        # LiteCP immutable IDs can start with a dash. Keep cPanel and Plesk
        # account identifiers strictly separate from command options.
        pattern = r'[A-Za-z0-9_-]{1,128}' if platform == 'litecp' else r'[A-Za-z0-9][A-Za-z0-9_.-]{0,252}'
        if not re.fullmatch(pattern, account):
            raise RuntimeError('Invalid account identifier')
        root = pathlib.Path('/root') / ('litecp-export-' + job)
        if mode == 'cleanup':
            if platform != 'cpanel':
                raise RuntimeError('Source archive cleanup is supported only for cPanel migrations')
            try:
                directory = root.lstat()
            except FileNotFoundError:
                raise RuntimeError('The cPanel migration export directory is missing')
            if not stat.S_ISDIR(directory.st_mode) or directory.st_uid != 0 or directory.st_mode & 0o077:
                raise RuntimeError('The cPanel migration export directory is unsafe')
            removed = cleanup_cpanel_export(root, account)
            return {'sourceExport':str(root), 'warnings':['Removed verified source archive(s): ' + ', '.join(removed)] if removed else []}
    if platform == 'cpanel' and mode == 'capture':
        # LiteCP already selected a discovered account. Recheck that account
        # locally without requiring a fresh WHM inventory for every capture.
        source_home = cpanel_account_home(account)
        skip_mysql = cpanel_database_map_missing(account)
        recreate_empty = request.get('recreate_empty_databases') == 'true'
        if skip_mysql and not ((recreate_empty or cpanel_blank_root_candidate(source_home))
                               and cpanel_database_map_preview_empty(account)):
            raise RuntimeError('cPanel capture blocked: the account is missing from the source cPanel database map; repair the map on the source before retrying. No archive was created.')
    elif platform == 'cpanel':
        binary = '/usr/local/cpanel/bin/whmapi1'
        if not pathlib.Path(binary).is_file():
            raise RuntimeError('cPanel WHM API command is not installed on this source')
        response = json.loads(command([binary, '--output=json', 'listaccts']))
        if response.get('metadata', {}).get('result') != 1:
            reason = str(response.get('metadata', {}).get('reason', ''))
            if cpanel_storage_exhausted(reason):
                raise RuntimeError('cPanel account discovery failed: the source server is out of disk space or quota')
            raise RuntimeError('cPanel account discovery was rejected by WHM; check listaccts on the source server before retrying')
        accounts = [{'id':x['user'], 'domain':x.get('domain','')} for x in response.get('data',{}).get('acct',[])]
    elif platform == 'plesk':
        binary = shutil.which('plesk')
        if not binary:
            raise RuntimeError('Plesk CLI is not installed on this source')
        # Subscriptions omit child subdomains and additional hosted domains.
        # Site inventory includes each independently hosted website and its root.
        names = command([binary,'bin','site','--list']).splitlines()
        accounts = [{'id':name, 'domain':name} for name in sorted(set(value.strip() for value in names if value.strip()))]
    elif platform == 'litecp':
        with open('/var/lib/litecp/config.json') as config_file:
            config = json.load(config_file)
        accounts = [{'id': x['id'], 'domain': x['domain']} for x in config.get('sites', [])]
    else:
        raise RuntimeError('Unsupported source platform')
    if mode == 'discover':
        return {'accounts':accounts, 'platform':platform, 'python':sys.version.split()[0]}
    if platform != 'cpanel' and account not in [x['id'] for x in accounts]:
        raise RuntimeError('Select an account discovered on this source')
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
        dump_binary = None
        if databases:
            dump_binary = litecp_dump_binary()
        for index, database in enumerate(databases):
            if not re.fullmatch(r'[A-Za-z0-9_][A-Za-z0-9_$-]{0,63}', database) or database.lower() in ('mysql','sys','information_schema','performance_schema'):
                raise RuntimeError('Unsafe database name')
            dump = root / ('database-' + str(index) + '.sql')
            with open(dump, 'xb') as destination:
                result = subprocess.run([dump_binary,'--protocol=socket','--user=root','--single-transaction','--routines','--triggers',database],stdout=destination,stderr=log,timeout=3600)
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
        log_path = root / 'export.log'
        args = ['/usr/local/cpanel/scripts/pkgacct']
        if skip_mysql:
            args.append('--skipmysql')
        try:
            command(args + [account, str(root)], log)
        except SourceCommandError as error:
            reason = cpanel_capture_diagnostic(log_path)
            detail = f': {reason}' if reason else ''
            raise RuntimeError(f'cPanel capture failed (pkgacct exit {error.returncode}){detail}. Review the source export log at {log_path}.') from None
        except subprocess.TimeoutExpired:
            raise RuntimeError(f'cPanel capture timed out. Review the source export log at {log_path}.') from None
        archive = root / ('cpmove-' + account + '.tar.gz')
        if not archive.is_file() or archive.is_symlink():
            raise RuntimeError('The cPanel native archive was not created at its expected location')
        result = {'native':describe(archive), 'portable':None, 'sourceExport':str(root), 'warnings':['cPanel mailboxes, Maildir messages and compatible account cron jobs are imported with the website. DNS zones are imported for review and export; registrar delegation, SSL, forwarders and package settings require separate review.']}
        portable = root/'cpanel-website.tar.gz'
        if skip_mysql:
            if not cpanel_database_map_preview_empty(account):
                raise RuntimeError('Could not reconfirm the empty cPanel account database inventory after capture. Native export retained.')
            if not recreate_empty:
                result['warnings'].append('Blank cPanel account captured after its source database-map preview confirmed no databases to export.')

        def supplement(target, included):
            inventory_empty = not included if skip_mysql else supplement_cpanel_databases(
                target, included, account, root, log, result['warnings'])
            if recreate_empty and inventory_empty:
                if skip_mysql or cpanel_database_map_preview_empty(account):
                    result['cpanel_database_inventory_empty'] = True
                    result['warnings'].append('The cPanel source confirmed no account databases to export.')
                else:
                    result['warnings'].append('Empty database recreation requires a verified empty cPanel database inventory.')

        website_files, private_files, skipped, website_root_found = cpanel_portable(
            archive, portable, source_home, None if skip_mysql and not recreate_empty else supplement,
            blank_only=skip_mysql and not recreate_empty)
        os.chmod(portable,0o600)
        if website_files or website_root_found:
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
    subscription = plesk_backup_subscription(binary, account)
    command([binary,'bin','pleskbackup','--domains-name',subscription,'-output-file',str(archive),'-keep-local-backup'],log)
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
    database_names = plesk_website_databases(binary, account, subscription, source)
    for index, database in enumerate(database_names):
        database = database.strip()
        if not re.fullmatch(r'[A-Za-z0-9_][A-Za-z0-9_$-]{0,63}',database) or database.lower() in ('psa','mysql','information_schema','performance_schema','sys'):
            raise RuntimeError('A required database name needs manual export; native backup retained')
        dump = root / ('database-' + str(index + 1) + '.sql')
        with open(dump,'xb') as destination:
            result = subprocess.run([binary,'db','dump',database,'--single-transaction','--skip-lock-tables','--routines','--triggers'],stdout=destination,stderr=log,timeout=3600)
        os.chmod(dump,0o600)
        if result.returncode:
            raise RuntimeError('Required portable database export failed; inspect retained source export log. Native backup retained.')
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
