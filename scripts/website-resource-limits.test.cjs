const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
const script=fs.readFileSync(path.join(__dirname,'../web/website-resource-limits.js'),'utf8');
const formDefaults=Object.fromEntries(Array.from(script.matchAll(/<input name="(\w+)" type="number"[^>]* value="([\d.]+)"/g),([,name,value])=>[name,value]));
function panel(hash='',options={} ){
 class Element{
  constructor(){this.children=new Map();this.attributes=new Map();this.dataset={};this.value='qa';this.hidden=false;this.disabled=false;this.classes=new Set();this.classList={add:(name)=>this.classes.add(name)};this.options=[{value:'qa'}];this.elements=Object.fromEntries(['cpuPercent','memoryMiB','diskMiB','tasks','concurrentRequests','readMiBPerSecond','writeMiBPerSecond'].map(k=>[k,{value:'64'}]));this.elements.enabled={checked:false,disabled:false};}
  querySelector(k){if(!this.children.has(k))this.children.set(k,new Element());return this.children.get(k)}
  querySelectorAll(){return [...this.children.values()].flatMap(x=>[x,...x.children.values()])}
  addEventListener(type, listener){this["on"+type]=listener;}
  setAttribute(name,value){this.attributes.set(name,String(value));}
  removeAttribute(name){this.attributes.delete(name);}
  getAttribute(name){return this.attributes.get(name)??null;}
  append(){}insertAdjacentHTML(){}reset(){for(const [name,value] of Object.entries(formDefaults))this.elements[name].value=value;}
 }
 const page=new Element(),root=new Element();let rows=options.rows||[],mutations=[];
 for(const [selector,value] of [['[data-sort]',''],['[data-direction]','1'],['[data-page-size]','10']]) page.querySelector(selector).value=value;
 const settings={enabled:!!options.enabled,cpuPercent:25,memoryMiB:256,diskMiB:1024,tasks:64,concurrentRequests:8};
 const state={sites:options.sites||[{id:'qa',domain:'qa.example.test'}]};
 const api=async(url,request)=>{
  if(request){
   mutations.push({...request,url});
   const id=new URL(url,'https://panel.example.test').searchParams.get('siteId');
   if(id===options.failSite&&request.method==='PUT')throw new Error('Runtime unavailable');
   if(options.manageRows){
    if(request.method!=='POST')rows=rows.filter(row=>row.id!==id);
    if(request.method==='PUT')rows.push({...state.sites.find(site=>site.id===id),settings:request.body,usage:{}});
   }else rows=[{id:'qa',domain:'qa.example.test',settings,usage:{}}];
   return {state:options.resultState||'Active',enforced:options.resultEnforced??true};
  }
  if(url.includes('?')){
   const id=new URL(url,'https://panel.example.test').searchParams.get('siteId');
   const row=options.manageRows?rows.find(row=>row.id===id):null;
   return {settings:options.manageRows?row?.settings:options.newSite?null:settings,state:row?.state||(options.enabled?'Active':'Disabled'),enforced:row?.enforced,capabilities:{ready:true,prerequisites:[]}};
  }
  return {items:rows,updatedAt:new Date().toISOString()};
 };
 vm.runInNewContext(script,{document:{createElement:()=>page,querySelector:()=>root,hidden:true,addEventListener(){}},location:{hash},URL,window:{LiteCPDialogs:{confirm:async()=>options.confirm??true},LiteCP:{escapeHTML:String,state:()=>state,api,formatBytes:String},LiteCPFeatures:{render(){}},addEventListener(){}},setTimeout(){},clearTimeout(){}});
 return {page,mutations,open:async()=>{page.querySelector('[data-picker]').onsubmit({preventDefault(){}});await new Promise(setImmediate)},save:async()=>{page.querySelector('[data-picker]').onsubmit({preventDefault(){}});await new Promise(setImmediate);await page.querySelector('[data-editor]').onsubmit({preventDefault(){}})}};
}
test('saving a one-page resource draft preserves disabled pagination',async()=>{
 const ui=panel();await ui.save();assert.equal(ui.page.querySelector('[data-previous]').disabled,true);assert.equal(ui.page.querySelector('[data-next]').disabled,true);assert.match(ui.page.querySelector('[data-resources]').innerHTML,/qa.example.test/);
});
test('resource pagination cannot move beyond either boundary',async()=>{
 const ui=panel();await ui.save();ui.page.querySelector('[data-previous]').onclick();ui.page.querySelector('[data-next]').onclick();assert.equal(ui.page.querySelector('[data-count]').textContent,'Page 1 of 1 · 1 websites');
});
test('direct resource route activates its dynamically registered page',()=>{
 assert.equal(panel('#website-resource-limits').page.classes.has('active'),true);
 assert.equal(panel('#dashboard').page.classes.has('active'),false);
});


