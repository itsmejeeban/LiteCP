const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
function setup(action){
 const source=fs.readFileSync('web/app.js','utf8'),start=source.indexOf('  async function handleSiteAction('),end=source.indexOf('  async function handlePHPChange(',start);
 let answer,request;const waiting=new Promise(resolve=>answer=resolve),calls=[],button={dataset:{siteAction:action,id:'site1'},disabled:false};
 const state={sites:[{id:'site1',domain:'fixture.example.test',ssl:false}],settings:{email:''}};
 const dialogs={confirm:(message,options)=>{request={message,options};return waiting;},prompt:(message,value,options)=>{request={message,value,options};return waiting;}};
 const context={currentState:state,window:{LiteCPDialogs:dialogs},api:async(url,options)=>{calls.push({url,options});return state;},showToast(){},handleError(error){throw error;},renderAll(){},encodeURIComponent};
 vm.runInNewContext(source.slice(start,end)+'\nthis.run=handleSiteAction;',context);
 return {answer,calls,button,request:()=>request,run:()=>context.run({target:{closest:()=>button}})};
}
test('SSL requests wait for the validated email dialog; cancelling sends no request',async()=>{
 const p=setup('ssl'),work=p.run();assert.equal(p.calls.length,0);assert.equal(p.request().options.type,'email');assert.equal(p.request().options.required,true);assert.equal(p.request().options.confirmText,'Enable SSL');
 p.answer(null);await work;assert.equal(p.calls.length,0);assert.equal(p.button.disabled,false);
 const accepted=setup('ssl'),running=accepted.run();accepted.answer('owner@example.test');await running;assert.equal(accepted.calls[0].url,'/api/sites/ssl');assert.equal(accepted.calls[0].options.body.email,'owner@example.test');
});
test('detach keeps its destructive scope and waits for explicit styled confirmation',async()=>{
 const p=setup('delete'),work=p.run();assert.equal(p.calls.length,0);assert.equal(p.request().options.confirmText,'Detach website');assert.equal(p.request().options.danger,true);assert.match(p.request().message,/files and database will be preserved/);
 p.answer(false);await work;assert.equal(p.calls.length,0);
 const accepted=setup('delete'),running=accepted.run();accepted.answer(true);await running;assert.equal(accepted.calls[0].url,'/api/sites/site1');assert.equal(accepted.calls[0].options.method,'DELETE');
});
