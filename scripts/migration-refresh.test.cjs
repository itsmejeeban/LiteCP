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
  assert.equal(context.jobs[0].id,'new');assert.equal(shown.length,1);
  const failed=context.load(),latest=context.load();
  pending[3].resolve({items:[{id:'latest',status:'restored'}]});await latest;
  pending[2].reject(Error('obsolete error'));await failed;
  assert.equal(context.jobs[0].id,'latest');assert.equal(output.textContent,undefined);
});
