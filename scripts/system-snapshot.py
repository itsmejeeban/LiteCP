#!/usr/bin/env python3
"""LiteCP portable managed-data snapshots. Run only as root via LiteCP."""
import argparse, contextlib, datetime, hashlib, json, os, pathlib, platform, re, shutil, stat, subprocess, sys, tempfile, zipfile
BASE=pathlib.Path('/var/backups/litecp/system-snapshots')
ROOTS={'panel':'/var/lib/litecp','websites':'/home/litecp','legacy-websites':'/var/www/litecp','mail':'/var/vmail','mail-config':'/etc/litecp/mail','panel-tls':'/etc/litecp/tls','certificates':'/etc/letsencrypt','backups':'/var/backups/litecp','webmail':'/var/lib/litecp-webmail','webmail-config':'/opt/litecp/webmail/config','waf':'/etc/litecp/waf'}
GENERATED_CONFIGS=['/etc/nginx/conf.d/00-litecp-waf-policy.conf','/etc/nginx/conf.d/00-litecp-traffic.conf','/etc/nginx/conf.d/00-litecp-captcha-zones.conf']
SERVICE_CONFIGS=['/etc/postfix/main.cf','/etc/postfix/master.cf','/etc/dovecot/conf.d/99-litecp.conf','/etc/dovecot/conf.d/zz-litecp-webmail-tls.conf','/etc/dovecot/conf.d/00-litecp-webmail-signon.conf','/etc/opendkim.conf','/etc/nginx/conf.d/litecp-webmail.conf','/etc/nginx/conf.d/00-litecp-captcha-zones.conf','/etc/nginx/conf.d/00-litecp-waf-policy.conf','/etc/nginx/conf.d/00-litecp-traffic.conf']
EXCLUDE={'panel':{'application-hosting/command-logs','developer-trust.json','sessions.json','developer-mode.json','operations.json'},'backups':{'system-snapshots'},'webmail':{'temp','logs'}}
def run(args, **kw):
    return subprocess.run(args, check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, **kw).stdout

def remap_dovecot_vmail_uid(config_text, uid):
    if uid <= 0:
        raise ValueError('Invalid destination vmail UID')
    updated, count = re.subn(r'(?m)^[ \t]*first_valid_uid[ \t]*=[^\r\n]*',
                             f'first_valid_uid = {uid}', config_text)
    if count > 1:
        raise ValueError('Ambiguous LiteCP Dovecot UID configuration')
    if not count:
        updated = config_text + f'\nfirst_valid_uid = {uid}\n'
    return updated

def digest(path):
    h=hashlib.sha256()
    with open(path,'rb') as f:
        for block in iter(lambda:f.read(1024*1024),b''): h.update(block)
    return h.hexdigest()

def config():
    return json.loads(pathlib.Path('/var/lib/litecp/config.json').read_text())

def osinfo():
    values={}
    for line in pathlib.Path('/etc/os-release').read_text().splitlines():
        if '=' in line:
            k,v=line.split('=',1); values[k]=v.strip('"')
    return values['ID'],values['VERSION_ID'].split('.')[0]

def managed_configs():
    patterns=['/etc/nginx/conf.d/litecp-*.conf','/etc/nginx/sites-available/litecp-*.conf','/etc/cron.d/litecp-*','/etc/php/*/fpm/pool.d/litecp*.conf','/etc/opt/remi/php*/php-fpm.d/litecp*.conf','/etc/litecp/php73-pools/*','/etc/litecp/waf/*']
    import glob
    return sorted({p for pattern in patterns for p in glob.glob(pattern) if os.path.isfile(p) and not os.path.islink(p)} | {p for p in SERVICE_CONFIGS if os.path.isfile(p)})

def safe_name(name):
    p=pathlib.PurePosixPath(name)
    if not name or '\\' in name or '\x00' in name or p.is_absolute() or '..' in p.parts or str(p)!=name.rstrip('/'):
        raise ValueError('Unsafe archive path')
    return p

def excluded(key,rel):
    # The mounted quota filesystem is captured through /home/litecp, never as
    # its live backing image. Restoring that image would corrupt active storage.
    if key=='panel' and rel=='website-storage.ext4':return True
    return any(rel==x or rel.startswith(x+'/') for x in EXCLUDE.get(key,set()))

