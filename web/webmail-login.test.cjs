const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync(require('node:path').join(__dirname, 'features.js'), 'utf8');
const start = source.indexOf('  $(\'[data-page-view="email"]\').addEventListener("click",async event=>{');
const end = source.indexOf('  $("#mailboxForm")', start);
function harness(result, blocked=false) {
  let handler, submitted=false, failed=false;
  const body={textContent:'',append(form){this.form=form;}};
  const tab={closed:false,opener:{},document:{body,createElement(tag){return tag==='form'?{append(input){this.input=input;},submit(){submitted=true;}}:{};}},close(){this.closed=true;}};
  const context={$:()=>({addEventListener(_,fn){handler=fn;}}),window:{open:()=>blocked?null:tab},cp:{api:async()=>result,handleError(){failed=true;},showToast(){failed=true;}},URL,Date,Error};
  vm.runInNewContext(source.slice(start,end),context);
  const button={dataset:{webmailLogin:'mailbox'},disabled:false};
  return {tab,body,button,run:()=>handler({target:{closest:()=>button}}),submitted:()=>submitted,failed:()=>failed};
}
test('mailbox ticket submits in the opened tab without credentials in URL',async()=>{
 const h=harness({url:'https://mail.example.test:8444/',ticket:'one-use-ticket'});await h.run();
 assert.equal(h.submitted(),true);assert.equal(h.body.form.target,'_self');assert.equal(h.body.form.input.name,'litecp_ticket');assert.equal(h.body.form.input.value,'one-use-ticket');assert.equal(h.tab.opener,null);assert.equal(h.button.disabled,false);
});
test('unsafe webmail URL closes tab and reports failure',async()=>{
 const h=harness({url:'http://mail.example.test/',ticket:'ticket'});await h.run();assert.equal(h.submitted(),false);assert.equal(h.tab.closed,true);assert.equal(h.failed(),true);
});
test('blocked tab reports failure without starting login',async()=>{
 const h=harness({},true);await h.run();assert.equal(h.submitted(),false);assert.equal(h.failed(),true);
});
