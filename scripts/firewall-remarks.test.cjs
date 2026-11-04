const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
test('custom remarks are escaped and both directions have distinct badges',()=>{
 const source=fs.readFileSync('web/features.js','utf8');
 const start=source.indexOf('    const rows=(state.firewallRules||[])'),end=source.indexOf('    $("#mailHostingToggle")',start);
 const host={};const context={state:{firewallRules:[{id:'test',direction:'inbound',port:65001,protocol:'tcp',source:'any',remarks:'<script>bad</script>'},{id:'out',direction:'outbound',port:65002,protocol:'udp',destination:'any',remarks:'NTP test'}]},esc:value=>String(value??'').replaceAll('<','&lt;'),$:()=>host};
 vm.runInNewContext(source.slice(start,end),context);
 assert.match(host.innerHTML,/&lt;script>/);assert.doesNotMatch(host.innerHTML,/<script>/);
 assert.match(host.innerHTML,/firewall-direction-inbound/);assert.match(host.innerHTML,/firewall-direction-outbound/);assert.match(host.innerHTML,/<th>REMARKS<\/th>/);
 assert.match(source,/remarks: data.get\("remarks"\)/);
 assert.match(fs.readFileSync('web/index.html','utf8'),/textarea name="remarks" maxlength="500"/);
 const profile=fs.readFileSync('web/firewall-profile.js','utf8');assert.match(profile,/<th>Remarks<\/th>/);assert.match(profile,/rule\.remarks/);assert.match(profile,/cell.textContent=text/);
});
