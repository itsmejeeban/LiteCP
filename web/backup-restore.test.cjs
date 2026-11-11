const test=require('node:test'),assert=require('node:assert/strict'),vm=require('node:vm'),fs=require('node:fs');
function setup(accept=true,terminal='restored',hash='#dashboard',options={}){
 class Element{
  constructor(){this.nodes=new Map();this.handlers=new Map();this.className='';this.innerHTML='';this.disabled=false;}
  append(){}addEventListener(type,fn){this.handlers.set(type,fn);}showModal(){this.open=true;}close(){this.open=false;}remove(){}
  querySelector(key){if(!this.nodes.has(key))this.nodes.set(key,new Element());return this.nodes.get(key);}
  querySelectorAll(){return [];}
 }
 const calls=[],toasts=[],errors=[],confirmations=[],elements=[],location={hash};
 let inventory=options.items||[],refreshes=0;
 const cp={escapeHTML:value=>String(value).replaceAll('&','&amp;').replaceAll('<','&lt;').replaceAll('>','&gt;').replaceAll('"','&quot;'),formatDateTime:String,formatBytes:String,
  refresh:async()=>{refreshes++;},showToast:(...args)=>toasts.push(args),handleError:error=>errors.push(error.message),
  api:async(path,request)=>{
   calls.push({path,options:request});
   if(path==='/api/backups/restorable'){
    if(options.loadInventory)return options.loadInventory();
    return {items:JSON.parse(JSON.stringify(inventory)),warnings:[]};
   }
   if(request?.method==='DELETE'){
    if(options.deleteBackup)await options.deleteBackup(path,request);
    const id=request.body?.id||new URL(path,'https://panel.test').searchParams.get('id');
    inventory=inventory.filter(item=>item.id!==id);return {ok:true};
   }
   return path.includes('?')?{status:terminal,message:'Verification result'}:{id:'job-1',status:'running',stage:'verify',message:'Inspecting backup'};
  }};
 const window={LiteCP:cp,LiteCPFeatures:{navigate(){}},LiteCPDialogs:{confirm:async(...args)=>{confirmations.push(args);return accept;}},addEventListener(){}};
 vm.runInNewContext(fs.readFileSync(__dirname+'/backup-restore.js','utf8'),{window,document:{body:new Element(),createElement:()=>{const el=new Element();elements.push(el);return el;},querySelector:()=>new Element()},location,setTimeout:fn=>{fn();return 1}});
 const click=(selector,index)=>{
  const dataset=selector==='[data-delete-backup-index]'?{deleteBackupIndex:String(index)}:selector==='[data-restore-index]'?{restoreIndex:String(index)}:{};
  return elements[0].handlers.get('click')({target:{closest:key=>key===selector?{dataset}:null}});
 };
 const flush=()=>new Promise(resolve=>setImmediate(resolve));
 return {window,calls,toasts,errors,confirmations,elements,flush,refreshes:()=>refreshes,
  clickDelete:index=>click('[data-delete-backup-index]',index),clickRestore:index=>click('[data-restore-index]',index),
  setItems:items=>{inventory=items;},reload:async()=>{location.hash='#backups/restore';window.LiteCPFeatures.navigate();await flush();}};
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


const backup=(overrides={})=>({id:'backup-one',siteId:'site-one',domain:'example.com',name:'example.com-backup.tar.gz',kind:'history',size:1024,createdAt:'2026-11-11T00:00:00Z',local:false,remote:true,destination:'Daily remote',websiteExists:true,...overrides});

test('every inventory row renders Delete beside Restore',async()=>{
 const h=setup(true,'restored','#backups/restore',{items:[backup(),backup({id:'manual',kind:'website-zip',local:true,remote:false})]});await h.flush();
 const html=h.elements[0].querySelector('[data-restore-list]').innerHTML;
 assert.equal((html.match(/data-delete-backup-index=/g)||[]).length,2);
 for(const index of [0,1])assert.match(html,new RegExp(`data-restore-index="${index}"[\\s\\S]*?data-delete-backup-index="${index}"`));
 assert.match(html,/class="[^"]*danger[^"]*"[^>]*data-delete-backup-index|data-delete-backup-index[^>]*class="[^"]*danger/);
});

