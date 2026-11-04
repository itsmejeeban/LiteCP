const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
test('unchanged server scan polling preserves report controls and open details',async()=>{
 const source=fs.readFileSync('web/server-scan.js','utf8'),start=source.indexOf(' async function refresh()'),end=source.indexOf(' button.onclick=',start);
 let renders=0;const elements={frequency:{},time:{}};
 const payload={available:true,status:{state:'running',output:''},schedule:{frequency:'daily',time:'02:00'}};
 const context={button:{},status:{},output:{},schedule:{elements},window:{LiteCP:{api:async()=>payload},LiteCPScanReport:{render:()=>renders++}}};
 vm.runInNewContext('let initialized=false,lastReport=null;'+source.slice(start,end)+';this.refresh=refresh;',context);
 await context.refresh();await context.refresh();assert.equal(renders,1);assert.equal(context.button.disabled,true);
 payload.status.output='test output';await context.refresh();assert.equal(renders,2);
 assert.equal(elements.frequency.value,'daily');assert.equal(elements.time.value,'02:00');
 delete payload.status;await context.refresh();assert.match(context.status.textContent,/temporarily unavailable/);
});
