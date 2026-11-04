const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

test('SELinux controls escape logs and only offer enablement when admitted',()=>{
 const source=fs.readFileSync('web/security-dashboard.js','utf8');
 const start=source.indexOf(' function repairHTML('),end=source.indexOf(' function render()',start);
 const context={esc:value=>String(value).replaceAll('<','&lt;')};
 vm.runInNewContext(source.slice(start,end)+';this.html=repairHTML;',context);
 assert.match(context.html({mode:'Permissive',canEnable:true}),/data-selinux-enable/);
 assert.doesNotMatch(context.html({mode:'Disabled',canEnable:false}),/data-selinux-enable/);
 const running=context.html({mode:'Enforcing',state:'running',log:'<script>attack</script>'});
 assert.match(running,/<progress/);assert.doesNotMatch(running,/<script>/);
 assert.match(context.html({mode:'Disabled'}),/will not reboot/);
 const cooldown=context.html({mode:'Permissive',state:'failed',canEnable:false,retryAt:new Date(Date.now()+60000).toISOString()});
 assert.match(cooldown,/button[^>]*disabled/);
 assert.match(cooldown,/Retry available in/);
 assert.match(cooldown,/automatically/);
 assert.doesNotMatch(cooldown,/data-selinux-enable/);
});

test('SELinux enablement requires confirmation and submits only a fixed request',async()=>{
 const source=fs.readFileSync('web/security-dashboard.js','utf8');
 const start=source.indexOf(' async function enableSELinux()'),end=source.indexOf(' let pending=',start);
 let allowed=false,calls=0,rendered=0,scheduled=0;
 const button={disabled:false};
 const context={window:{LiteCPDialogs:{confirm:async()=>allowed}},page:{querySelector:()=>button},cp:{api:async(path,options)=>{calls++;assert.equal(path,'/api/security/selinux');assert.equal(options.method,'POST');assert.equal(options.body.confirm,true);return {state:'running'};}},render:()=>rendered++,scheduleRepair:()=>scheduled++};
 vm.runInNewContext(source.slice(start,end)+';this.enable=enableSELinux;',context);
 await context.enable();assert.equal(calls,0);
 allowed=true;await context.enable();assert.equal(calls,1);assert.equal(rendered,1);assert.equal(scheduled,1);assert.equal(button.disabled,true);
});

test('failed refresh clears previous security observations and allows retry', async () => {
  const source = fs.readFileSync('web/security-dashboard.js', 'utf8');
  const start = source.indexOf(' let pending=null,generation=0;');
  const end = source.indexOf(" page.querySelector('button').onclick", start);
  const status = {}, button = {}, checks = { stale: true, replaceChildren() { this.stale = false; } };
  let reject;
  const context = {
    page: { querySelector: selector => selector === 'button' ? button : selector === '[data-security-status]' ? status : checks },
    cp: { api: () => new Promise((_, fail) => { reject = fail; }) }
  };
  vm.runInNewContext(source.slice(start, end) + ';this.refresh=load;', context);
  const request = context.refresh();
  assert.equal(checks.stale, false);
  assert.equal(button.disabled, true);
  reject(new Error('Connection unavailable'));
  await request;
  assert.equal(button.disabled, false);
  assert.match(status.textContent, /Unable to check security status.*Connection unavailable/);
});