def add_tree(z,key,root,manifest):
    root=pathlib.Path(root)
    if not root.exists(): return
    for directory,dirs,files in os.walk(root,followlinks=False):
        for name in list(dirs)+files:
            p=pathlib.Path(directory)/name; rel=p.relative_to(root).as_posix()
            if excluded(key,rel):
                if name in dirs: dirs.remove(name)
                continue
            s=p.lstat(); entry='data/'+key+'/'+rel
            if stat.S_ISLNK(s.st_mode):
                target=os.readlink(p)
                resolved=os.path.normpath(os.path.join(str(p.parent),target))
                if os.path.commonpath([str(root),resolved])!=str(root): raise ValueError('External symlink requires removal before snapshot: '+str(p))
                data=target.encode(); zi=zipfile.ZipInfo(entry);zi.create_system=3;zi.external_attr=(stat.S_IFLNK|0o777)<<16;z.writestr(zi,data)
                if name in dirs: dirs.remove(name)
            elif stat.S_ISDIR(s.st_mode):
                zi=zipfile.ZipInfo(entry+'/');zi.external_attr=(stat.S_IFDIR|stat.S_IMODE(s.st_mode))<<16;z.writestr(zi,b'');data=None
            elif stat.S_ISREG(s.st_mode):
                if key=='panel' and rel=='snapshot-settings.json':
                    settings=json.loads(p.read_text(encoding='utf-8'));settings.pop('pending',None);settings['uploadStatus']='';settings['enabled']=False
                    data=json.dumps(settings).encode('utf-8');z.writestr(entry,data)
                else:z.write(p,entry);data=None
            else: raise ValueError('Unsupported special file: '+str(p))
            manifest['files'][entry]={'mode':stat.S_IMODE(s.st_mode),'uid':s.st_uid,'gid':s.st_gid,'size':len(data) if data is not None else s.st_size,'sha256':hashlib.sha256(data).hexdigest() if data is not None else (digest(p) if p.is_file() else '')}

def pause_containers(ids=None):
    # Mutate the caller's recovery list before stopping: Podman may return an
    # error after it already stopped the process, or after earlier apps stopped.
    if ids is None:ids=[]
    if not shutil.which('podman'):return ids
    result=run(['podman','ps','--format','{{.ID}} {{.Names}}']).decode().splitlines()
    for line in result:
        fields=line.split()
        if len(fields)==2 and fields[1].startswith('litecp-app-'):
            ids.append(fields[0])
            try:run(['podman','stop','--time','20',fields[0]])
            except Exception:
                # A stop can report a conmon exit error despite successful stop.
                state=run(['podman','inspect','--format','{{.State.Status}}',fields[0]]).decode().strip()
                if state not in ('exited','stopped'):raise
    return ids

def resume_containers(ids):
    failed=[]
    for container in ids:
        try:
            run(['systemd-run','--scope','--quiet','podman','start',container])
            state=run(['podman','inspect','--format','{{.State.Status}}',container]).decode().strip()
            if state!='running':failed.append(container)
        except Exception:failed.append(container)
    return failed

def php_units():
    result=subprocess.run(['systemctl','list-units','--all','--plain','--no-legend','php*-fpm.service','php*-php-fpm.service','litecp-php73-fpm.service'],stdout=subprocess.PIPE,stderr=subprocess.DEVNULL)
    return [line.split()[0] for line in result.stdout.decode().splitlines() if line.split() and line.split()[0].endswith('.service')]

def creation_pause_units():
    # Nginx also proxies the panel HTTPS listener: stopping it disconnects admins.
    # Keep the control plane reachable while pausing application/mail writers.
    return ['crond','cron','postfix','dovecot','litecp-ftps']+php_units()

