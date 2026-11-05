const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync(require('node:path').join(__dirname,'../web/live-migration.js'),'utf8');
const code=source.slice(source.indexOf('  let jobsRequest=0;'),source.indexOf('  async function revealMigrationJob('));
test('older migration refresh cannot replace newer jobs or errors',async()=>{
  const pending=[],shown=[],output={};
  const context=vm.createContext({cp:{demo:false,api:()=>new Promise((resolve,reject)=>pending.push({resolve,reject}))},$:()=>output,active:()=>false,renderJobs:()=>shown.push(context.jobs),clearTimeout(){},setTimeout(){},pollTimer:null,jobs:[]});
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
