const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
function panel(hash='',options={} ){
 class Element{
  constructor(){this.children=new Map();this.dataset={};this.value='qa';this.hidden=false;this.disabled=false;this.classes=new Set();this.classList={add:(name)=>this.classes.add(name)};this.options=[{value:'qa'}];this.elements=Object.fromEntries(['cpuPercent','memoryMiB','diskMiB','tasks','concurrentRequests','readMiBPerSecond','writeMiBPerSecond'].map(k=>[k,{value:'64'}]));this.elements.enabled={checked:false,disabled:false};}
  querySelector(k){if(!this.children.has(k))this.children.set(k,new Element());return this.children.get(k)}
  querySelectorAll(){return [...this.children.values()].flatMap(x=>[x,...x.children.values()])}
  append(){}insertAdjacentHTML(){}reset(){}
 }
 const page=new Element(),root=new Element();let rows=options.rows||[],mutations=[];
 const settings={enabled:!!options.enabled,cpuPercent:25,memoryMiB:256,diskMiB:1024,tasks:64,concurrentRequests:8};
 const state={sites:[{id:'qa',domain:'qa.example.test'}]};
 const api=async(url,request)=>{if(request){mutations.push(request);rows=[{id:'qa',domain:'qa.example.test',settings,usage:{}}];return {state:options.resultState||'Active'}}if(url.includes('?'))return {settings,capabilities:{ready:true,prerequisites:[]}};return {items:rows,updatedAt:new Date().toISOString()}};
 vm.runInNewContext(fs.readFileSync(path.join(__dirname,'../web/website-resource-limits.js'),'utf8'),{document:{createElement:()=>page,querySelector:()=>root,hidden:true,addEventListener(){}},location:{hash},window:{LiteCP:{escapeHTML:String,state:()=>state,api,formatBytes:String},LiteCPFeatures:{render(){}},addEventListener(){}},setTimeout(){},clearTimeout(){}});
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
