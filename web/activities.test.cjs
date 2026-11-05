const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync(__dirname+'/activities.js','utf8');
function setup(){
 const nodes=new Map(),writes=[];const node=key=>{if(!nodes.has(key))nodes.set(key,{value:'0'});return nodes.get(key);};
 const context=vm.createContext({document:{querySelector:node},cp:{api:async(path,options)=>{if(options)writes.push(options);return path.includes('activity-policy')?{days:0}:{items:[],pages:1,total:0};}},window:{LiteCPDialogs:{confirm:async()=>false},LiteCPFeatures:{activity(){}}}});
 const code=source.slice(source.indexOf(' let current=1'),source.indexOf(' const previousNavigate='));
 vm.runInContext(code+';this.reload=load;this.changePolicy=change;',context);return {context,node,writes};
}
test('refresh preserves unsaved retention after focus moves away',async()=>{
 const h=setup();await h.context.reload();const select=h.node('#activityRetention');select.value='30';select.onchange();await h.context.reload();
 assert.equal(select.value,'30');assert.equal(h.node('#refreshActivities').disabled,false);
});
test('cancelled confirmation releases operation guard without deleting events',async()=>{
 const h=setup();let prompts=0;h.context.window.LiteCPDialogs.confirm=async()=>{prompts++;return false;};
 await h.context.changePolicy(true);await h.context.changePolicy(false);assert.equal(prompts,2);assert.equal(h.writes.length,0);
});
