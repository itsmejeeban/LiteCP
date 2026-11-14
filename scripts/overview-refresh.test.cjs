const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
test('overview refresh is bounded, pauses outside dashboard and reports errors',async()=>{
 let requests=0,finish,interval,fail=false;const events={},status={};
 const location={hash:'#dashboard'},document={hidden:false,createElement:()=>status,querySelector:s=>s==='#overviewMetricsStatus'?status:{after(){}},addEventListener:(k,f)=>events[k]=f};
 const cp={state:()=>({}),refreshMetrics:()=>{requests++;return fail?Promise.reject(Error('offline')):new Promise(resolve=>finish=resolve);}};
 const features={navigate(){}};
 vm.runInNewContext(fs.readFileSync('web/overview-refresh.js','utf8'),{window:{LiteCP:cp,LiteCPFeatures:features,addEventListener:(k,f)=>events[k]=f},document,location,Date,setInterval(fn,ms){assert.equal(ms,15000);interval=fn;}});
 assert.equal(requests,1);await interval();assert.equal(requests,1);
 finish();await new Promise(setImmediate);assert.match(status.textContent,/Metrics updated/);
 location.hash='#websites';await interval();assert.equal(requests,1);
 location.hash='#dashboard';document.hidden=true;await interval();assert.equal(requests,1);
 document.hidden=false;fail=true;await interval();assert.match(status.textContent,/out of date/);
});
test('preset primary icons inherit the readable foreground rather than forced white',()=>{
 const css=fs.readFileSync('web/appearance.css','utf8');
 assert.match(css,/\.button\.button-primary svg\{color:inherit;stroke:currentColor;flex-shrink:0\}/);
});