def create(destination):
    cfg=config(); osid,major=osinfo()
    manifest={'format':'litecp-system-snapshot','schema':1,'createdAt':datetime.datetime.now(datetime.timezone.utc).isoformat(),'os':osid,'osMajor':major,'architecture':platform.machine(),'sites':len(cfg.get('sites',[])),'databases':len(cfg.get('databases',[])),'mailboxes':len(cfg.get('mailboxes',[])),'files':{},'configs':[],'roots':{},'hostname':platform.node()}
    dbs=sorted({d['name'] for d in cfg.get('databases',[])}|{site['database'] for site in cfg.get('sites',[]) if site.get('database')})
    if dbs:manifest['mysqlMajor']=run(['mysql','--protocol=socket','--user=root','--batch','--skip-column-names','-e','SELECT VERSION()']).decode().strip().split('.')[0]
    users=sorted({d['username'] for d in cfg.get('databases',[])}|{site['dbUser'] for site in cfg.get('sites',[]) if site.get('dbUser')})
    for site in cfg.get('sites',[]):
        root=site.get('root','')
        if not any(root.startswith(x+'/') for x in ['/home/litecp','/var/www/litecp']):raise ValueError('Unsupported website location: '+root)
    with tempfile.TemporaryDirectory(prefix='snapshot-',dir=BASE) as tmp:
        dump=pathlib.Path(tmp)/'databases.sql'
        if dbs:
            with open(dump,'wb') as f:
                subprocess.run(['mysqldump','--protocol=socket','--user=root','--single-transaction','--add-drop-database','--routines','--events','--triggers','--hex-blob','--databases',*dbs],stdout=f,stderr=subprocess.PIPE,check=True)
        else: dump.write_text('-- No managed databases\n')
        auth=[]
        for user in users:
            if not user.replace('_','').isalnum(): raise ValueError('Invalid database user')
            query="SHOW CREATE USER '%s'@'localhost'; SHOW GRANTS FOR '%s'@'localhost';"%(user,user)
            rows=run(['mysql','--protocol=socket','--user=root','--batch','--raw','--skip-column-names'],input=query.encode()).decode().splitlines()
            for row in rows:
                sql=row.split('\t')[-1]
                if sql.startswith('CREATE USER '):
                    auth.append(sql.replace('CREATE USER ','CREATE USER IF NOT EXISTS ',1)+';')
                    auth.append(sql.replace('CREATE USER ','ALTER USER ',1)+';')
                else:auth.append(sql+';')
        authfile=pathlib.Path(tmp)/'users.sql';authfile.write_text('\n'.join(auth))
        images=[]
        for site in cfg.get('sites',[]):
            app=site.get('application') or {}
            if app.get('container'):
                image=run(['podman','inspect','--format','{{.ImageName}}',app['container']]).decode().strip()
                if image not in images:images.append(image)
        imagefile=pathlib.Path(tmp)/'images.tar'
        if images:run(['podman','save','--multi-image-archive','--output',str(imagefile),*images])
        manifest['images']=images
        with zipfile.ZipFile(destination,'x',compression=zipfile.ZIP_DEFLATED,allowZip64=True) as z:
            for key,root in ROOTS.items():
                if os.path.exists(root):
                    st=os.stat(root);manifest['roots'][key]={'mode':stat.S_IMODE(st.st_mode),'uid':st.st_uid,'gid':st.st_gid}
                add_tree(z,key,root,manifest)
            for p in managed_configs():
                entry='configuration/'+p.lstrip('/');z.write(p,entry);s=os.stat(p)
                manifest['files'][entry]={'mode':stat.S_IMODE(s.st_mode),'uid':s.st_uid,'gid':s.st_gid,'size':s.st_size,'sha256':digest(p)};manifest['configs'].append(p)
            for name,p in [('databases.sql',dump),('users.sql',authfile)]:
                entry='mysql/'+name;z.write(p,entry);manifest['files'][entry]={'mode':0o600,'uid':0,'gid':0,'size':p.stat().st_size,'sha256':digest(p)}
            if images:
                z.write(imagefile,'runtime/images.tar');manifest['files']['runtime/images.tar']={'mode':0o600,'uid':0,'gid':0,'size':imagefile.stat().st_size,'sha256':digest(imagefile)}
            z.writestr('manifest.json',json.dumps(manifest))
    os.chmod(destination,0o600)
    return {'path':str(destination),'size':os.stat(destination).st_size,**{k:manifest[k] for k in ['createdAt','sites','databases','mailboxes']}}

def supported_admin_password_hash(encoded):
    # Match the formats and work-factor bounds accepted by the panel. Never
    # derive a password hash while inspecting an untrusted archive.
    import base64, binascii, re
    if not isinstance(encoded,str) or len(encoded)>256:return False
    legacy=re.fullmatch(r'pbkdf2-sha256\$([0-9]+)\$[a-fA-F0-9]{32}\$[a-fA-F0-9]{64}',encoded)
    if legacy:return 100000<=int(legacy[1])<=2000000
    argon=re.fullmatch(r'\$argon2id\$v=19\$m=([0-9]+),t=([0-9]+),p=([0-9]+)\$([A-Za-z0-9+/]+)\$([A-Za-z0-9+/]+)',encoded)
    if not argon:return False
    memory,iterations,parallelism=map(int,argon.group(1,2,3))
    if any(str(value)!=raw for value,raw in zip((memory,iterations,parallelism),argon.group(1,2,3))):return False
    if not (19456<=memory<=65536 and 2<=iterations<=5 and 1<=parallelism<=4):return False
    try:
        salt,key=(base64.b64decode(raw+'='*((-len(raw))%4),validate=True) for raw in argon.group(4,5))
    except (ValueError,binascii.Error):return False
    if not (16<=len(salt)<=32 and len(key)==32):return False
    return all(base64.b64encode(value).decode().rstrip('=')==raw for value,raw in zip((salt,key),argon.group(4,5)))

