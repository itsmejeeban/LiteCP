const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync(require('node:path').join(__dirname,'../web/live-migration.js'),'utf8');
const code=source.slice(source.indexOf('  let jobsRequest=0;'),source.indexOf('  async function revealMigrationJob('));
test('source connection pages render discovered account pickers open',()=>{
  const nodes=new Map(),node=key=>{if(!nodes.has(key))nodes.set(key,{innerHTML:'',hidden:false,disabled:false});return nodes.get(key);};
  const context=vm.createContext({location:{hash:''},page:{querySelector:()=>node('heading')},$:node,document:{querySelectorAll:()=>[]},cp:{formatBytes:String},esc:String});
  const start=source.indexOf('  let jobs=[]');
  vm.runInContext(source.slice(start,source.indexOf('  let jobsRequest=0;'))+';this.render=renderJobs;this.setJobs=value=>{jobs=value;};',context);
  for(const platform of ['cpanel','plesk','litecp']){
    context.location.hash='#migrations/'+platform;
    context.setJobs([{id:'connection',status:'connected',source:{platform,host:'source.test'},credentialsRetained:true,accounts:[{id:'demo',domain:'demo.test'}]}]);
    context.render();
    const html=node('#liveJobs').innerHTML;
    assert.match(html,/<div class="live-job-details" id="migration-details-connection"\s*>/);
    assert.match(html,/data-account="demo"/);
  }
});
test('older migration refresh cannot replace newer jobs or errors',async()=>{
  const pending=[],shown=[],output={};
  const context=vm.createContext({document:{hidden:false},location:{hash:'#migrations/jobs'},cp:{demo:false,api:()=>new Promise((resolve,reject)=>pending.push({resolve,reject}))},$:()=>output,active:()=>false,renderJobs:()=>shown.push(context.jobs),clearTimeout(){},setTimeout(){},pollTimer:null,jobs:[]});
  vm.runInContext(code+';this.load=loadJobs;',context);
  const old=context.load(),current=context.load();
  pending[1].resolve({items:[{id:'new',status:'restored'}]});await current;
  pending[0].resolve({items:[{id:'old',status:'restored'}]});await old;
  assert.equal(context.jobs[0].id,'new');assert.equal(shown.length,3);
  assert.equal(shown[0].length,0);assert.equal(shown[1].length,0);
  const failed=context.load(),latest=context.load();
  pending[3].resolve({items:[{id:'latest',status:'restored'}]});await latest;
  pending[2].reject(Error('obsolete error'));await failed;
  assert.equal(context.jobs[0].id,'latest');assert.equal(output.textContent,undefined);
});

test('staged archive listing reports malformed responses and permits retry',async()=>{
  const featureSource=fs.readFileSync(require('node:path').join(__dirname,'../web/features.js'),'utf8');
  const start=featureSource.indexOf('  $("#migrationStagesRefresh").addEventListener');
  const code=featureSource.slice(start,featureSource.indexOf('  function fillSiteSelect',start));
  const button={disabled:false,addEventListener(type,fn){this.click=fn;}},container={textContent:'',replaceChildren(){this.textContent='';}},errors=[];
  let response={};
  vm.runInNewContext(code,{$:key=>key==='#migrationStagesRefresh'?button:container,cp:{demo:false,api:async()=>response},notifyError:error=>errors.push(error.message)});
  await button.click();assert.match(container.textContent,/Click Show staged archives to retry/);assert.equal(button.disabled,false);
  response={items:[]};await button.click();assert.equal(container.textContent,'No staged archives.');assert.equal(errors.length,1);
});

test('completed restores with health warnings keep manual checks out of Pending',()=>{
  const nodes=new Map(),node=key=>{if(!nodes.has(key))nodes.set(key,{innerHTML:'',hidden:false,disabled:false});return nodes.get(key);};
  const context=vm.createContext({location:{hash:'#migrations/pending'},page:{querySelector:()=>node('heading')},$:node,document:{querySelectorAll:()=>[]},cp:{formatBytes:String},esc:String});
  const start=source.indexOf('  let jobs=[]');
  vm.runInContext(source.slice(start,source.indexOf('  let jobsRequest=0;'))+`;jobs=[{id:'health',status:'restored-review',stage:'result',createdAt:'2026-11-06T00:00:00Z',source:{platform:'cpanel',host:'source.test'},account:'site',cpanelWebsiteId:'site-id',nativeReady:true}];renderJobs();`,context);
  assert.doesNotMatch(node('#liveJobs').innerHTML,/data-job-id="health"/);
  assert.equal(node('#liveJobsRetryPending').disabled,true);
  context.location.hash='#migrations/completed';
  vm.runInContext('renderJobs()',context);
  const html=node('#liveJobs').innerHTML;
  assert.match(html,/<button[^>]*data-live-auto="health"[^>]*data-live-retry-restore[^>]*>Retry checks<\/button>/);
  // The post-render cleanup intentionally removes all auto buttons without this marker.
});