test('editing an enabled policy preserves activation instead of silently disabling it',async()=>{
 const ui=panel('',{enabled:true});await ui.save();assert.equal(ui.mutations[0].body.enabled,true);
});
test('explicit disable sends a disabled policy and retains its allowance values',async()=>{
 const ui=panel('',{enabled:true});await ui.open();assert.equal(ui.page.querySelector('[data-disable]').hidden,false);ui.page.querySelector('[data-disable]').onclick();await new Promise(setImmediate);assert.equal(ui.mutations[0].body.enabled,false);assert.equal(ui.mutations[0].body.cpuPercent,25);
});

test('blocked rows explain the cause and recovery while active rows hide stale cooldowns',async()=>{
 const settings={enabled:true,cpuPercent:25,memoryMiB:64,diskMiB:256,tasks:64,concurrentRequests:8};
 const row={id:'qa',domain:'qa.example.test',settings,state:'Resource blocked',reason:'CPU',nextRecovery:'2026-11-05T15:00:00Z',enforced:true,usage:{available:true,memoryBytes:0,processes:0}};
 const ui=panel('',{rows:[row]});await ui.page.querySelector('[data-refresh]').onclick();
 let html=ui.page.querySelector('[data-resources]').innerHTML;assert.match(html,/Cause: CPU/);assert.match(html,/Next recovery/);
 row.state='Active';await ui.page.querySelector('[data-refresh]').onclick();html=ui.page.querySelector('[data-resources]').innerHTML;
 assert.doesNotMatch(html,/Cause: CPU|Next recovery/);
 row.state='Error';row.error='Monitor stopped';await ui.page.querySelector('[data-refresh]').onclick();assert.match(ui.page.querySelector('[data-resources]').innerHTML,/Monitor stopped/);
});

test('saving unchanged blocked settings does not claim recovery succeeded',async()=>{
 const ui=panel('',{enabled:true,resultState:'Resource blocked'});await ui.save();assert.match(ui.page.querySelector('[data-status]').textContent,/remains resource blocked/);
});

test('an older disabled list row cannot overwrite an enabled editor status',async()=>{
 const ui=panel('',{enabled:true,rows:[{id:'qa',domain:'qa.example.test',settings:{enabled:false},state:'Disabled',usage:{}}]});
 await ui.open();
 await ui.page.querySelector('[data-refresh]').onclick();
 assert.equal(ui.page.querySelector('[data-runtime-status]').textContent,'Active');
});

test('saving an enabled policy reports an unverified runtime without claiming success',async()=>{
 const ui=panel('',{enabled:true,resultState:'Error',resultEnforced:false});
 await ui.save();
 assert.equal(ui.page.querySelector('[data-status]').textContent,'Resource settings saved. Error');
});

test('single and bulk creation prefill the same editable resource defaults',async()=>{
 const ui=panel('',{newSite:true});
 const expected={cpuPercent:'100',memoryMiB:'512',diskMiB:'10240',tasks:'64',concurrentRequests:'8',readMiBPerSecond:'0',writeMiBPerSecond:'0'};
 const editor=ui.page.querySelector('[data-editor]');
 const values=()=>Object.fromEntries(Object.keys(expected).map(name=>[name,editor.elements[name].value]));
 await ui.open();
 assert.deepEqual(values(),expected);
 editor.elements.memoryMiB.value='768';
 ui.page.querySelector('[data-create-all]').onclick();
 assert.deepEqual(values(),expected);
 assert.equal(ui.page.querySelector('[data-site-details]').hidden,true);
 assert.equal(ui.page.querySelector('[data-bulk-details]').hidden,false);
});

