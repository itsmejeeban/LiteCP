const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
test('failed firewall inspection offers read-only retry instead of applying an unverified profile',async()=>{
 const source=fs.readFileSync('web/firewall-profile.js','utf8'),start=source.indexOf('  function refresh()'),end=source.indexOf('  const navigate=',start);
 const button={},panel={},calls=[];let fail=true;
 const context={button,panel,show:()=>{button.disabled=false;},cp:{state:()=>({}),api:async(path,options)=>{calls.push({path,options});if(fail)throw Error('service unavailable');return {enabled:false,rules:[]};}},window:{LiteCPDialogs:{confirm:()=>{throw Error('Retry must not change rules');}}}};
 vm.runInNewContext('let busy=false,verified=false,enabled=false,inspection;'+source.slice(start,end)+';this.refresh=refresh;',context);
 assert.equal(await context.refresh(),false);assert.equal(button.disabled,false);assert.equal(button.textContent,'Retry firewall check');
 fail=false;await button.onclick();assert.equal(calls.length,2);assert.ok(calls.every(call=>call.options===undefined));
});

test('firewall status loads only on authenticated navigation and coalesces open requests', async () => {
 const source = fs.readFileSync('web/firewall-profile.js', 'utf8');
 const element = () => ({
  append(){},after(){},insertBefore(){},replaceChildren(){},
  closest:() => ({classList:{contains:() => false}})
 });
 const button = element(), panel = element();
 const routes = [], calls = [];
 let authenticated, reply;
 const features = {navigate:page => routes.push(page)};
 vm.runInNewContext(source, {
  document:{querySelector:element,getElementById:element,createElement:tag => tag==='button'?button:tag==='article'?panel:element()},
  location:{hash:'#firewall'},
  window:{LiteCPFeatures:features,LiteCP:{
   state:()=>authenticated,
   api:path=>{
    calls.push(path);
    return new Promise(resolve => { reply=resolve; });
   }
  }}
 });
 assert.equal(calls.length, 0);
 features.navigate('firewall');
 assert.equal(calls.length, 0);
 authenticated = {};
 features.navigate('dashboard');
 assert.equal(calls.length, 0);
 features.navigate('firewall');
 features.navigate('firewall');
 assert.deepEqual(routes, ['firewall','dashboard','firewall','firewall']);
 assert.deepEqual(calls, ['/api/firewall/apply-litecp']);
 assert.equal(button.disabled, true);
 reply({enabled:true,rules:[]});
 await new Promise(setImmediate);
 assert.equal(button.textContent, 'Disable LiteCP Firewall');
 assert.equal(button.disabled, false);
 features.navigate('dashboard');
 features.navigate('firewall');
 assert.equal(calls.length, 2);
 reply({enabled:false,rules:[]});
 await new Promise(setImmediate);
 assert.equal(button.textContent, 'Apply LiteCP Firewall');
});

test('required rule editor saves the selected rule and keeps the dialog open on error', async () => {
 const source = fs.readFileSync('web/firewall-profile.js', 'utf8');
 const start = source.indexOf('  function editRule(');
 const end = source.indexOf('  function refresh()', start);
 const port = {focus() {}}, protocol = {}, remarks = {}, addresses = {}, message = {}, cancel = {}, scope = {}, addressLabel = {};
 const controls = [port, protocol, remarks, addresses, cancel];
 const form = {elements:{port, protocol, remarks, addresses}, querySelectorAll:() => controls, reportValidity:() => true};
 let closed = false, fail = true, updated;
 const dialog = {
  isConnected:true, close(){closed = true;}, showModal(){}, setAttribute(){}, addEventListener(){},
  querySelector(selector) {return {'form':form, '[data-scope]':scope, '[data-cancel]':cancel, '[data-error]':message, '[data-address-label]':addressLabel}[selector];}
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
 context.editRule({port:'8444/tcp', direction:'inbound', zone:'public', address:'192.0.2.1', remarks:'Webmail'});
 assert.equal(port.value, '8444');
 assert.equal(protocol.value, 'tcp');
 assert.equal(addressLabel.textContent, 'Source IP / CIDR');
 assert.equal(addresses.value, '192.0.2.1');
 port.value = '9443';
 addresses.value = '192.0.2.2, 2001:db8::/64';
 await form.onsubmit({preventDefault(){}});
 assert.equal(message.hidden, false);
 assert.match(message.textContent, /Previous rule restored/);
 assert.equal(closed, false);
 assert.ok(controls.every(control => !control.disabled));
 fail = false;
 await form.onsubmit({preventDefault(){}});
 assert.deepEqual(JSON.parse(JSON.stringify(requests.at(-1))), {
  path:'/api/firewall/apply-litecp', method:'PUT',
  body:{port:'8444/tcp', direction:'inbound', zone:'public', address:'192.0.2.1', newPort:'9443/tcp', addresses:'192.0.2.2, 2001:db8::/64', remarks:'Webmail'}
 });
 assert.equal(closed, true);
 assert.deepEqual(updated, {rules:[]});
});
