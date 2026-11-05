const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('web/features.js','utf8');
const line=source.split('\n').find(line=>line.includes("$$('[data-service=\"mail\"]')"));
test('mail controls retain running/stopped/disabled protections after feature rendering',()=>{
 assert.ok(line);
 for(const state of ['running','stopped','degraded','not installed'])for(const enabled of [true,false]){
  const buttons=['start','stop','restart'].map(action=>({dataset:{action},disabled:false}));
  vm.runInNewContext(line,{$$:()=>buttons,state:{settings:{emailHostingEnabled:enabled},system:{services:[{key:'mail',state}]}}});
  assert.equal(buttons[0].disabled,!enabled||state==='not installed'||state==='running');
  assert.equal(buttons[1].disabled,!enabled||state==='not installed'||state==='stopped');
  assert.equal(buttons[2].disabled,!enabled||state==='not installed');
 }
});
test('service cards use timer display labels without changing canonical controls',()=>{
 const app=fs.readFileSync('web/app.js','utf8');
 assert.match(app,/escapeHTML\(service.stateLabel \|\| service.state\)/);
 assert.match(app,/action === "start" && service.state === "running"/);
});
