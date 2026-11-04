const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

function editor() {
  const source = fs.readFileSync(path.join(__dirname, '../web/website-dns.js'), 'utf8');
  const controls = new Map();
  const $ = selector => {
    if (!controls.has(selector)) controls.set(selector, { disabled: false, innerHTML: '', textContent: '' });
    return controls.get(selector);
  };
  const picker = { value: 'website-id', disabled: false };
  let rows = [];
  const page = { querySelectorAll(selector) {
    return selector === 'button' ? [...controls.values()] : rows;
  } };
  for (const name of ['prev', 'next', 'page-size', 'records', 'page-status']) $(`[data-${name}]`);
  const context = vm.createContext({ $, picker, page, esc: String });
  vm.runInContext(`let records=Array.from({length:23},(_,i)=>({name:'record'+i,type:'A',ttl:3600,value:'192.0.2.1'})),busy=false,recordPage=1,pageSize=10;
    ${source.slice(source.indexOf('  function buttons()'), source.indexOf('  async function load()'))}
    globalThis.editor={render,buttons,read,setPage:n=>recordPage=n,setBusy:v=>busy=v,getRecords:()=>records};`, context);
  return { api: context.editor, $, picker, setRows: value => rows = value };
}

test('DNS pagination keeps boundary buttons disabled after a completed load', () => {
  const {api,$} = editor();
  api.render(); api.buttons();
  assert.equal($('[data-prev]').disabled, true);
  assert.equal($('[data-next]').disabled, false);
  assert.equal($('[data-page-status]').textContent, '1-10 of 23 records | Page 1 of 3');
  api.setPage(3); api.render(); api.buttons();
  assert.equal($('[data-prev]').disabled, false);
  assert.equal($('[data-next]').disabled, true);
  assert.equal($('[data-page-status]').textContent, '21-23 of 23 records | Page 3 of 3');
  assert.match($('[data-records]').innerHTML, /record22/);
  assert.doesNotMatch($('[data-records]').innerHTML, /record0\"/);
});

test('DNS edits on a later page preserve records on other pages', () => {
  const {api,setRows} = editor();
  setRows([{dataset:{index:'20'},querySelectorAll:()=>[
    {dataset:{key:'name'},value:'changed'}, {dataset:{key:'type'},value:'TXT'},
    {dataset:{key:'ttl'},value:'600'}, {dataset:{key:'value'},value:'"kept  spaces"'}
  ]}]);
  api.read();
  assert.equal(api.getRecords().length,23);
  assert.equal(api.getRecords()[0].name,'record0');
  assert.equal(api.getRecords()[20].name,'changed');
  assert.equal(api.getRecords()[20].value,'"kept  spaces"');
});

test('DNS blocks page and website selection during a save', () => {
  const {api,$,picker} = editor();
  api.setBusy(true); api.buttons();
  assert.equal(picker.disabled,true);
  assert.equal($('[data-page-size]').disabled,true);
  assert.equal($('[data-next]').disabled,true);
});

test('switching DNS websites clears import text but refreshing the same website preserves it', async () => {
  const source=fs.readFileSync(path.join(__dirname,'../web/website-dns.js'),'utf8');
  const nodes=new Map(),$=key=>{if(!nodes.has(key))nodes.set(key,{value:'',hidden:false,textContent:''});return nodes.get(key)};
  const picker={value:'one'};
  const context=vm.createContext({$,picker,endpoint:()=>'/test',buttons(){},render(){},cp:{api:async()=>({zone:{records:[]},installed:false})}});
  vm.runInContext(`let generation=0,loadedSite='',records=[],recordPage=1,busy=false;${source.slice(source.indexOf('  async function load()'),source.indexOf('  async function save('))}globalThis.load=load;`,context);
  await context.load();$('[data-import]').value='zone for one';await context.load();assert.equal($('[data-import]').value,'zone for one');
  picker.value='two';await context.load();assert.equal($('[data-import]').value,'');
});