test('pending retry recaptures incomplete cPanel database exports and hides superseded attempts',()=>{
  const start=source.indexOf('  let jobs=[]');
  const context=vm.createContext({});
  vm.runInContext(source.slice(start,source.indexOf('  let jobsRequest=0;'))+
    ';this.retry=pendingRetryAction;this.replacements=migrationReplacements;',context);
  const job={id:'old',status:'restore-review',createdAt:'2026-11-07T00:00:00Z',
    source:{platform:'cpanel',host:'source.test'},account:'site',credentialsRetained:true,
    nativeReady:true,message:'Automatic restore stopped: configuration .env has no matching database dump.',
    warnings:['Some account databases could not be exported. Check the source export log before restoring.']};
  assert.equal(context.retry(job),'capture');
  assert.equal(context.retry({...job,credentialsRetained:false}),null);
  assert.equal(context.retry({...job,warnings:[],message:'Automatic restore stopped: local database server is unavailable.'}),'restore');
  const replacement={...job,id:'new',createdAt:'2026-11-08T00:00:00Z'};
  assert.equal(context.replacements([job,{...replacement,status:'queued'}]).size,0);
  assert.equal(context.replacements([job,{...replacement,status:'restored'}]).get(job.id).id,'new');
});

test('invalid jobs responses preserve the last successful jobs for refresh retry',async()=>{
  const output={},original=[{id:'kept',status:'restored'}];
  const context=vm.createContext({document:{hidden:false},location:{hash:'#migrations/jobs'},cp:{demo:false,api:async()=>({})},$:()=>output,active:()=>false,renderJobs(){},clearTimeout(){},setTimeout(){},pollTimer:null,jobs:original});
  vm.runInContext(code+';this.load=loadJobs;',context);await context.load();
  assert.equal(context.jobs,original);assert.match(output.textContent,/Refresh jobs to retry/);
});

test('another archive can be inspected without deleting the retained selection',async()=>{
  const featureSource=fs.readFileSync(require('node:path').join(__dirname,'../web/features.js'),'utf8');
  const start=featureSource.indexOf('  $("#migrationUploadForm").addEventListener');
  const handler=featureSource.slice(start,featureSource.indexOf('  $("#migrationImportForm").addEventListener',start));
  const nodes=new Map(),node=key=>{if(!nodes.has(key))nodes.set(key,{addEventListener(type,fn){this.submit=fn;}});return nodes.get(key);};
  let response={id:'new-stage',roots:['public_html'],databases:[],files:1,bytes:4,skipped:0};const calls=[],errors=[];
  const context=vm.createContext({$:node,preview:{id:'retained'},FormData:class{},esc:String,output(){},syncMigrationTargetDatabases(){},notifyError:err=>errors.push(err.message),cp:{demo:false,state:()=>({databases:[]}),formatBytes:String,api:async(url,options)=>{calls.push([url,options.method]);return response;}}});
  context.busy=async(form,action)=>{try{await action();}catch(err){errors.push(err.message);}};
  vm.runInContext(handler,context);await node('#migrationUploadForm').submit({preventDefault(){},currentTarget:{}});await new Promise(resolve=>setImmediate(resolve));
  assert.equal(context.preview.id,'new-stage');assert.deepEqual(calls,[['/api/migrations','POST']]);
  response={};await node('#migrationUploadForm').submit({preventDefault(){},currentTarget:{}});await new Promise(resolve=>setImmediate(resolve));
  assert.equal(context.preview.id,'new-stage');assert.match(errors[0],/previous selection was kept/);
});

test('import confirmation stays bound to the archive and target reviewed by the user',async()=>{
  const featureSource=fs.readFileSync(require('node:path').join(__dirname,'../web/features.js'),'utf8');
  const start=featureSource.indexOf('  $("#migrationImportForm").addEventListener');
  const handler=featureSource.slice(start,featureSource.indexOf('  $("#migrationDiscardButton").addEventListener',start));
  let submit,confirm,captured;
  const form={target:'original-site',elements:{password:{value:'secret'}}};
  const context=vm.createContext({$:()=>({addEventListener(type,fn){submit=fn;}}),preview:{id:'original-archive'},FormData:class{constructor(form){this.target=form.target;}get(key){return key==='siteId'?this.target:'';}},window:{LiteCPDialogs:{confirm:()=>new Promise(resolve=>{confirm=resolve;})}},output(){},cp:{state:()=>({sites:[{id:'original-site',domain:'original.example.test'}]}),api:async(url,options)=>{captured=options.body;return{note:'done'};},refresh:async()=>{}}});
  context.busy=async(form,action)=>action();vm.runInContext(handler,context);
  const pending=submit({preventDefault(){},currentTarget:form});
  context.preview={id:'other-archive'};form.target='other-site';confirm(true);await pending;await new Promise(resolve=>setImmediate(resolve));
  assert.equal(captured.id,'original-archive');assert.equal(captured.siteId,'original-site');
});

