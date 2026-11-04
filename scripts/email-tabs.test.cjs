const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('web/email-tabs.js','utf8');
test('mail management and diagnostics stay available without optional webmail',()=>{
 const start=source.indexOf('    const ready='),end=source.indexOf("    if(!active)promptedRoute",start);
 for(const tab of ['create-mailbox','mailboxes','add-domain','create-alias','forward','domains','aliases','delivery','smtp-port-check','configure-webmail']){
  let noticeHidden;
  const context={configured:false,active:true,selected:[tab],cp:{state:()=>({system:{webmailAvailable:false}})},setupNotice:{classList:{toggle:(name,value)=>noticeHidden=value}}};
  vm.runInNewContext(source.slice(start,end)+';globalThis.result=blocked;',context);
  assert.equal(context.result,false,tab);assert.equal(noticeHidden,tab==='configure-webmail',tab);
 }
});
test('webmail readiness hides the setup notice without hiding mail tools',()=>{
 const start=source.indexOf('    const ready='),end=source.indexOf("    if(!active)promptedRoute",start);let hidden;
 const context={configured:true,active:true,selected:['mailboxes'],setupNotice:{classList:{toggle:(_,value)=>hidden=value}}};
 vm.runInNewContext(source.slice(start,end)+';globalThis.result=blocked;',context);assert.equal(hidden,true);assert.equal(context.result,false);
});
