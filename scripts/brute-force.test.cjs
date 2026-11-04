const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('web/brute-force.js','utf8');
function fixture(){
 const nodes={};
 const context={busy:false,data:{installed:true,running:true,jails:[]},loadError:false,esc:String,toggle:{},message:{},form:{elements:{jail:{value:'sshd',innerHTML:''}}},events:()=>{},rule:()=>{},page:{querySelector:selector=>nodes[selector]??=( {})},cp:{api:async()=>{throw new Error('Request failed (502)')}}};
 const start=source.indexOf(' async function load()'),end=source.indexOf(' async function update(',start);
 vm.runInNewContext(source.slice(start,end)+';this.load=load;',context);
 return {context,nodes};
}
test('read failure removes stale status, disables actions, and clears after successful retry',async()=>{
 const {context,nodes}=fixture();
 await context.load();
 assert.equal(context.data,null);assert.equal(context.toggle.disabled,true);
 assert.match(nodes['[data-state]'].textContent,/could not be verified/);
 assert.equal(nodes['[data-next]'].disabled,true);
 context.cp.api=async()=>({installed:true,running:true,jails:[{name:'sshd'}],logsAvailable:true,events:[]});
 await context.load();
 assert.equal(context.message.textContent,'');assert.equal(context.loadError,false);
 assert.equal(nodes['[data-state]'].textContent,'Protection is running.');
 assert.equal(context.toggle.disabled,false);
});
test('refresh locks save and does not erase an unrelated settings result',async()=>{
 const {context,nodes}=fixture();let resolve;
 context.message.textContent='Protection settings updated.';
 context.cp.api=()=>new Promise(done=>resolve=done);
 const request=context.load();
 assert.equal(nodes['[data-save]'].disabled,true);
 resolve({installed:true,running:true,jails:[],events:[]});await request;
 assert.equal(context.message.textContent,'Protection settings updated.');
});
