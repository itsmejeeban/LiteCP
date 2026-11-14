const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
test('failed firewall inspection offers read-only retry instead of applying an unverified profile',async()=>{
 const source=fs.readFileSync('web/firewall-profile.js','utf8'),start=source.indexOf('  async function refresh()'),end=source.indexOf('  window.addEventListener',start);
 const button={},panel={},calls=[];let fail=true;
 const context={button,panel,show:()=>{button.disabled=false;},cp:{api:async(path,options)=>{calls.push({path,options});if(fail)throw Error('service unavailable');return {enabled:false,rules:[]};}},window:{LiteCPDialogs:{confirm:()=>{throw Error('Retry must not change rules');}}}};
 vm.runInNewContext('let busy=false,verified=false,enabled=false;'+source.slice(start,end)+';this.refresh=refresh;',context);
 assert.equal(await context.refresh(),false);assert.equal(button.disabled,false);assert.equal(button.textContent,'Retry firewall check');
 fail=false;await button.onclick();assert.equal(calls.length,2);assert.ok(calls.every(call=>call.options===undefined));
});

test('required rule editor saves the selected rule and keeps the dialog open on error', async () => {
 const source = fs.readFileSync('web/firewall-profile.js', 'utf8');
 const start = source.indexOf('  function editRule(');
 const end = source.indexOf('  async function refresh()', start);
 const port = {focus() {}}, protocol = {}, remarks = {}, message = {}, cancel = {}, scope = {};
 const controls = [port, protocol, remarks, cancel];
 const form = {elements:{port, protocol, remarks}, querySelectorAll:() => controls, reportValidity:() => true};
 let closed = false, fail = true, updated;
 const dialog = {
  isConnected:true, close(){closed = true;}, showModal(){}, setAttribute(){}, addEventListener(){},
  querySelector(selector) {return {'form':form, '[data-scope]':scope, '[data-cancel]':cancel, '[data-error]':message}[selector];}
 };
 const requests = [];
 const context = {
  document:{createElement:() => dialog, body:{append(){}}},
  window:{LiteCPDialogs:{confirm:async () => true}},
  cp:{api:async (path, options) => {
   requests.push({path, ...options});
   if (fail) throw Error('Previous rule restored');
   return {rules:[]};
  }, showToast(){}},
  show:state => {updated = state;}
 };
 vm.runInNewContext('let busy=false;' + source.slice(start, end) + ';this.editRule=editRule;', context);
 context.editRule({port:'8444/tcp', direction:'inbound', zone:'public', remarks:'Webmail'});
 assert.equal(port.value, '8444');
 assert.equal(protocol.value, 'tcp');
 port.value = '9443';
 await form.onsubmit({preventDefault(){}});
 assert.equal(message.hidden, false);
 assert.match(message.textContent, /Previous rule restored/);
 assert.equal(closed, false);
 assert.ok(controls.every(control => !control.disabled));
 fail = false;
 await form.onsubmit({preventDefault(){}});
 assert.deepEqual(JSON.parse(JSON.stringify(requests.at(-1))), {
  path:'/api/firewall/apply-litecp', method:'PUT',
  body:{port:'8444/tcp', direction:'inbound', zone:'public', newPort:'9443/tcp', remarks:'Webmail'}
 });
 assert.equal(closed, true);
 assert.deepEqual(updated, {rules:[]});
});
