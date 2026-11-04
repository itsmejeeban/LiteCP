const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
function panel(){
 class Element{
  constructor(){this.children=new Map();this.dataset={};this.value='qa';this.hidden=false;this.disabled=false;this.classList={add(){}};this.options=[{value:'qa'}];this.elements=Object.fromEntries(['cpuPercent','memoryMiB','diskMiB','tasks','concurrentRequests','readMiBPerSecond','writeMiBPerSecond'].map(k=>[k,{value:'64'}]));}
  querySelector(k){if(!this.children.has(k))this.children.set(k,new Element());return this.children.get(k)}
  querySelectorAll(){return [...this.children.values()].flatMap(x=>[x,...x.children.values()])}
  append(){}insertAdjacentHTML(){}reset(){}
 }
 const page=new Element(),root=new Element();let rows=[];
 const settings={cpuPercent:25,memoryMiB:256,diskMiB:1024,tasks:64,concurrentRequests:8};
 const state={sites:[{id:'qa',domain:'qa.example.test'}]};
 const api=async(url,options)=>{if(options){rows=[{id:'qa',domain:'qa.example.test',settings,usage:{}}];return {}}if(url.includes('?'))return {settings,capabilities:{prerequisites:[]}};return {items:rows,updatedAt:new Date().toISOString()}};
 vm.runInNewContext(fs.readFileSync(path.join(__dirname,'../web/website-resource-limits.js'),'utf8'),{document:{createElement:()=>page,querySelector:()=>root,hidden:true,addEventListener(){}},location:{hash:''},window:{LiteCP:{escapeHTML:String,state:()=>state,api,formatBytes:String},LiteCPFeatures:{render(){}},addEventListener(){}},setTimeout(){},clearTimeout(){}});
 return {page,save:async()=>{page.querySelector('[data-picker]').onsubmit({preventDefault(){}});await new Promise(setImmediate);await page.querySelector('[data-editor]').onsubmit({preventDefault(){}})}};
}
test('saving a one-page resource draft preserves disabled pagination',async()=>{
 const ui=panel();await ui.save();assert.equal(ui.page.querySelector('[data-previous]').disabled,true);assert.equal(ui.page.querySelector('[data-next]').disabled,true);assert.match(ui.page.querySelector('[data-resources]').innerHTML,/qa.example.test/);
});
test('resource pagination cannot move beyond either boundary',async()=>{
 const ui=panel();await ui.save();ui.page.querySelector('[data-previous]').onclick();ui.page.querySelector('[data-next]').onclick();assert.equal(ui.page.querySelector('[data-count]').textContent,'Page 1 of 1 · 1 websites');
});
