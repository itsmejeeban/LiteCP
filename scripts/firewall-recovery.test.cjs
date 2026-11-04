const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
test('failed firewall inspection offers read-only retry instead of applying an unverified profile',async()=>{
 const source=fs.readFileSync('web/firewall-profile.js','utf8'),start=source.indexOf('  async function refresh()'),end=source.indexOf('  window.addEventListener',start);
 const button={},panel={},calls=[];let fail=true;
 const context={button,panel,show:()=>{button.disabled=false;},cp:{api:async(path,options)=>{calls.push({path,options});if(fail)throw Error('service unavailable');return {enabled:false,rules:[]};}},window:{LiteCPDialogs:{confirm:()=>{throw Error('Retry must not change rules');}}}};
 vm.runInNewContext('let busy=false,verified=false,enabled=false;'+source.slice(start,end)+';this.refresh=refresh;',context);
 assert.equal(await context.refresh(),false);assert.equal(button.disabled,false);assert.equal(button.textContent,'Retry firewall check');
 fail=false;await button.onclick();assert.equal(calls.length,2);assert.ok(calls.every(call=>call.options===undefined));
});
