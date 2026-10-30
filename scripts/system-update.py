#!/usr/bin/env python3
"""Package-repository updates only; no application/database upgrades or reboot."""
import datetime, fcntl, json, os, pathlib, re, shutil, subprocess, sys
ROOT = pathlib.Path('/var/lib/litecp/system-updates')
ENV = dict(os.environ, LC_ALL='C', LANG='C', DEBIAN_FRONTEND='noninteractive', NEEDRESTART_MODE='l')
def save(path, value):
    temporary = path.with_name(path.name + '.new')
    temporary.write_text(json.dumps(value), encoding='utf-8'); os.chmod(temporary, 0o600); os.replace(temporary, path)
def run(args, timeout=1800):
    result = subprocess.run(args, env=ENV, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout)
    with (ROOT / 'operations.log').open('a', encoding='utf-8') as log:
        log.write(' '.join(args[:3]) + '\n' + result.stdout + result.stderr + '\n')
    if result.returncode: raise RuntimeError('Package manager command failed. Review /var/lib/litecp/system-updates/operations.log.')
    return result.stdout

def manager():
    if shutil.which('dnf'): return 'dnf'
    if shutil.which('apt-get'): return 'apt'
    raise RuntimeError('Ubuntu or AlmaLinux package manager required.')

def installed(kind):
    if kind == 'dnf':
        rows=run(['rpm','-qa','--qf','%{NAME}.%{ARCH}\t%{EPOCHNUM}:%{VERSION}-%{RELEASE}\n']).splitlines()
        return dict(row.split('\t',1) for row in rows if '\t' in row)
    rows=run(['dpkg-query','-W','-f=${db:Status-Abbrev}\t${binary:Package}\t${Version}\n']).splitlines()
    return {parts[1]:parts[2] for row in rows if len(parts:=row.split('\t'))==3 and parts[0].startswith('ii')}

def category(name):
    return 'Panel service' if re.match(r'^(nginx|php|mysql|mariadb|postfix|dovecot|opendkim|rspamd|clamav|clamd|pure-ftpd|podman|fail2ban|firewalld|ufw|certbot|openssh|openssl|python|ruby|node|cronie|cron)([-.:0-9]|$)',name) else 'System package'

def inventory(refresh=True):
    kind=manager()
    if refresh: run(['dnf','-q','makecache','--refresh'] if kind=='dnf' else ['apt-get','update'])
    current=installed(kind); candidates={}
    if kind=='dnf':
        data=run(['dnf','-q','repoquery','--upgrades','--latest-limit=1','--qf','%{name}.%{arch}\t%{epoch}:%{version}-%{release}'])
        candidates=dict(row.split('\t',1) for row in data.splitlines() if '\t' in row)
    else:
        names=list(current)
        for offset in range(0,len(names),100):
            data=run(['apt-cache','policy']+names[offset:offset+100]); name=None
            for line in data.splitlines():
                if line and not line[0].isspace() and line.endswith(':'): name=line[:-1]
                elif name in current and line.strip().startswith('Candidate:'):
                    candidate=line.split(':',1)[1].strip()
                    if candidate!='(none)' and subprocess.run(['dpkg','--compare-versions',candidate,'gt',current[name]],env=ENV).returncode==0: candidates[name]=candidate
    source_waf=pathlib.Path('/etc/ld.so.conf.d/litecp-modsecurity.conf').exists()
    items=[dict(name=name,installed=value,latest=candidates.get(name,value),updateAvailable=name in candidates,blocked=source_waf and name.startswith('nginx'),category=category(name)) for name,value in sorted(current.items())]
    result=dict(manager=kind,checkedAt=datetime.datetime.now(datetime.timezone.utc).isoformat(),items=items)
    save(ROOT/'inventory.json',result); return result