test('archive deletion uses the confirmed archive and preserves a newer selection',async()=>{
  const featureSource=fs.readFileSync(require('node:path').join(__dirname,'../web/features.js'),'utf8');
  const start=featureSource.indexOf('  $("#migrationDiscardButton").addEventListener');
  const handler=featureSource.slice(start,featureSource.indexOf('  $("#migrationStagesRefresh").addEventListener',start));
  let click,confirm,deleted;
  const context=vm.createContext({$:()=>({addEventListener(type,fn){click=fn;}}),preview:{id:'confirmed-archive'},encodeURIComponent,window:{LiteCPDialogs:{confirm:()=>new Promise(resolve=>{confirm=resolve;})}},output(){},notifyError:err=>{throw err;},cp:{api:async(url)=>{deleted=url;}}});
  vm.runInContext(handler,context);const pending=click();context.preview={id:'new-selection'};confirm(true);await pending;
  assert.equal(deleted,'/api/migrations/confirmed-archive');assert.equal(context.preview.id,'new-selection');
});

test('live job polling resumes after a transient refresh failure',async()=>{
  const timers=[],output={};let fail=true;
  const requests=[],location={hash:'#migrations/jobs'},document={hidden:false};
  const context=vm.createContext({document,location,cp:{demo:false,api:async(url)=>{requests.push(url);if(fail)throw Error('Temporary connection failure');return{items:[{id:'busy',status:'running',stage:'capture'}]};}},$:()=>output,active:job=>job.status==='running',renderJobs(){},clearTimeout(){},setTimeout(fn,delay){timers.push({fn,delay});return timers.length;},pollTimer:null,jobs:[{id:'busy',status:'running'}]});
  vm.runInContext(code+';this.load=loadJobs;',context);await context.load();
  assert.match(output.textContent,/Temporary connection/);assert.equal(timers.length,1);assert.equal(timers[0].delay,5000);
  fail=false;await timers[0].fn();assert.equal(context.jobs[0].id,'busy');assert.equal(timers.length,2);
  assert.deepEqual(requests,['/api/live-migrations','/api/live-migrations']);
  document.hidden=true;await context.load();assert.equal(timers.length,2);
  document.hidden=false;location.hash='#dashboard';await context.load();assert.equal(timers.length,2);
  context.jobs=[{id:'done',status:'restored'}];fail=true;await context.load();assert.equal(timers.length,2);
});

test('upload status stays on Upload tab until a job explicitly opens staged review',()=>{
  const tabs=fs.readFileSync(require('node:path').join(__dirname,'../web/migration-tabs.js'),'utf8');
  function element(){const classes=new Set();return{classList:{toggle(key,on){if(on)classes.add(key);else classes.delete(key);},add(...keys){keys.forEach(key=>classes.add(key));},remove(...keys){keys.forEach(key=>classes.delete(key));},contains:key=>classes.has(key)},setAttribute(){},removeAttribute(){},querySelector(){return element();}};}
  const entries=['upload','jobs','pending','completed'].map(key=>Object.assign([key,key],{link:element()})),output=element(),events={};
  const context=vm.createContext({entries,output,location:{hash:'#migrations/upload'},page:{dataset:{},querySelectorAll:()=>[],querySelector:()=>element()},toggle:element(),source:element(),upload:element(),importForm:element(),jobs:element(),database:element(),litecp:element(),expand(){},Event:class{},document:{dispatchEvent(){},addEventListener(name,fn){events[name]=fn;}}});
  const start=tabs.indexOf("  let lastRoute='';");vm.runInContext(tabs.slice(start,tabs.indexOf('  const previous=',start)),context);
  vm.runInContext('sync()',context);assert.equal(output.classList.contains('migration-output-hidden'),false);
  for(const key of ['jobs','pending','completed']){context.location.hash='#migrations/'+key;vm.runInContext('sync()',context);assert.equal(output.classList.contains('migration-output-hidden'),true);}
  const review=tabs.slice(tabs.indexOf("  document.addEventListener('migration-show-review'"),tabs.indexOf('const navigate='));vm.runInContext(review,context);
  events['migration-show-review']();assert.equal(output.classList.contains('migration-output-hidden'),false);
});
