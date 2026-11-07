const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs');
const read=path=>fs.readFileSync(path,'utf8');
test('network service is unprivileged and cannot inherit Linux capabilities',()=>{
 const unit=read('deploy/litecp.service');
 for(const line of ['User=litecp-web','Group=litecp-web','ExecStart=/usr/local/bin/litecp web','CapabilityBoundingSet=','AmbientCapabilities=','NoNewPrivileges=true','ProtectSystem=strict'])assert.ok(unit.split(/\r?\n/).includes(line),line);
 assert.doesNotMatch(unit,/^User=root$/m);
 assert.doesNotMatch(unit,/^(?:ReadWritePaths|BindPaths)=/m);
 assert.match(unit,/InaccessiblePaths=.*\/var\/lib\/litecp/);
 assert.doesNotMatch(unit,/^InaccessiblePaths=.*\/etc\/shadow/m);
});
test('updates runtime-check the namespace before stopping the current panel',()=>{
 for(const [path,marker] of [['scripts/deploy-published-update.sh',"echo '[Server 3/4]"],['scripts/update-panel.sh','status running "Restarting LiteCP']]){
  const script=read(path),body=script.slice(script.indexOf(marker));
  assert.ok(body.indexOf('--prepare-only')>=0&&body.indexOf('--prepare-only')<body.indexOf('systemctl stop litecp'),path);
 }
 const checker=read('scripts/check-web-sandbox.sh');
 assert.doesNotMatch(checker,/'--pipe'/,'background probes must not inherit journald descriptors');
 for(const signal of ['systemd-run','/etc/shadow','/var/lib/litecp/secret.key','CapEff','NoNewPrivs'])assert.ok(checker.includes(signal),signal);
});
test('broker socket admits only root and the dedicated service group',()=>{
 const socket=read('deploy/litecp-broker.socket');
 for(const line of ['SocketUser=root','SocketGroup=litecp-web','SocketMode=0660','ListenStream=/run/litecp-broker/control.sock'])assert.ok(socket.includes(line));
 const unit=read('deploy/litecp-broker.service');
 assert.match(unit,/ExecStart=\/usr\/local\/bin\/litecp broker/);
 assert.doesNotMatch(unit,/LITECP_BIND/);
});
test('fresh install prepares DNS storage before the broker can start',()=>{
 const installer=read('scripts/install-privilege-services.sh');
 const preparation=installer.indexOf('install -d -m 0755 -o root -g root /etc/litecp/dns');
 assert.ok(preparation>=0 && preparation<installer.indexOf('[[ $prepare_only == 0 ]] || exit 0'));
 assert.match(read('deploy/litecp-broker.service'),/^ReadWritePaths=\/etc\/litecp\/dns\r?$/m);
});
test('all installation and update entry points install the split and preserve rollback',()=>{
 for(const path of ['scripts/install.sh','scripts/deploy-published-update.sh','scripts/update-panel.sh'])assert.ok(read(path).includes('scripts/install-privilege-services.sh'),path);
 for(const path of ['scripts/deploy-published-update.sh','scripts/update-panel.sh'])assert.ok(read(path).includes('--restore "$work/service-recovery"'),path);
 const installer=read('scripts/install-privilege-services.sh');
 assert.ok(installer.includes('--no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin'));
 assert.doesNotMatch(installer,/NOPASSWD|sudoers|chmod\s+777|chown\s+-R/);
 assert.match(read('scripts/install.sh'),/check-privilege-services\.sh/);
 const check=read('scripts/check-privilege-services.sh');
 for(const signal of ['MainPID','Uid:','CapEff:','NoNewPrivs:','/api/version'])assert.ok(check.includes(signal),signal);
});
test('new API routes require an explicit broker policy entry and root terminal is excluded',()=>{
 const policy=JSON.parse(read('deploy/broker-operations.json'));
 const routes=[...read('main.go').matchAll(/mux\.HandleFunc\("([^"]+)"/g)].map(match=>match[1]).filter(path=>!path.startsWith('/api/terminal'));
 assert.deepEqual([...policy].sort(),[...new Set(routes)].sort());
 assert.ok(!policy.some(path=>path==='/'||path.startsWith('/api/terminal')));
});

test('broker and PHP compatibility runtime keep their shared socket directory through boot ordering',()=>{
 const broker=read('deploy/litecp-broker.service'),php=read('deploy/litecp-php73-fpm.service');
 assert.match(broker,/^RuntimeDirectory=litecp litecp-php73$/m);
 for(const unit of [broker,php])assert.match(unit,/^RuntimeDirectoryPreserve=yes$/m);
});

test('application restart waits for database sockets and the quota mount in every split install',()=>{
 const installer=read('scripts/install-privilege-services.sh'),order=read('scripts/install-application-boot-order.sh');
 assert.ok(installer.includes('scripts/install-application-boot-order.sh'));
 assert.ok(order.includes('After=mysqld.service mysql.service mariadb.service'));
 assert.ok(order.includes('RequiresMountsFor=/home/litecp'));
 assert.ok(order.includes('cp -p "$path" "$backup/previous.conf"'));
});
