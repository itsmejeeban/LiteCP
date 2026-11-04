const test=require('node:test'),assert=require('node:assert/strict'),vm=require('node:vm'),fs=require('node:fs');
function setup(accept=true,terminal='restored',hash='#dashboard'){
 class Element{constructor(){this.nodes=new Map();this.className='';}append(){}addEventListener(){}showModal(){this.open=true}close(){this.open=false}remove(){}querySelector(key){if(!this.nodes.has(key))this.nodes.set(key,new Element());return this.nodes.get(key)}}
 const calls=[],toasts=[],elements=[];const cp={escapeHTML:String,formatDateTime:String,formatBytes:String,refresh:async()=>{},showToast:(...args)=>toasts.push(args),handleError(){},api:async(path,options)=>{calls.push({path,options});if(path==='/api/backups/restorable')return {items:[]};return path.includes('?')?{status:terminal,message:'Verification result'}:{id:'job-1',status:'running',stage:'verify',message:'Inspecting backup'}}};
 const window={LiteCP:cp,LiteCPFeatures:{navigate(){}},LiteCPDialogs:{confirm:async()=>accept},addEventListener(){}};
 vm.runInNewContext(fs.readFileSync(__dirname+'/backup-restore.js','utf8'),{window,document:{body:new Element(),createElement:()=>{const el=new Element();elements.push(el);return el;},querySelector:()=>new Element()},location:{hash},setTimeout:fn=>{fn();return 1}});
 return {window,calls,toasts,elements};
}
test('cancelled overwrite confirmation makes no restore request',async()=>{const h=setup(false);await h.window.LiteCPBackupRestore({id:'backup',siteId:'site',domain:'example.com'});assert.equal(h.calls.length,0)});
test('confirmed restore starts background job and polls progress',async()=>{const h=setup();await h.window.LiteCPBackupRestore({id:'backup',siteId:'site',domain:'example.com',kind:'history'});assert.equal(h.calls[0].path,'/api/backups/restore-direct');assert.equal(h.calls[0].options.body.confirmDomain,'example.com');assert.equal(h.calls[0].options.body.confirm,true);assert.equal(h.calls[1].path,'/api/backups/restore-direct?id=job-1');assert.equal(h.toasts[0][0],'Restore finished')});

test('missing website restore is confirmed and submitted',async()=>{const h=setup();await h.window.LiteCPBackupRestore({id:'backup',siteId:'removed-site',domain:'example.com',kind:'history',websiteExists:false});assert.equal(h.calls[0].options.body.siteID,'removed-site');assert.equal(h.calls[0].options.body.confirm,true)});

test('health-check review is never presented as successful completion',async()=>{
 const h=setup(true,'restored-review');await h.window.LiteCPBackupRestore({id:'backup',siteId:'site',domain:'example.com'});
 assert.equal(h.toasts[0][0],'Restore needs review');assert.equal(h.toasts[0][2],'error');
 assert.match(h.elements[1].querySelector('[data-result]').textContent,/verification needs review/);
});

test('a second restore click while confirmation is pending cannot submit another job',async()=>{
 const h=setup();let answer,confirmations=0;
 h.window.LiteCPDialogs.confirm=()=>{confirmations++;return new Promise(resolve=>{answer=resolve;});};
 const first=h.window.LiteCPBackupRestore({id:'one',domain:'example.com'});
 await h.window.LiteCPBackupRestore({id:'two',domain:'example.com'});
 assert.equal(confirmations,1);assert.equal(h.calls.length,0);
 answer(true);await first;assert.equal(h.calls.filter(c=>c.options?.method==='POST').length,1);
});

test('restore completion reloads the inventory on the Restore page',async()=>{
 const h=setup(true,'restored','#backups/restore');
 await new Promise(resolve=>setImmediate(resolve));const before=h.calls.filter(c=>c.path==='/api/backups/restorable').length;
 await h.window.LiteCPBackupRestore({id:'backup',domain:'example.com'});
 assert.equal(h.calls.filter(c=>c.path==='/api/backups/restorable').length,before+1);
});