test('cancelled deletion keeps the backup and makes no DELETE request',async()=>{
 const h=setup(false,'restored','#backups/restore',{items:[backup()]});await h.flush();await h.clickDelete(0);
 assert.equal(h.calls.filter(call=>call.options?.method==='DELETE').length,0);
 assert.match(h.elements[0].querySelector('[data-restore-list]').innerHTML,/example.com-backup.tar.gz/);
 assert.equal(h.toasts.length,0);
});

test('confirmed remote history deletion names the exact backup and storage and reloads inventory',async()=>{
 const h=setup(true,'restored','#backups/restore',{items:[backup({id:'backup /?&'})]});await h.flush();
 const before=h.calls.filter(call=>call.path==='/api/backups/restorable').length;
 await h.clickDelete(0);await h.flush();
 const removed=h.calls.find(call=>call.options?.method==='DELETE');
 assert.equal(removed.path,'/api/backups/item?id=backup%20%2F%3F%26&remote=true');
 const [message,settings]=h.confirmations[0];
 assert.match(message,/example\.com-backup\.tar\.gz/);assert.match(message,/example\.com/);assert.match(message,/Daily remote/);
 assert.equal(settings.danger,true);
 assert.equal(h.calls.filter(call=>call.path==='/api/backups/restorable').length,before+1);
 assert.match(h.elements[0].querySelector('[data-restore-list]').innerHTML,/No available/);
 assert.equal(h.refreshes(),1);assert.equal(h.toasts[0][2],'success');
});

test('recorded but unavailable remote copy is included in deletion rather than orphaned',async()=>{
 const h=setup(true,'restored','#backups/restore',{items:[backup({local:true,remote:false,remoteRecorded:true})]});await h.flush();await h.clickDelete(0);
 assert.equal(h.calls.find(call=>call.options?.method==='DELETE').path,'/api/backups/item?id=backup-one&remote=true');
 assert.match(h.confirmations[0][0],/Daily remote/);
});

test('local-only history deletion does not request remote removal',async()=>{
 const h=setup(true,'restored','#backups/restore',{items:[backup({local:true,remote:false})]});await h.flush();await h.clickDelete(0);
 assert.equal(h.calls.find(call=>call.options?.method==='DELETE').path,'/api/backups/item?id=backup-one&remote=false');
 assert.match(h.confirmations[0][0],/local/i);
});

test('manual ZIP deletion uses its website and explicitly includes the recorded remote copy',async()=>{
 const h=setup(true,'restored','#backups/restore',{items:[backup({id:'manual-id',siteId:'removed /?&',kind:'website-zip',local:false,remote:true,websiteExists:false})]});await h.flush();await h.clickDelete(0);
 const call=h.calls.find(call=>call.options?.method==='DELETE');
 assert.equal(call.path,'/api/website-backups?siteId=removed%20%2F%3F%26');
 assert.deepEqual(JSON.parse(JSON.stringify(call.options.body)),{id:'manual-id',confirm:true,removeRemote:true});
 assert.equal(h.calls.some(call=>call.path.includes('restore-direct')),false);
});

test('local manual ZIP deletion never requests a remote operation',async()=>{
 const h=setup(true,'restored','#backups/restore',{items:[backup({id:'manual-local',kind:'website-zip',local:true,remote:false})]});await h.flush();await h.clickDelete(0);
 const call=h.calls.find(call=>call.options?.method==='DELETE');
 assert.deepEqual(JSON.parse(JSON.stringify(call.options.body)),{id:'manual-local',confirm:true,removeRemote:false});
});