test('bulk creation enables existing drafts and new websites using their respective values',async()=>{
 const sites=[{id:'qa',domain:'qa.example.test'},{id:'second',domain:'second.example.test'}];
 const ui=panel('',{sites,manageRows:true,rows:[{...sites[0],settings:{enabled:false,cpuPercent:25},usage:{}}]});
 const editor=ui.page.querySelector('[data-editor]');
 ui.page.querySelector('[data-create-all]').onclick();
 editor.elements.memoryMiB.value='768';
 await editor.onsubmit({preventDefault(){}});
 assert.deepEqual(ui.mutations.map(x=>[x.url,x.method,x.body.enabled]),sites.map(x=>['/api/sites/resource-limits?siteId='+x.id,'PUT',true]));
 assert.equal(ui.mutations[0].body.cpuPercent,25);
 assert.equal(ui.mutations[1].body.memoryMiB,768);
 assert.match(ui.page.querySelector('[data-status]').textContent,/Enabled: 2 · Skipped: 0 · Failed: 0/);
});

test('bulk removal confirms, disables active policies first, and preserves failed policies',async()=>{
 const sites=[{id:'qa',domain:'qa.example.test'},{id:'second',domain:'second.example.test'},{id:'draft',domain:'draft.example.test'}];
 const rows=sites.map(site=>({...site,settings:{enabled:site.id!=='draft',cpuPercent:25},usage:{}}));
 const cancelled=panel('',{sites,rows,manageRows:true,confirm:false});
 await cancelled.page.querySelector('[data-remove-all]').onclick();
 assert.equal(cancelled.mutations.length,0);
 const ui=panel('',{sites,rows,manageRows:true,failSite:'second'});
 await ui.page.querySelector('[data-remove-all]').onclick();
 assert.deepEqual(ui.mutations.map(x=>[x.url.split('=').at(-1),x.method]),[['qa','PUT'],['qa','DELETE'],['second','PUT'],['draft','DELETE']]);
 assert.equal(ui.mutations[0].body.enabled,false);
 assert.match(ui.page.querySelector('[data-status]').textContent,/Removed: 2.*Failed: 1.*second.example.test/);
 assert.match(ui.page.querySelector('[data-resources]').innerHTML,/second.example.test/);
});

test('bulk enable distinguishes blocked enforcement from a stale runtime that needs recovery',async()=>{
 const sites=[{id:'blocked',domain:'blocked.example.test'},{id:'stale',domain:'stale.example.test'}];
 const ui=panel('',{sites,manageRows:true,rows:[
  {...sites[0],settings:{enabled:true},state:'Resource blocked',enforced:true},
  {...sites[1],settings:{enabled:true},state:'Error',enforced:false},
 ]});
 ui.page.querySelector('[data-create-all]').onclick();
 await ui.page.querySelector('[data-editor]').onsubmit({preventDefault(){}});
 assert.deepEqual(ui.mutations.map(x=>[x.url.split('=').at(-1),x.method]),[['stale','POST']]);
 assert.match(ui.page.querySelector('[data-status]').textContent,/Failed: 0.*Already enforced but blocked: 1.*Recovery requested: 1/);
});


function resourceRow(domain,usage={},settings={},state='Active'){
 return {id:domain,domain:domain+'.example.test',state,enforced:true,
  settings:{enabled:true,cpuPercent:100,memoryMiB:512,diskMiB:10240,tasks:64,concurrentRequests:8,...settings},
  usage:{available:true,cpuPercent:0,memoryBytes:0,diskBytes:0,processes:0,...usage}};
}
function displayedResourceDomains(ui){
 return Array.from(ui.page.querySelector('[data-resources]').innerHTML.matchAll(/<tr><td>([^<]+)<br>/g),match=>match[1].replace('.example.test',''));
}
function clickResourceSort(ui,key){ui.page.querySelector('[data-sort-key="'+key+'"]').onclick();}

for(const [key,usageKey] of [['cpuPercent','cpuPercent'],['memoryMiB','memoryBytes'],['diskMiB','diskBytes'],['tasks','processes']]){
 test(key+' sorts current usage high/low instead of the configured allowance',async()=>{
  const rows=[
   resourceRow('low',{[usageKey]:5},{[key]:9000}),
   resourceRow('high',{[usageKey]:80},{[key]:10}),
   resourceRow('middle',{[usageKey]:25},{[key]:500})
  ];
  const ui=panel('',{rows});await ui.page.querySelector('[data-refresh]').onclick();
  clickResourceSort(ui,key);
  assert.deepEqual(displayedResourceDomains(ui),['high','middle','low']);
  assert.equal(ui.page.querySelector('[data-sort-column="'+key+'"]').getAttribute('aria-sort'),'descending');
  clickResourceSort(ui,key);
  assert.deepEqual(displayedResourceDomains(ui),['low','middle','high']);
  assert.equal(ui.page.querySelector('[data-sort-column="'+key+'"]').getAttribute('aria-sort'),'ascending');
 });
}

