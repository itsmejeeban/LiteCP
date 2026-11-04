const test=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const source=fs.readFileSync(require('node:path').join(__dirname,'../web/app.js'),'utf8');

test('creating a database refreshes sizes after showing one-time credentials',async()=>{
  const code=source.slice(source.indexOf('  async function createDatabase('),source.indexOf('  async function handleDatabaseAction('));
  const calls=[],button={disabled:false};
  const form={querySelector:()=>null,reset(){},dispatchEvent(){}};
  const sandbox={databaseSiteID:'site',FormData:class{get(name){return {name:'db',username:'user',password:'test-only'}[name];}},Event:class{},$:()=>button,api:async url=>url==='/api/state'?{}:{host:'localhost',database:{name:'db',username:'user'},password:'test-only'},renderAll(){},showOneTimeCredentials(){calls.push('credentials');},loadDatabaseSizes(force){calls.push(force);},handleError(error){throw error;}};
  const create=vm.runInNewContext(code+';createDatabase',sandbox);
  await create({preventDefault(){},currentTarget:form});
  assert.deepEqual(calls,['credentials',true]);assert.equal(button.disabled,false);
});

test('size refresh requested during an old query runs again for the newly created database',async()=>{
  const code=source.slice(source.indexOf('  let databaseSizes ='),source.indexOf('  function renderDatabases()'));
  const pending=[];
  const load=vm.runInNewContext(code+';loadDatabaseSizes',{api:()=>new Promise(resolve=>pending.push(resolve)),renderDatabases(){},showToast(){}});
  const first=load();await load(true);assert.equal(pending.length,1);
  pending[0]({sizes:{old:1}});await first;assert.equal(pending.length,2);
  pending[1]({sizes:{old:1,new:0}});await new Promise(resolve=>setImmediate(resolve));
});
