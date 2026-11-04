const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs');
const read=path=>fs.readFileSync(path,'utf8');
test('network service is unprivileged and cannot inherit Linux capabilities',()=>{
 const unit=read('deploy/litecp.service');
 for(const line of ['User=litecp-web','Group=litecp-web','ExecStart=/usr/local/bin/litecp web','CapabilityBoundingSet=','AmbientCapabilities=','NoNewPrivileges=true','ProtectSystem=strict'])assert.ok(unit.split(/\r?\n/).includes(line),line);
 assert.doesNotMatch(unit,/^User=root$/m);
 assert.doesNotMatch(unit,/^(?:ReadWritePaths|BindPaths)=/m);
 assert.match(unit,/InaccessiblePaths=.*\/var\/lib\/litecp/);
});
test('broker socket admits only root and the dedicated service group',()=>{
 const socket=read('deploy/litecp-broker.socket');
 for(const line of ['SocketUser=root','SocketGroup=litecp-web','SocketMode=0660','ListenStream=/run/litecp-broker/control.sock'])assert.ok(socket.includes(line));
 const unit=read('deploy/litecp-broker.service');
 assert.match(unit,/ExecStart=\/usr\/local\/bin\/litecp broker/);
 assert.doesNotMatch(unit,/LITECP_BIND/);
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