def inspect(source,stage=None):
    with zipfile.ZipFile(source) as z:
        if len(z.infolist())>2000000:raise ValueError('Too many archive entries')
        if z.getinfo('manifest.json').file_size>64*1024*1024:raise ValueError('Snapshot manifest too large')
        m=json.loads(z.read('manifest.json'))
        if m.get('format')!='litecp-system-snapshot' or m.get('schema')!=1:raise ValueError('Not a LiteCP system snapshot')
        osid,major=osinfo()
        if (m['os'],m['osMajor'],m['architecture'])!=(osid,major,platform.machine()):raise ValueError('Restore requires the same OS major version and architecture')
        seen=set();total=0;links=set();config_paths=set()
        allowed=set(m['files'])|{'manifest.json'}
        for f in z.infolist():
            name=f.filename.rstrip('/');safe_name(f.filename)
            if name in seen or name not in allowed:raise ValueError('Unexpected or duplicate archive entry')
            seen.add(name);total+=f.file_size
            if any(name.startswith(x+'/') for x in links):raise ValueError('Symlink ancestor forbidden')
            if total>shutil.disk_usage(BASE).free:raise ValueError('Not enough free disk space to inspect and stage snapshot')
            if name=='manifest.json':continue
            parts=pathlib.PurePosixPath(name).parts
            if parts[0]=='data':
                if len(parts)<3 or parts[1] not in ROOTS or excluded(parts[1],'/'.join(parts[2:])):raise ValueError('Invalid data location')
            elif parts[0]=='configuration':
                p='/'+name[len('configuration/'):]
                import fnmatch
                patterns=['/etc/nginx/conf.d/litecp-*.conf','/etc/nginx/sites-available/litecp-*.conf','/etc/cron.d/litecp-*','/etc/php/*/fpm/pool.d/litecp*.conf','/etc/opt/remi/php*/php-fpm.d/litecp*.conf','/etc/litecp/php73-pools/*','/etc/litecp/waf/*']
                if not (p in SERVICE_CONFIGS or any(fnmatch.fnmatch(p,x) for x in patterns)) or '..' in pathlib.PurePosixPath(p).parts:raise ValueError('Configuration destination forbidden')
                config_paths.add(p)
            elif name not in ['mysql/databases.sql','mysql/users.sql','runtime/images.tar']:raise ValueError('Invalid snapshot location')
            mode=f.external_attr>>16
            if stat.S_ISLNK(mode):
                links.add(name)
                if any(x.startswith(name+'/') for x in seen):raise ValueError('Symlink ancestor forbidden')
                target=z.read(f).decode();root=pathlib.PurePosixPath(ROOTS[parts[1]]);dest=root.joinpath(*parts[2:]);resolved=os.path.normpath(os.path.join(str(dest.parent),target))
                if os.path.commonpath([str(root),resolved])!=str(root):raise ValueError('Escaping symlink')
            elif not (f.is_dir() or stat.S_ISREG(mode) or stat.S_IFMT(mode)==0):raise ValueError('Special files forbidden')
            h=hashlib.sha256()
            output=None
            if stage:
                dest=pathlib.Path(stage)/name;dest.parent.mkdir(parents=True,exist_ok=True)
                if f.is_dir():dest.mkdir(exist_ok=True)
                else:output=open(dest,'xb')
            with z.open(f) as stream:
                for block in iter(lambda:stream.read(1024*1024),b''):
                    h.update(block)
                    if output:output.write(block)
            if output:output.close()
            if not f.is_dir() and h.hexdigest()!=m['files'][name]['sha256']:raise ValueError('Snapshot checksum mismatch')
        if seen!=allowed:raise ValueError('Snapshot is incomplete')
        if set(m['configs'])!=config_paths:raise ValueError('Configuration manifest mismatch')
        if 'data/panel/config.json' not in seen:raise ValueError('Panel configuration missing')
        panel=json.loads(z.read('data/panel/config.json'))
        password_hash=panel.get('admin',{}).get('passwordHash','')
        if not panel.get('admin',{}).get('username') or not supported_admin_password_hash(password_hash):raise ValueError('Snapshot administrator credentials are missing or invalid')
        return m

