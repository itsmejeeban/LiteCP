const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
test('security actions reflect installed capabilities and recover after installation',()=>{
 const source=fs.readFileSync('web/features.js','utf8');
 const start=source.indexOf('  function render(state){'),end=source.indexOf('  window.LiteCPFeatures=',start);
 assert.ok(start>=0&&end>start);
 const nodes=new Map(),wafSelect={disabled:false};
 const $=selector=>{if(!nodes.has(selector))nodes.set(selector,{value:'',classList:{toggle(){}},innerHTML:'',textContent:'',disabled:false});return nodes.get(selector);};
 const scope={$, $$:selector=>selector==='#wafTable select'?[wafSelect]:[],esc:String,fillSiteSelect(){},document:{activeElement:null},location:{hostname:'panel.example.test'}};
 vm.runInNewContext(source.slice(start,end)+'\nthis.render=render;',scope);
 const state={sites:[],mailboxes:[],mailDomains:[],settings:{},system:{antivirusAvailable:false,wafEngine:'not installed'}};
 scope.render(state);
 assert.equal($('#scanForm button[type="submit"]').disabled,true);assert.equal(wafSelect.disabled,true);
 assert.match($('#scannerAvailability').textContent,/not installed/);assert.match($('#wafEngineLabel').textContent,/not installed/);
 state.system.antivirusAvailable=true;state.system.wafEngine='ModSecurity';scope.render(state);
 assert.equal($('#scanForm button[type="submit"]').disabled,false);assert.equal(wafSelect.disabled,false);
 assert.match($('#scannerAvailability').textContent,/available/);assert.match($('#wafEngineLabel').textContent,/ModSecurity/);
});
