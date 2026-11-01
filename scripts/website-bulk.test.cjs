const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

function fixture(sites, options = {}) {
  const node = () => ({textContent:'',innerHTML:'',hidden:false,disabled:false,addEventListener(event, fn){this[event] = fn;},setAttribute(){}});
  const toolbar = node(), nodes = new Map();
  for (const selector of ['[data-bulk-count]','[data-bulk-clear]','[data-bulk-progress]','[data-bulk-results]','.website-bulk-results']) nodes.set(selector,node());
  const buttons = ['suspend','unsuspend','ssl','remove'].map(action => ({...node(),dataset:{bulkAction:action}}));
  toolbar.querySelector = selector => nodes.get(selector);
  toolbar.querySelectorAll = () => [...buttons,nodes.get('[data-bulk-clear]')];
  const check = id => ({dataset:{siteSelect:id},checked:false,matches:selector => selector === '[data-site-select]'});
  let visible = sites.map(site => check(site.id));
  const all = {...node(),checked:false,matches:selector => selector === '[data-sites-select-page]'};
  const table = {...node(),before(){},querySelector:() => all,querySelectorAll:() => visible};
  const requests = [], prompts = [], confirmations = [];
  let refreshes = 0, active = 0, maxActive = 0;
  const state = {sites,settings:{email:'owner@example.test'}};
  const cp = {
    state:() => state,
    escapeHTML:value => String(value).replaceAll('&','&amp;').replaceAll('<','&lt;').replaceAll('"','&quot;'),
    api:async (url, args) => {
      requests.push({url,args}); active++; maxActive=Math.max(active,maxActive);
      try { if(options.api) await options.api(url,args); }
      finally {active--;}
    },
    refresh:async () => {refreshes++;},handleError:error => {throw error;}
  };
  const window = {LiteCP:cp,LiteCPDialogs:{
    prompt:async (message,value,args) => {prompts.push({message,value,args});return options.prompt === undefined ? (args.match || value) : options.prompt;},
    confirm:async (message,args) => {confirmations.push({message,args});return options.confirm !== false;}
  }};
  vm.runInNewContext(fs.readFileSync('web/website-bulk.js','utf8'),{window,document:{getElementById:() => table,createElement:() => toolbar},Set,encodeURIComponent});
  return {
    requests,prompts,confirmations,nodes,table,all,
    select(id,checked=true){const input=visible.find(input => input.dataset.siteSelect===id);input.checked=checked;table.change({target:input});},
    selectPage(checked=true){all.checked=checked;table.change({target:all});},
    page(ids){visible=ids.map(check);window.LiteCPWebsiteBulk.render();return visible;},
    run(action){return toolbar.click({target:{closest:() => buttons.find(button => button.dataset.bulkAction===action)}});},
    get refreshes(){return refreshes;},get maxActive(){return maxActive;}
  };
}
const sites = () => [{id:'one',domain:'one.example.test',ssl:true,suspended:false},{id:'two',domain:'two.example.test',ssl:false,suspended:true},{id:'three',domain:'three.example.test',ssl:false,suspended:false}];

test('page selection persists across pagination and filtering without selecting hidden websites', () => {
  const f=fixture(sites());f.page(['one','two']);f.select('one');assert.equal(f.all.indeterminate,true);
  f.page(['three']);f.selectPage();assert.equal(f.nodes.get('[data-bulk-count]').textContent,'2 selected');
  const previous=f.page(['one','two']);assert.equal(previous[0].checked,true);assert.equal(previous[1].checked,false);
  f.selectPage(false);assert.equal(f.nodes.get('[data-bulk-count]').textContent,'1 selected');
});
test('permanent bulk removal requires exact typed confirmation and passes individual domain confirmations', async () => {
  const cancelled=fixture(sites(),{prompt:'wrong'});cancelled.selectPage();await cancelled.run('remove');assert.equal(cancelled.requests.length,0);assert.equal(cancelled.table.inert,false);
  const f=fixture(sites());f.select('one');f.select('three');await f.run('remove');
  assert.equal(f.prompts[0].args.match,'REMOVE 2 WEBSITES');assert.match(f.prompts[0].message,/backups and mailboxes are kept/);
  assert.deepEqual(f.requests.map(request => request.url),['/api/sites/one?deleteData=true','/api/sites/three?deleteData=true']);
  assert.equal(f.requests[0].args.method,'DELETE');assert.equal(f.requests[0].args.body.domain,'one.example.test');assert.equal(f.refreshes,1);
});
test('SSL requests only unprotected websites, validates one email dialog and does not renew existing SSL', async () => {
  const f=fixture(sites());f.selectPage();await f.run('ssl');assert.equal(f.prompts[0].args.type,'email');
  assert.equal(f.requests.length,2);assert.ok(f.requests.every(request => request.url==='/api/sites/ssl' && request.args.body.email==='owner@example.test' && !request.args.body.renew));
  assert.match(f.nodes.get('[data-bulk-progress]').textContent,/2 succeeded, 1 skipped, 0 failed/);
  const cancelled=fixture(sites(),{prompt:null});cancelled.selectPage();await cancelled.run('ssl');assert.equal(cancelled.requests.length,0);
});
test('suspension is idempotent, sequential, reports failures and retains failed selections', async () => {
  const f=fixture(sites(),{api:async (url,args) => {await Promise.resolve();if(args.body.id==='three')throw new Error('<failure>');}});
  f.selectPage();await f.run('suspend');assert.equal(f.requests.length,2);assert.equal(f.maxActive,1);
  assert.ok(f.requests.every(request => request.args.method==='PUT' && request.args.body.suspended===true));
  assert.match(f.nodes.get('[data-bulk-progress]').textContent,/1 succeeded, 1 skipped, 1 failed/);
  assert.equal(f.nodes.get('[data-bulk-count]').textContent,'1 selected');assert.match(f.nodes.get('.website-bulk-results').innerHTML,/&lt;failure>/);
});
test('unsuspend preserves data and cancelling confirmation sends no request', async () => {
  const f=fixture(sites());f.selectPage();await f.run('unsuspend');assert.equal(f.requests.length,1);assert.equal(f.requests[0].args.body.id,'two');assert.equal(f.requests[0].args.body.suspended,false);
  const cancelled=fixture(sites(),{confirm:false});cancelled.selectPage();await cancelled.run('suspend');assert.equal(cancelled.requests.length,0);
});
test('authentication failure stops remaining requests and unlocks the controls', async () => {
  const f=fixture(sites(),{api:async () => {throw Object.assign(new Error('Sign in'),{status:401});}});f.selectPage();await f.run('remove');
  assert.equal(f.requests.length,1);assert.equal(f.table.inert,false);assert.match(f.nodes.get('[data-bulk-progress]').textContent,/3 failed/);
});
test('a running batch rejects duplicate clicks and ignores new selections', async () => {
  let finish;const waiting=new Promise(resolve => finish=resolve);
  const f=fixture(sites(),{api:async () => waiting});f.select('one');const work=f.run('suspend');
  await Promise.resolve();await f.run('suspend');f.select('three');assert.equal(f.requests.length,1);assert.equal(f.table.inert,true);
  finish();await work;assert.equal(f.requests.length,1);assert.equal(f.table.inert,false);
});