def validate_restore_accounts(stage):
    import pwd
    # Check destination package identities before replacing databases or data.
    # Preserving numeric source IDs can assign private files to another user.
    paths={'mail':'vmail','webmail':'litecp-webmail','mail-config/users':'dovecot','mail-config/webmail-master-users':'dovecot','mail-config/dkim':'opendkim'}
    for relative,owner in paths.items():
        if not (stage/'data'/relative).exists():continue
        try:pwd.getpwnam(owner)
        except KeyError:raise ValueError('Install required service account '+owner+' before restore (snapshot contains '+relative+')') from None

def mysql_managed_objects(cfg):
    dbs=sorted({d['name'] for d in cfg.get('databases',[])}|{site['database'] for site in cfg.get('sites',[]) if site.get('database')})
    users=sorted({d['username'] for d in cfg.get('databases',[])}|{site['dbUser'] for site in cfg.get('sites',[]) if site.get('dbUser')})
    for name in dbs:
        if not isinstance(name,str) or not name.replace('_','').isalnum() or name.lower() in ['mysql','sys','performance_schema','information_schema']:raise ValueError('Invalid managed database')
    for user in users:
        if not isinstance(user,str) or not user.replace('_','').isalnum() or user.lower()=='root':raise ValueError('Invalid managed database user')
    return dbs,users

def mysql_existing(kind,names):
    if not names:return []
    values=','.join('0x'+name.encode().hex() for name in names)
    query=('SELECT SCHEMA_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME IN ('+values+');') if kind=='databases' else ('SELECT User FROM mysql.user WHERE Host=0x6c6f63616c686f7374 AND User IN ('+values+');')
    rows=run(['mysql','--protocol=socket','--user=root','--batch','--raw','--skip-column-names'],input=query.encode()).decode().splitlines()
    if any(row not in names for row in rows):raise ValueError('Unexpected database inventory')
    return sorted(set(rows))

def preflight_mysql_replacement(source_cfg,destination_cfg):
    source_dbs,source_users=mysql_managed_objects(source_cfg);destination_dbs,destination_users=mysql_managed_objects(destination_cfg)
    for kind,source,destination in [('databases',source_dbs,destination_dbs),('users',source_users,destination_users)]:
        if mysql_existing(kind,sorted(set(source)-set(destination))):raise ValueError('Snapshot conflicts with unmanaged MySQL '+kind+'; retain/export and resolve the collision before restore')

def rollback_mysql(source_cfg,destination_cfg,recovery,rollback):
    source_dbs,source_users=mysql_managed_objects(source_cfg);destination_dbs,destination_users=mysql_managed_objects(destination_cfg)
    dbs=mysql_existing('databases',sorted(set(source_dbs+destination_dbs)));users=mysql_existing('users',sorted(set(source_users+destination_users)))
    # Keep failed imported SQL and grants before any rollback DROP. These are
    # exclusively the validated managed objects covered by whole-panel consent.
    if dbs:
        with open(rollback/'failed-restored-databases.sql','xb') as output:
            subprocess.run(['mysqldump','--protocol=socket','--user=root','--single-transaction','--add-drop-database','--routines','--events','--triggers','--hex-blob','--databases',*dbs],stdout=output,stderr=subprocess.PIPE,check=True)
    if users:
        grants=[]
        for user in users:
            query="SHOW CREATE USER '%s'@'localhost'; SHOW GRANTS FOR '%s'@'localhost';"%(user,user)
            rows=run(['mysql','--protocol=socket','--user=root','--batch','--raw','--skip-column-names'],input=query.encode()).decode().splitlines()
            if not rows:raise ValueError('Cannot retain failed database user grants')
            grants.extend(row.split('\t')[-1]+';' for row in rows)
        with open(rollback/'failed-restored-users.sql','x') as output:output.write('\n'.join(grants))
    for name in dbs:run(['mysql','--protocol=socket','--user=root'],input=('DROP DATABASE IF EXISTS `'+name+'`;').encode())
    for user in users:run(['mysql','--protocol=socket','--user=root'],input=("DROP USER IF EXISTS '%s'@'localhost';"%user).encode())
    with zipfile.ZipFile(recovery) as archive:
        for name in ['users.sql','databases.sql']:run(['mysql','--protocol=socket','--user=root'],input=archive.read('mysql/'+name))

