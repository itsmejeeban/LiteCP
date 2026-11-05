const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync(__dirname+'/system-updates.js','utf8');
const code=source.slice(source.indexOf(' async function perform('),source.indexOf(" page.querySelector('[data-check]').onclick"));
test('operations wait for loaded state and cannot overlap background work',async()=>{
 const calls=[];const c=vm.createContext({data:null,pending:false,request:0,timer:null,loadedSchedule:true,render(){},clearTimeout(){},setTimeout(){},status:{},load(){},cp:{api:async()=>{calls.push(1);return {state:'running',message:'started'};}}});
 vm.runInContext(code+';this.run=perform;',c);
 await c.run({action:'check'});assert.equal(calls.length,0);
 c.data={status:{state:'running'}};await c.run({action:'check'});assert.equal(calls.length,0);
 c.data={status:{state:'idle'}};await c.run({action:'check'});assert.equal(calls.length,1);assert.equal(c.request,1);
});
test('failed schedule submission preserves unsaved form state',async()=>{
 const c=vm.createContext({data:{status:{state:'idle'}},pending:false,request:0,timer:null,loadedSchedule:true,render(){},clearTimeout(){},setTimeout(){},status:{},load(){},cp:{api:async()=>{throw Error('server unavailable');}}});
 vm.runInContext(code+';this.run=perform;',c);await c.run({action:'schedule'});
 assert.equal(c.loadedSchedule,true);assert.equal(c.pending,false);assert.equal(c.status.textContent,'server unavailable');
});