def update(request):
    data=inventory(); available={item['name'] for item in data['items'] if item['updateAvailable'] and not item.get('blocked')}
    packages=request.get('packages',[])
    if packages:
        if len(packages)>512 or any(not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9+_.:-]*',p) or p not in available for p in packages): raise RuntimeError('One or more selected packages no longer have an available update. Check updates again.')
        packages=list(dict.fromkeys(packages))
    if not available: return
    kind=data['manager']
    if kind=='apt':
        run(['apt-get','-y','--no-remove','-o','Dpkg::Options::=--force-confold']+(['--only-upgrade','install']+packages if packages else ['upgrade']),3600)
    else:
        # No allowerasing, autoremove, distribution upgrade, or repository changes.
        protected=['--exclude=nginx*'] if any(item.get('blocked') for item in data['items']) else []
        run(['dnf','-y','--setopt=install_weak_deps=False','--setopt=obsoletes=False']+protected+['upgrade']+packages,3600)
    inventory(False)

def schedule(request):
    frequency=request.get('frequency','disabled'); clock=request.get('time','03:00')
    if frequency not in ('disabled','daily','weekly') or not re.fullmatch(r'(?:[01][0-9]|2[0-3]):[0-5][0-9]',clock): raise RuntimeError('Invalid automatic update schedule.')
    if frequency=='disabled':
        if pathlib.Path('/etc/systemd/system/litecp-system-update.timer').exists(): run(['systemctl','disable','--now','litecp-system-update.timer'])
        save(ROOT/'schedule.json',dict(frequency=frequency,time=clock))
        return
    service=pathlib.Path('/etc/systemd/system/litecp-system-update.service'); timer=service.with_suffix('.timer')
    for target in (service,timer):
        if target.exists(): shutil.copy2(target,ROOT/(target.name+'.'+datetime.datetime.now().strftime('%Y%m%d%H%M%S%f')+'.previous'))
    service.write_text('[Unit]\nDescription=LiteCP automatic system package updates\nWants=network-online.target\nAfter=network-online.target\n[Service]\nType=oneshot\nUMask=0077\nTimeoutStartSec=7200\nExecStart=/usr/bin/python3 '+str(ROOT/'system-update.py')+' auto\n',encoding='utf-8')
    calendar=('*-*-* ' if frequency=='daily' else 'Sun *-*-* ')+clock+':00'
    timer.write_text('[Unit]\nDescription=LiteCP system update schedule\n[Timer]\nOnCalendar='+calendar+'\nPersistent=true\nRandomizedDelaySec=300\n[Install]\nWantedBy=timers.target\n',encoding='utf-8')
    run(['systemctl','daemon-reload']);run(['systemctl','enable','--now','litecp-system-update.timer']);run(['systemctl','restart','litecp-system-update.timer'])
    save(ROOT/'schedule.json',dict(frequency=frequency,time=clock))

def main():
    if os.geteuid()!=0: raise RuntimeError('Root privileges required.')
    ROOT.mkdir(mode=0o700,parents=True,exist_ok=True)
    with (ROOT/'worker.lock').open('a') as lock:
        try: fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
        except BlockingIOError: raise RuntimeError('Another system update operation is running.')
        request=dict(action='update') if sys.argv[1]=='auto' else json.loads(pathlib.Path(sys.argv[1]).read_text(encoding='utf-8'))
        action=request.get('action')
        save(ROOT/'status.json',dict(state='running',action=action,message='Checking repositories…' if action=='check' else 'Applying '+str(action)+' operation…'))
        try:
            if action=='check': inventory()
            elif action=='update': update(request)
            elif action=='schedule': schedule(request)
            else: raise RuntimeError('Unsupported operation.')
            save(ROOT/'status.json',dict(state='complete',action=action,message='Operation completed. No automatic reboot was requested.',rebootRequired=pathlib.Path('/var/run/reboot-required').exists()))
        except Exception as error:
            save(ROOT/'status.json',dict(state='failed',action=action,message=str(error)));raise
if __name__=='__main__': main()