def restore(source,confirmation):
    if confirmation!='RESTORE ENTIRE LITECP':raise ValueError('Explicit restore confirmation required')
    stage=pathlib.Path(tempfile.mkdtemp(prefix='restore-stage-',dir=BASE));m=inspect(source,stage)
    if m.get('testFixture'):raise ValueError('An inspection-only QA fixture cannot be restored')
    validate_restore_accounts(stage)
    destination_cfg=config()
    cfg=json.loads((stage/'data/panel/config.json').read_text())
    preflight_mysql_replacement(cfg,destination_cfg)
    if m.get('mysqlMajor') and run(['mysql','--protocol=socket','--user=root','--batch','--skip-column-names','-e','SELECT VERSION()']).decode().strip().split('.')[0]!=m['mysqlMajor']:raise ValueError('Install the same MySQL major version before restore')
    for site in cfg.get('sites',[]):
        root=site.get('root','')
        if os.path.normpath(root)!=root or not any(root.startswith(x+'/') for x in ['/home/litecp','/var/www/litecp']):raise ValueError('Unsupported restored website location')
    if shutil.disk_usage(BASE).free<os.path.getsize(source)*2:raise ValueError('Allow at least twice the ZIP size free for staging and recovery')
    for version in {s['phpVersion'] for s in cfg.get('sites',[])}:
        candidates=['/usr/bin/php'+version,'/opt/remi/php'+version.replace('.','')+'/root/usr/bin/php']
        if version!='7.3' and not any(os.path.exists(p) for p in candidates):raise ValueError('Install required PHP '+version+' before restore')
    for unit,needed in [('dovecot',bool(cfg.get('mailboxes'))),('litecp-ftps',(stage/'data/panel/ftp/installed').exists())]:
        if needed and subprocess.run(['systemctl','cat',unit],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode!=0:raise ValueError('Install required service '+unit+' before restore')
    stamp=datetime.datetime.now().strftime('%Y%m%d-%H%M%S-%f')
    recovery=BASE/('before-restore-'+stamp+'.zip')
    rollback=BASE/('recovery-'+stamp);rollback.mkdir(mode=0o700)
    stopped=[];moves=[];written=[];containers=[];completed=False;mysql_started=False
    try:
        for unit in ['litecp','nginx','crond','cron','postfix','dovecot','litecp-ftps']+php_units():
            if subprocess.run(['systemctl','is-active','--quiet',unit]).returncode==0:
                stopped.append(unit);run(['systemctl','stop',unit])
        pause_containers(containers)
        create(recovery)
        mysql_started=True
        # Do not replace MySQL's system schema or the destination root account.
        # Source managed users are replaced with the source grants/password hashes.
        for name in mysql_managed_objects(destination_cfg)[0]:
            run(['mysql','--protocol=socket','--user=root'],input=('DROP DATABASE IF EXISTS `'+name+'`;').encode())
        users=sorted(set(mysql_managed_objects(destination_cfg)[1]+mysql_managed_objects(cfg)[1]))
        for user in users:
            run(['mysql','--protocol=socket','--user=root'],input=("DROP USER IF EXISTS '%s'@'localhost';"%user).encode())
        if m.get('images'):run(['podman','load','--input',str(stage/'runtime/images.tar')])
        for name in ['users.sql','databases.sql']:
            with open(stage/'mysql'/name,'rb') as f:run(['mysql','--protocol=socket','--user=root'],stdin=f)
        # Existing data trees are moved to recovery, never deleted.
        for key,root in ROOTS.items():
            incoming=stage/'data'/key;target=pathlib.Path(root)
            if not incoming.exists():continue
            # Mount points cannot be renamed (EBUSY). Keep the filesystem in
            # place and retain each child separately, including on rollback.
            if key in ['panel','backups'] or os.path.ismount(target):
                target.mkdir(parents=True,exist_ok=True)
                for child in list(target.iterdir()):
                    if excluded(key,child.name):continue
                    save=rollback/key/child.name;save.parent.mkdir(parents=True,exist_ok=True);shutil.move(str(child),save);moves.append((save,child))
                for child in incoming.iterdir():
                    old=target/child.name
                    if old.exists() or old.is_symlink():
                        save=rollback/key/child.name;save.parent.mkdir(parents=True,exist_ok=True);shutil.move(str(old),save)
                    shutil.move(str(child),old);written.append(old)
            else:
                if target.exists():
                    shutil.move(str(target),rollback/key);moves.append((rollback/key,target))
                target.parent.mkdir(parents=True,exist_ok=True);shutil.move(str(incoming),target);written.append(target)
        for old in managed_configs():
            # An older source may not contain an optional destination service.
            # Keep its package configuration instead of leaving an enabled unit
            # without a config at the next boot. Site configs still replace.
            if old in SERVICE_CONFIGS and old not in m['configs']:continue
            oldpath=pathlib.Path(old);save=rollback/'old-configurations'/old.lstrip('/');save.parent.mkdir(parents=True,exist_ok=True);shutil.move(str(oldpath),save);moves.append((save,oldpath))
        for p in m['configs']:
            target=pathlib.Path(p);target.parent.mkdir(parents=True,exist_ok=True)
            if target.exists():
                save=rollback/'configuration'/p.lstrip('/');save.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(target,save)
            shutil.copy2(stage/'configuration'/p.lstrip('/'),target);written.append(target)
            if p == '/etc/dovecot/conf.d/99-litecp.conf':
                # The destination package may have assigned vmail a lower UID.
                target.write_text(remap_dovecot_vmail_uid(target.read_text(), int(run(['id','-u','vmail']))))
        # Restore file owners/modes; recreate only validated in-tree symlinks.
        # Read the central directory once: reopening it for every file makes
        # metadata restoration quadratic on large website snapshots.
        with zipfile.ZipFile(source) as z:
            archive_modes={item.filename.rstrip('/'):item.external_attr>>16 for item in z.infolist()}
        for name,meta in m['files'].items():
            parts=pathlib.PurePosixPath(name).parts
            if parts[0] in ['mysql','runtime']:continue
            target=pathlib.Path(ROOTS[parts[1]]).joinpath(*parts[2:]) if parts[0]=='data' else pathlib.Path('/'+name[len('configuration/'):])
            mode=archive_modes[name]
            if stat.S_ISLNK(mode):
                link=target.read_text();target.unlink();target.symlink_to(link)
            else:os.chmod(target,meta['mode'] & 0o777);os.chown(target,meta['uid'],meta['gid'])
        for key,meta in m.get('roots',{}).items():
            if key not in ROOTS:raise ValueError('Unknown root')
            if os.path.exists(ROOTS[key]):os.chmod(ROOTS[key],meta['mode'] & 0o777);os.chown(ROOTS[key],meta['uid'],meta['gid'])
        # Re-map service-owned trees to destination service IDs.
        owners=[]
        if 'mail' in ROOTS:owners.append((ROOTS['mail'],'vmail'))
        if 'webmail' in ROOTS:owners.append((ROOTS['webmail'],'litecp-webmail'))
        if 'mail-config' in ROOTS:owners.extend([(ROOTS['mail-config']+'/users','dovecot'),(ROOTS['mail-config']+'/webmail-master-users','dovecot'),(ROOTS['mail-config']+'/dkim','opendkim')])
        for root,owner in owners:
            if os.path.exists(root):run(['chown','-R',owner+':'+owner,root])
        # ZIP mode metadata does not retain the signer's traversal ACL.
        # Reapply only its existing package identity, never world-readable access.
        mail_config=ROOTS.get('mail-config')
        if mail_config and os.path.isdir(mail_config+'/dkim'):
            run(['setfacl','-m','u:opendkim:--x',str(pathlib.Path(mail_config).parent),mail_config])
        # ZIP metadata does not contain SELinux contexts. Staged files otherwise
        # retain the backup directory's label and services cannot read them.
        if shutil.which('selinuxenabled') and subprocess.run(['selinuxenabled'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:
            paths=[root for root in ROOTS.values() if os.path.exists(root)]
            paths.extend(p for p in m['configs'] if os.path.exists(p))
            if paths:run(['restorecon','-R',*paths])
        # Keep persistent container monitors outside the short-lived snapshot
        # service. The scope survives while workloads run and is collected later.
        # Older archives omit generated HTTP-context includes. Reconciliation
        # recreates them; retain/remove them with the same rollback bookkeeping.
        for generated in GENERATED_CONFIGS:
            if pathlib.Path(generated) not in written:written.append(pathlib.Path(generated))
        run(['systemd-run','--scope','--quiet','/usr/local/bin/litecp','snapshot-reconcile'])
        if cfg.get('mailboxes') or cfg.get('mailDomains'):
            run(['/usr/local/bin/litecp','sync-mail'])
            run(['doveconf','-n'])
        for unit in ['php83-php-fpm','php8.3-fpm']:
            subprocess.run(['systemctl','try-restart',unit],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
        if 'webmail' in ROOTS and (pathlib.Path(ROOTS['webmail'])/'roundcube.db').exists():
            import sqlite3
            with sqlite3.connect(str(pathlib.Path(ROOTS['webmail'])/'roundcube.db')) as db:db.execute('DELETE FROM session')
        run(['nginx','-t'])
        if cfg.get('settings',{}).get('emailHostingEnabled') and (cfg.get('mailboxes') or cfg.get('mailDomains')):
            mail_units=['postfix','dovecot']
            for optional in ['opendkim','rspamd']:
                if subprocess.run(['systemctl','cat',optional],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:mail_units.append(optional)
            for unit in mail_units:
                if unit not in stopped:stopped.append(unit)
        timezone=cfg.get('settings',{}).get('timezone','UTC')
        if '..' not in timezone and (pathlib.Path('/usr/share/zoneinfo')/timezone).is_file():run(['timedatectl','set-timezone',timezone])
        hostname=m.get('hostname','')
        if hostname and all(c.isalnum() or c in '.-' for c in hostname):run(['hostnamectl','set-hostname',hostname])
        completed=True
        return {'restored':True,'recovery':str(recovery),'message':'Restore complete. Sign in using the source administrator credentials.'}
    except Exception:
        # Preserve failed restored data separately, then put destination files back.
        failed=rollback/'failed-restored-data';failed.mkdir(exist_ok=True)
        for index,target in enumerate(reversed(written)):
            if target.exists() or target.is_symlink():shutil.move(str(target),failed/str(index))
        for saved,target in reversed(moves):
            if saved.exists() or saved.is_symlink():
                target.parent.mkdir(parents=True,exist_ok=True);shutil.move(str(saved),target)
        if recovery.exists() and mysql_started:rollback_mysql(cfg,destination_cfg,recovery,rollback)
        raise
    finally:
        failed_units=resume_containers(containers) if not completed else []
        for unit in reversed(stopped):
            if subprocess.run(['systemctl','start',unit],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode:failed_units.append(unit)
        if failed_units:
            message='Data operation finished but services need attention: '+', '.join(failed_units)
            original=sys.exc_info()[1]
            if original is not None:
                detail=str(original)
                if isinstance(original,subprocess.CalledProcessError):detail+='; '+(original.stderr or b'').decode(errors='replace')[:800]
                message+='; original operation error: '+detail
            raise RuntimeError(message)

def status(value):
    path=BASE/'status.json';temporary=BASE/'status.tmp';temporary.write_text(json.dumps(value));os.replace(temporary,path)

def main():
    os.umask(0o077);BASE.mkdir(parents=True,exist_ok=True,mode=0o700)
    import fcntl
    lock=open(BASE/'operation.lock','a');fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
    p=argparse.ArgumentParser();p.add_argument('action',choices=['create','inspect','restore']);p.add_argument('path');p.add_argument('--confirm',default='');a=p.parse_args()
    if os.geteuid()!=0:raise ValueError('Root required')
    path=pathlib.Path(a.path)
    if path.parent.resolve()!=BASE.resolve() or path.suffix!='.zip' or path.is_symlink():raise ValueError('Use a regular ZIP in '+str(BASE))
    if a.action!='inspect':status({'state':'running','action':a.action})
    if a.action=='create':
        # Pause application writers; keep the panel HTTPS proxy available.
        stopped=[];containers=[]
        try:
            for unit in creation_pause_units():
                if subprocess.run(['systemctl','is-active','--quiet',unit]).returncode==0:
                    stopped.append(unit);run(['systemctl','stop',unit])
            pause_containers(containers)
            result=create(path)
        finally:
            failed_units=resume_containers(containers)
            for unit in reversed(stopped):
                if subprocess.run(['systemctl','start',unit],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode:failed_units.append(unit)
            if failed_units:raise RuntimeError('Snapshot saved but services need attention: '+', '.join(failed_units))
    elif a.action=='inspect':
        m=inspect(path);result={k:m[k] for k in ['createdAt','sites','databases','mailboxes','os','osMajor','architecture']};result['size']=path.stat().st_size
    else:result=restore(path,a.confirm)
    if a.action!='inspect':status({'state':'complete','action':a.action,'result':result})
    print(json.dumps(result))
if __name__=='__main__':
    try:main()
    except Exception as e:
        if isinstance(e,subprocess.CalledProcessError):e=RuntimeError('Server command failed: '+str(e.cmd[0])+'; '+(e.stderr or b'').decode(errors='replace')[:800])
        if len(sys.argv)>1 and sys.argv[1]!='inspect':status({'state':'failed','error':str(e),'message':'Recovery files were retained. Check the journal for litecp-system-snapshot.'})
        print(json.dumps({'error':str(e)}));sys.exit(1)
