const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs');
test('installer and guarded repair label both LiteCP HTTP listeners',()=>{
 for(const file of ['scripts/install.sh','scripts/enable-selinux.sh']){
  const source=fs.readFileSync(file,'utf8');
  assert.match(source,/for port in 4321 9091; do/);
  assert.match(source,/semanage port -a -t http_port_t -p tcp "\$port"/);
  assert.match(source,/semanage port -m -t http_port_t -p tcp "\$port"/);
 }
 const script=fs.readFileSync('scripts/enable-selinux.sh','utf8');
 assert.ok(script.indexOf('for port in 4321 9091')<script.indexOf('setenforce 1'));
 assert.match(script,/nginx -t -e stderr/);
 assert.match(script,/Nginx reload failed under SELinux enforcement/);
 assert.match(script,/Previously active service \$unit failed verification/);
 assert.match(script,/--on-active=5m/);
 assert.match(script,/trap rollback EXIT/);
});
