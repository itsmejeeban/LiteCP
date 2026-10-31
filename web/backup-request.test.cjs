const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const text=fs.readFileSync(__dirname+'/app.js','utf8');const source=text.slice(text.indexOf('  async function runBackup('),text.indexOf('  function configureBackupProtocol('));
test('accepted backup opens history and displays queued request before refreshing',async()=>{
 const events=[],button={};let resolveState;
 const state={sites:[{id:'site',domain:'example.test'}],backups:[]};
 const context={currentState:state,$:s=>s==='#backupSite'?{value:'site'}:s==='#backupDestination'?{value:''}:button,showToast(){},renderAll(){events.push('render')},navigate(route){events.push(route)},handleError(e){throw e},api:async path=>path==='/api/backups'?{id:'job',status:'queued'}:new Promise(resolve=>{resolveState=resolve})};
 vm.createContext(context);vm.runInContext(source,context);const result=context.runBackup({preventDefault(){},currentTarget:{hasAttribute:()=>false}});await new Promise(resolve=>setImmediate(resolve));
 assert.ok(events.includes('backups/history'));assert.equal(state.backups[0].status,'queued');resolveState(state);await result;assert.equal(button.disabled,false);
});
