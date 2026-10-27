const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('web/panel-search.js','utf8');
const tick=()=>new Promise(resolve=>setImmediate(resolve));
function panel(options={}) {
  const nodes=new Map(),listeners={},navigations=[],timers=[],eventTypes=[];
  class Element {
    constructor(tag='div'){this.tag=tag;this.children=[];this.handlers={};this.attributes={};this.value='';this.textContent='';this.dataset={};this.hidden=false;this.isConnected=true;this.classList={add(){},remove(){}};}
    querySelector(selector){return node(selector);}
    querySelectorAll(){return [];}
    setAttribute(key,value){this.attributes[key]=value;}
    removeAttribute(key){delete this.attributes[key];}
    addEventListener(name,callback){this.handlers[name]=callback;}
    append(...children){this.children.push(...children);}
    before(element){this.insertedBefore=element;}
    replaceChildren(){this.children=[];}
    scrollIntoView(){this.scrolled=true;}
    focus(){document.activeElement=this;this.handlers.focus?.();}
    blur(){document.activeElement=null;}
    select(){this.selected=true;}
    closest(){return null;}
    contains(value){return value===this||[...nodes.values()].includes(value);}
    matches(selector){return ['input','select','textarea','button'].includes(this.tag)&&selector.includes(this.tag);}
    dispatchEvent(event){eventTypes.push(event.type);this.handlers[event.type]?.(event);}
  }
  function node(key){if(!nodes.has(key))nodes.set(key,new Element(key==='input'?'input':'div'));return nodes.get(key);}
  const field=new Element('input');field.value='MUST_NOT_INDEX_PRIVATE_VALUE';
  const heading=new Element('span');heading.textContent='Custom safe setting';heading.closest=selector=>selector==='label'?{querySelector:()=>field}:null;
  const page=new Element();page.dataset.pageView='settings';page.querySelector=()=>({textContent:'Settings'});page.querySelectorAll=()=>[heading];
  const document={activeElement:null,createElement:tag=>new Element(tag),querySelector:selector=>selector.startsWith('dialog[')?null:node(selector),querySelectorAll:selector=>selector==='[data-page-view]'?[page]:[],addEventListener:(name,callback)=>listeners[name]=callback};
  const state=options.state===undefined?{sites:[{domain:'my.example.test'}],databases:[{name:'customer_database',username:'db_user'}],mailboxes:[{address:'admin@example.test'}]}:options.state;
  const window={LiteCP:{state:()=>state,navigate:page=>navigations.push(page)},addEventListener(){}};
  vm.runInNewContext(source,{window,document,fetch:options.fetch||(async()=>({ok:false})),Event:class{constructor(type){this.type=type;}},setTimeout:callback=>{timers.push(callback);return timers.length;},clearTimeout(){}});
  const input=node('input'),dropdown=node('.panel-search-dropdown'),list=node('#panelSearchResults');
  return {input,dropdown,list,node,field,listeners,navigations,eventTypes,query(value){input.value=value;input.handlers.input();},key(key,extra={}){let prevented=false;input.handlers.keydown({key,preventDefault(){prevented=true;},stopPropagation(){},...extra});return prevented;},click(title){const button=list.children.find(button=>button.children[0].textContent===title);assert.ok(button,'Missing search result '+title);button.handlers.click();},titles:()=>list.children.map(button=>button.children[0].textContent)};
}
test('global search finds feature aliases and individual settings without indexing values',()=>{
  const ui=panel();
  for(const [query,title] of [['SPF','Email DNS records'],['add cron','Website cron jobs'],['NodeJS','Node.js hosting'],['time zone','Display timezone'],['phpmyadmin','phpMyAdmin'],['root password','Live cPanel / Plesk migration']]){ui.query(query);assert.ok(ui.titles().includes(title),query);}
  ui.query('custom safe');assert.ok(ui.titles().includes('Custom safe setting'));
  ui.query('MUST_NOT_INDEX_PRIVATE_VALUE');assert.equal(ui.list.children.length,0);
});
test('resource searches open the correct page and website result applies its domain filter',()=>{
  const ui=panel();ui.query('MY.EXAMPLE.TEST');ui.click('my.example.test');
  assert.deepEqual(ui.navigations,['websites']);assert.equal(ui.node('#siteSearch').value,'my.example.test');assert.deepEqual(ui.eventTypes,['input']);
  ui.query('customer_database');ui.click('customer_database');assert.equal(ui.navigations.at(-1),'databases');
  ui.query('admin@example.test');ui.click('admin@example.test');assert.equal(ui.navigations.at(-1),'email');
});
test('keyboard selection, Enter, Escape and Ctrl+K work without invoking settings actions',()=>{
  const ui=panel();ui.query('hosting');assert.equal(ui.input.attributes['aria-expanded'],'true');
  const next=ui.list.children[1].children[0].textContent;ui.key('ArrowDown');assert.equal(ui.list.children[1].attributes['aria-selected'],'true');ui.key('Enter');assert.equal(ui.navigations.length,1);assert.equal(ui.dropdown.hidden,true);
  ui.query('dns');ui.key('Escape');assert.equal(ui.dropdown.hidden,true);assert.equal(ui.input.attributes['aria-activedescendant'],undefined);
  let prevented=false;ui.listeners.keydown({key:'k',ctrlKey:true,preventDefault(){prevented=true;}});assert.equal(prevented,true);assert.equal(ui.input.selected,true);assert.equal(ui.navigations.length,1);
  assert.ok(next);
});
test('manual topics augment search safely and target the user manual filter',async()=>{
  const ui=panel({fetch:async()=>({ok:true,json:async()=>[{title:'Test help',topics:[{title:'Special feature guide',steps:['Specific instructions']}]}]})});
  ui.input.focus();await tick();ui.query('special feature');ui.click('Special feature guide');
  assert.equal(ui.navigations.at(-1),'documentation');assert.equal(ui.node('#manualSearch').value,'Special feature guide');
});
test('manual failure leaves local feature search working; signed-out search stays closed',async()=>{
  const ui=panel({fetch:async()=>{throw Error('Unavailable');}});ui.input.focus();await tick();ui.query('resolver');assert.ok(ui.titles().includes('DNS resolvers'));
  const signedOut=panel({state:null});signedOut.query('password');assert.equal(signedOut.dropdown.hidden,true);assert.equal(signedOut.list.children.length,0);
});
test('result labels use text nodes rather than interpreting domain markup',()=>{
  const ui=panel({state:{sites:[{domain:'<img onerror=attack>'}]}});ui.query('onerror');assert.equal(ui.list.children.length,1);assert.equal(ui.list.children[0].children[0].textContent,'<img onerror=attack>');assert.equal(ui.list.children[0].innerHTML,undefined);
  ui.query('timezone');const result=ui.list.children[0];assert.equal(result.children[0].textContent,'Display timezone');assert.equal(result.innerHTML,undefined);
});
