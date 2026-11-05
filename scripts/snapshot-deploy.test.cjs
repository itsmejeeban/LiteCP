const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs');
test('published updater installs the snapshot helper and retains rollback copy',()=>{
 const script=fs.readFileSync('scripts/deploy-published-update.sh','utf8');
 assert.match(script,/install -m 0700 "\$work\/scripts\/system-snapshot.py" \/usr\/local\/lib\/litecp\/system-snapshot.py/);
 assert.match(script,/cmp "\$work\/scripts\/system-snapshot.py"/);
 assert.match(script,/previous-system-snapshot.py/);
 assert.ok(script.indexOf('previous-system-snapshot.py')<script.indexOf("echo '[Server 1/4]"));
});
