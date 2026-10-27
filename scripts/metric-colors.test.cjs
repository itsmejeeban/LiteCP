const test=require('node:test'),assert=require('node:assert/strict'),vm=require('node:vm'),fs=require('node:fs');
const source=fs.readFileSync('web/app.js','utf8');
const fn=source.slice(source.indexOf('  function setMetric('),source.indexOf('  function renderServices('));
test('CPU, memory and disk use green, yellow and red at the requested boundaries and reset after usage drops',()=>{
 const nodes=new Map();const $=id=>{if(!nodes.has(id))nodes.set(id,{style:{},textContent:''});return nodes.get(id);};
 const context={$};vm.createContext(context);vm.runInContext(fn+';this.metric=setMetric;',context);
 for(const name of ['cpu','memory','disk'])for(const [value,color] of [[0,'#22c55e'],[50,'#22c55e'],[51,'#eab308'],[52,'#eab308'],[70,'#eab308'],[70.1,'#ef4444'],[100,'#ef4444'],[20,'#22c55e']]){
  context.metric(name,value);assert.equal($(`#${name}Bar`).style.background,color);assert.equal($(`#${name}Bar`).style.width,`${value}%`);
 }
});