test('delete confirmation remains bound to the selected backup after inventory changes',async()=>{
 const h=setup(true,'restored','#backups/restore',{items:[backup({id:'confirmed'})]});await h.flush();
 let answer;h.window.LiteCPDialogs.confirm=()=>new Promise(resolve=>{answer=resolve;});
 const pending=h.clickDelete(0);h.setItems([backup({id:'new-row'})]);await h.reload();answer(true);await pending;
 assert.equal(h.calls.find(call=>call.options?.method==='DELETE').path,'/api/backups/item?id=confirmed&remote=true');
});

test('pending deletion confirmation blocks duplicate deletes and restores',async()=>{
 const h=setup(true,'restored','#backups/restore',{items:[backup(),backup({id:'second'})]});await h.flush();
 let answer,confirmations=0;h.window.LiteCPDialogs.confirm=()=>{confirmations++;return new Promise(resolve=>{answer=resolve;});};
 const pending=h.clickDelete(0);await h.clickDelete(1);await h.clickRestore(1);
 assert.equal(confirmations,1);assert.equal(h.calls.filter(call=>call.options?.method).length,0);
 answer(false);await pending;
});

test('in-flight deletion blocks a second delete and restore',async()=>{
 let release;const h=setup(true,'restored','#backups/restore',{items:[backup(),backup({id:'second'})],deleteBackup:()=>new Promise(resolve=>{release=resolve;})});await h.flush();
 const pending=h.clickDelete(0);await h.flush();await h.clickDelete(1);await h.clickRestore(1);
 assert.equal(h.calls.filter(call=>call.options?.method==='DELETE').length,1);
 assert.equal(h.calls.some(call=>call.path.includes('restore-direct')),false);
 release();await pending;
});

test('remote deletion failure keeps the row and reports an error without success',async()=>{
 const h=setup(true,'restored','#backups/restore',{items:[backup()],deleteBackup:async()=>{throw Error('SFTP host key differs from the saved fingerprint');}});await h.flush();await h.clickDelete(0);
 assert.equal(h.toasts.some(toast=>toast[2]==='success'),false);
 assert.match(h.errors[0],/host key/);
 assert.match(h.elements[0].querySelector('[data-restore-status]').textContent,/host key/);
 assert.match(h.elements[0].querySelector('[data-restore-list]').innerHTML,/example.com-backup.tar.gz/);
});


test('an older inventory response cannot resurrect a successfully deleted backup',async()=>{
 let loads=0,releaseOld;
 const h=setup(true,'restored','#backups/restore',{loadInventory:()=>{
  loads++;
  if(loads===1)return {items:[backup()]};
  if(loads===2)return new Promise(resolve=>{releaseOld=resolve;});
  return {items:[]};
 }});await h.flush();await h.reload();
 const pending=h.clickDelete(0);await h.flush();
 releaseOld({items:[backup()]});await pending;await h.flush();
 assert.equal(loads,3);
 assert.match(h.elements[0].querySelector('[data-restore-list]').innerHTML,/No available/);
 assert.doesNotMatch(h.elements[0].querySelector('[data-restore-list]').innerHTML,/example.com-backup.tar.gz/);
});

test('panel-state refresh failure does not misreport a completed deletion or prevent inventory refresh',async()=>{
 const h=setup(true,'restored','#backups/restore',{items:[backup()]});await h.flush();
 h.window.LiteCP.refresh=async()=>{throw Error('Panel state refresh interrupted');};
 await h.clickDelete(0);
 assert.equal(h.calls.filter(call=>call.options?.method==='DELETE').length,1);
 assert.equal(h.toasts[0][0],'Backup deleted');assert.equal(h.toasts[0][2],'success');
 assert.match(h.errors[0],/refresh interrupted/);
 assert.match(h.elements[0].querySelector('[data-restore-list]').innerHTML,/No available/);
});
