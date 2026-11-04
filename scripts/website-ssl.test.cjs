const test = require('node:test');
const assert = require('node:assert/strict');
const vm = require('node:vm');
const fs = require('node:fs');
const path = require('node:path');
function panel({prompt = async () => 'qa@example.test', refresh = async () => {}, api = async () => {}} = {}) {
  class Element {
    constructor() { this.children = new Map(); this.dataset = {}; this.classList = {add(){}}; this.value = ''; this.textContent = ''; }
    querySelector(key) { if (!this.children.has(key)) this.children.set(key, new Element()); return this.children.get(key); }
    append() {} setAttribute() {} addEventListener(type, fn) { this[type] = fn; }
  }
  const page = new Element(), root = new Element(); let writes = 0, errors = 0;
  const state = {sites:[{id:'qa',domain:'qa.example.test',ssl:false}]};
  vm.runInNewContext(fs.readFileSync(path.join(__dirname,'../web/website-ssl.js'),'utf8'), {document:{createElement:()=>page,querySelector:()=>root},window:{LiteCP:{escapeHTML:String,state:()=>state,navigate(){},api:async (...args)=>{writes++;return api(...args);},refresh,showToast(){},handleError(){errors++;}},LiteCPFeatures:{render(){}},LiteCPDialogs:{prompt}}});
  return {state, click:()=>page.click({target:{closest:()=>({dataset:{installSsl:'qa'}})}}),status:()=>page.querySelector('[data-ssl-status]').textContent,writes:()=>writes,errors:()=>errors};
}
test('successful SSL issuance is not reported failed when refresh fails',async()=>{
  const ui=panel({refresh:async()=>{throw Error('offline');}});await ui.click();
  assert.equal(ui.writes(),1);assert.equal(ui.errors(),0);assert.match(ui.status(),/SSL installed.*could not refresh/);
});
test('duplicate clicks share only one SSL prompt and request',async()=>{
  let resolve, prompts=0;const ui=panel({prompt:()=>{prompts++;return new Promise(done=>resolve=done);}});
  const first=ui.click();await ui.click();assert.equal(prompts,1);resolve('qa@example.test');await first;assert.equal(ui.writes(),1);
});
test('renamed website cannot be issued from a stale SSL prompt',async()=>{
  let resolve;const ui=panel({prompt:()=>new Promise(done=>resolve=done)});const pending=ui.click();ui.state.sites[0].domain='renamed.example.test';resolve('qa@example.test');await pending;assert.equal(ui.writes(),0);assert.match(ui.status(),/Website changed/);
});
test('canceling SSL prompt allows retry without writing',async()=>{
  const ui=panel({prompt:async()=>null});await ui.click();await ui.click();assert.equal(ui.writes(),0);
});
