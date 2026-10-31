const test=require('node:test'),assert=require('node:assert/strict'),vm=require('node:vm'),fs=require('node:fs');
function setup(accept=true){
 class Element{constructor(){this.nodes=new Map();this.className='';}append(){}addEventListener(){}showModal(){this.open=true}close(){this.open=false}remove(){}querySelector(key){if(!this.nodes.has(key))this.nodes.set(key,new Element());return this.nodes.get(key)}}
 const calls=[],toasts=[];const cp={escapeHTML:String,formatDateTime:String,formatBytes:String,refresh:async()=>{},showToast:(...args)=>toasts.push(args),handleError(){},api:async(path,options)=>{calls.push({path,options});return path.includes('?')?{status:'restored-review',message:'Restore finished'}:{id:'job-1',status:'running',stage:'verify',message:'Inspecting backup'}}};
 const window={LiteCP:cp,LiteCPFeatures:{navigate(){}},LiteCPDialogs:{confirm:async()=>accept},addEventListener(){}};
 vm.runInNewContext(fs.readFileSync(__dirname+'/backup-restore.js','utf8'),{window,document:{body:new Element(),createElement:()=>new Element(),querySelector:()=>new Element()},location:{hash:'#dashboard'},setTimeout:fn=>{fn();return 1}});
 return {window,calls,toasts};
}
test('cancelled overwrite confirmation makes no restore request',async()=>{const h=setup(false);await h.window.LiteCPBackupRestore({id:'backup',siteId:'site',domain:'example.com'});assert.equal(h.calls.length,0)});
test('confirmed restore starts background job and polls progress',async()=>{const h=setup();await h.window.LiteCPBackupRestore({id:'backup',siteId:'site',domain:'example.com',kind:'history'});assert.equal(h.calls[0].path,'/api/backups/restore-direct');assert.equal(h.calls[0].options.body.confirmDomain,'example.com');assert.equal(h.calls[0].options.body.confirm,true);assert.equal(h.calls[1].path,'/api/backups/restore-direct?id=job-1');assert.equal(h.toasts[0][0],'Restore finished')});
