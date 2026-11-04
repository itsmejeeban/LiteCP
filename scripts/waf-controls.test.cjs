const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

function fixture(count = 2) {
  const node = () => {
    const children = new Map();
    return {innerHTML:'',textContent:'',value:'',dataset:{},hidden:false,disabled:false,
      classList:{add(){},remove(){},toggle(){}},setAttribute(){},append(){},remove(){},scrollIntoView(){this.scrolled=true},
      querySelector(s){if(!children.has(s))children.set(s,node());return children.get(s)},
      querySelectorAll(){return []}};
  };
  const root=node(), sections=[], links=[], handlers={};root.parentElement=root;
  const document={createElement(tag){const n=node();if(tag==='section')sections.push(n);else if(tag==='a')links.push(n);return n},querySelector(){return root}};
  const sites=Array.from({length:count},(_,i)=>({id:`s${i}`,domain:`website${i}.test`}));
  const policies={server:{mode:'off'},websites:Object.fromEntries(sites.map(s=>[s.id,{mode:'block',managed:true,rules:[],rateLimits:[]}]))};
  const requests=[],confirmations=[];let fail=false;
  const location={hash:''};
  const cp={escapeHTML:s=>String(s??'').replaceAll('"','&quot;'),state:()=>({sites}),async api(path,options){requests.push({path,options});if(fail)throw Error('Validation failed');if(options?.method==='PUT'){for(const id of options.body.websiteIds||[])policies.websites[id].enabled=options.body.enabled;return {ok:true}}if(path.includes('waf-events'))return {page:1,pages:1,total:0,entries:[],note:''};return {policies:structuredClone(policies),engine:'ModSecurity',nginxRunning:true}},navigate(name){location.hash='#'+name;window.LiteCPFeatures.navigate(name)}};
  const window={LiteCP:cp,LiteCPFeatures:{navigate(){}},LiteCPDialogs:{async confirm(message){confirmations.push(message);return true}},addEventListener(name,fn){handlers[name]=fn}};
  const context=vm.createContext({window,document,location,history:{replaceState(){}},structuredClone,Set,Map,console,setTimeout,clearTimeout});
  vm.runInContext(fs.readFileSync(require('node:path').join(__dirname,'../web/waf-policies.js'),'utf8'),context);
  const security=sections[1], editor=sections[0];
  const idle=()=>new Promise(resolve=>setImmediate(resolve));
  const button=attributes=>({closest(){return this},hasAttribute(name){return name in attributes},dataset:attributes.dataset||{}});
  const click=async attributes=>{security.onclick({target:button(attributes)});await idle()};
  const select=id=>security.onchange({target:{hasAttribute:name=>name==='data-switch-select',dataset:{switchSelect:id},checked:true}});
  return {security,editor,policies,requests,confirmations,links,select,click,idle,open:async()=>{cp.navigate('server-waf');await idle()},fail:()=>{fail=true},cp};
}

test('Security uses per-website switches and bulk updates the same policy endpoint',async()=>{
  const f=fixture();await f.open();
  assert.match(f.security.querySelector('[data-switch-rows]').innerHTML,/Disable/);
  f.select('s0');f.select('s1');
  await f.click({'data-switch-bulk':'',dataset:{switchBulk:'false'}});
  const writes=f.requests.filter(r=>r.options?.method==='PUT');
  assert.equal(writes.length,1);
  assert.deepEqual(JSON.parse(JSON.stringify(writes[0].options.body)),{websiteIds:['s0','s1'],enabled:false});
  assert.equal(f.policies.websites.s0.mode,'block');
  assert.equal(f.policies.websites.s0.enabled,false);
  assert.match(f.security.querySelector('[data-switch-message]').textContent,/disabled for 2/);
  assert.match(f.confirmations[0],/Saved rules will be retained/);
});

test('individual enable and failed updates never report success',async()=>{
  const f=fixture();f.policies.websites.s0.enabled=false;await f.open();
  await f.click({'data-switch-id':'',dataset:{switchId:'s0',switchEnabled:'true'}});
  assert.equal(f.policies.websites.s0.enabled,true);
  f.fail();await f.click({'data-switch-id':'',dataset:{switchId:'s0',switchEnabled:'false'}});
  assert.equal(f.policies.websites.s0.enabled,true);
  assert.equal(f.security.querySelector('[data-switch-message]').textContent,'Validation failed');
});

test('pagination and filtering preserve selection, configuration remains website scoped',async()=>{
  const f=fixture(12);await f.open();f.select('s0');
  await f.click({'data-switch-next':''});
  assert.match(f.security.querySelector('[data-switch-count]').textContent,/Page 2 of 2/);
  assert.match(f.security.querySelector('[data-switch-rows]').innerHTML,/website11/);
  const search=f.security.querySelector('[data-switch-search]');search.value='website11';search.oninput();
  assert.match(f.security.querySelector('[data-switch-count]').textContent,/1 websites/);
  assert.equal(f.security.querySelector('[data-switch-selected]').textContent,'1 selected');
  assert.deepEqual(f.links.map(n=>n.dataset.page),['server-waf','website-waf']);
  assert.ok(f.links.every(n=>n.innerHTML.includes('Website WAF')));
  f.cp.navigate('website-waf');await f.idle();
  assert.equal(f.editor.dataset.pageView,'website-waf');
  assert.equal(f.requests.some(r=>r.options?.body?.scope==='server'),false);
});


test('Configure and Check Logs open the selected website without changing protection',async()=>{
  const f=fixture();await f.open();
  const rows=f.security.querySelector('[data-switch-rows]').innerHTML;
  assert.match(rows,/data-switch-configure="s1"/);
  assert.match(rows,/data-switch-logs="s1"/);
  await f.click({'data-switch-configure':'',dataset:{switchConfigure:'s1'}});
  assert.equal(f.editor.querySelector('[data-website]').value,'s1');
  assert.equal(f.editor.scrolled,true);
  assert.ok(f.requests.some(r=>r.path.includes('waf-events?scope=s1')));
  await f.open();
  await f.click({'data-switch-logs':'',dataset:{switchLogs:'s0'}});
  assert.equal(f.editor.querySelector('[data-website]').value,'s0');
  assert.equal(f.editor.querySelector('[data-security-events]').scrolled,true);
  assert.ok(f.requests.some(r=>r.path.includes('waf-events?scope=s0&kind=detected')));
  assert.equal(f.requests.some(r=>r.options?.method==='PUT'),false);
});

test('policy save retains disabled protection status',async()=>{
 const f=fixture();await f.open();await f.click({'data-switch-configure':'',dataset:{switchConfigure:'s0'}});
 // The full fixture checks policy status on a disabled website after saving.
 f.policies.websites.s0.enabled=false;
 await f.open();await f.click({'data-switch-configure':'',dataset:{switchConfigure:'s0'}});
 await f.editor.querySelector('[data-save]').onclick();
 assert.match(f.editor.querySelector('[data-message]').textContent,/Protection is disabled/);
 assert.match(f.editor.querySelector('[data-status]').textContent,/Status: Disabled/);
 assert.equal(f.editor.querySelector('[data-save]').disabled,false);
});