for(const [key,usageKey] of [['cpuPercent','cpuPercent'],['memoryMiB','memoryBytes'],['tasks','processes']]){
 test(key+' keeps unavailable samples last and sorts a real zero as measured usage',async()=>{
  const rows=[
   resourceRow('missing',{[usageKey]:null}),
   resourceRow('unavailable',{available:false,[usageKey]:10000}),
   resourceRow('zero',{[usageKey]:0}),
   resourceRow('positive',{[usageKey]:20}),
   resourceRow('invalid',{[usageKey]:Number.NaN})
  ];
  const ui=panel('',{rows});await ui.page.querySelector('[data-refresh]').onclick();
  clickResourceSort(ui,key);
  assert.deepEqual(displayedResourceDomains(ui),['positive','zero','invalid','missing','unavailable']);
  clickResourceSort(ui,key);
  assert.deepEqual(displayedResourceDomains(ui),['zero','positive','invalid','missing','unavailable']);
 });
}

test('disk sorting uses cached byte usage even when process measurements are unavailable',async()=>{
 const rows=[resourceRow('missing',{diskBytes:null}),resourceRow('cached-high',{available:false,diskBytes:900}),resourceRow('zero',{diskBytes:0}),resourceRow('low',{diskBytes:50})];
 const ui=panel('',{rows});await ui.page.querySelector('[data-refresh]').onclick();
 clickResourceSort(ui,'diskMiB');assert.deepEqual(displayedResourceDomains(ui),['cached-high','low','zero','missing']);
 clickResourceSort(ui,'diskMiB');assert.deepEqual(displayedResourceDomains(ui),['zero','low','cached-high','missing']);
});

test('usage sorting orders the complete list before pagination and keeps alphabetical ties',async()=>{
 const rows=Array.from({length:15},(_,index)=>resourceRow('site-'+String(index+1).padStart(2,'0'),{cpuPercent:index+1}));
 rows.push(resourceRow('aaa-tie',{cpuPercent:15}));
 const ui=panel('',{rows});await ui.page.querySelector('[data-refresh]').onclick();
 clickResourceSort(ui,'cpuPercent');
 assert.deepEqual(displayedResourceDomains(ui),['aaa-tie','site-15','site-14','site-13','site-12','site-11','site-10','site-09','site-08','site-07']);
 ui.page.querySelector('[data-next]').onclick();
 assert.deepEqual(displayedResourceDomains(ui),['site-06','site-05','site-04','site-03','site-02','site-01']);
 clickResourceSort(ui,'cpuPercent');
 assert.deepEqual(displayedResourceDomains(ui),['site-01','site-02','site-03','site-04','site-05','site-06','site-07','site-08','site-09','site-10']);
 assert.equal(ui.page.querySelector('[data-count]').textContent,'Page 1 of 2 \u00b7 16 websites');
});

test('usage refresh reorders rows while retaining the chosen metric and direction',async()=>{
 const rows=[resourceRow('alpha',{memoryBytes:10}),resourceRow('beta',{memoryBytes:30})];
 const ui=panel('',{rows});await ui.page.querySelector('[data-refresh]').onclick();
 clickResourceSort(ui,'memoryMiB');assert.deepEqual(displayedResourceDomains(ui),['beta','alpha']);
 rows[0].usage.memoryBytes=50;
 await ui.page.querySelector('[data-refresh]').onclick();
 assert.deepEqual(displayedResourceDomains(ui),['alpha','beta']);
 assert.equal(ui.page.querySelector('[data-sort-column="memoryMiB"]').getAttribute('aria-sort'),'descending');
 clickResourceSort(ui,'cpuPercent');
 assert.equal(ui.page.querySelector('[data-sort-column="memoryMiB"]').getAttribute('aria-sort'),null);
});

test('status sorting retains alphabetical status order and domain tie ordering',async()=>{
 const rows=[resourceRow('blocked',{},{} ,'Resource blocked'),resourceRow('z-active'),resourceRow('disabled',{},{} ,'Disabled'),resourceRow('a-active')];
 const ui=panel('',{rows});await ui.page.querySelector('[data-refresh]').onclick();
 clickResourceSort(ui,'status');assert.deepEqual(displayedResourceDomains(ui),['a-active','z-active','disabled','blocked']);
 clickResourceSort(ui,'status');assert.deepEqual(displayedResourceDomains(ui),['blocked','disabled','a-active','z-active']);
});
