const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('web/theme.js','utf8');
function setup(saved,blocked=false){
  const memory=new Map(saved?[['litecp.uiTheme',saved]]:[]),events={},windowEvents={};
  function node(){return {children:[],listeners:{},style:{setProperty(){}},append(...items){this.children.push(...items);},setAttribute(){},addEventListener(name,fn){this.listeners[name]=fn;}};}
  const select=node(),cards=node(),status=node(),cancel=node(),form=node();
  const fields={'#appearanceTheme':select,'#appearancePresets':cards,'#appearanceStatus':status,'#appearanceCancel':cancel};
  form.querySelector=key=>fields[key];
  const document={documentElement:{dataset:{}},createElement:node,querySelector:key=>key==='#appearanceForm'?form:fields[key],querySelectorAll:()=>cards.children.map(label=>label.children[0]),addEventListener:(name,fn)=>events[name]=fn};
  const window={addEventListener:(name,fn)=>windowEvents[name]=fn};
  vm.runInNewContext(source,{document,window,localStorage:{getItem:key=>{if(blocked)throw Error('blocked');return memory.get(key);},setItem:(key,value)=>{if(blocked)throw Error('blocked');memory.set(key,value);}}});
  events.DOMContentLoaded();
  return {document,window,select,cards,status,cancel,form,memory,windowEvents};
}
test('all ten themes initialize and preview without changing the saved theme',()=>{
  const app=setup();
  assert.equal(app.select.children.length,10);
  assert.equal(app.cards.children.length,10);
  assert.equal(app.document.documentElement.dataset.uiTheme,'default');
  for(const theme of app.window.LiteCPTheme.themes){
    app.select.value=theme.id;app.select.listeners.change();
    assert.equal(app.document.documentElement.dataset.uiTheme,theme.id);
    assert.equal(app.document.documentElement.dataset.theme,'dark');
    assert.equal(app.memory.size,0);
  }
  app.cancel.listeners.click();
  assert.equal(app.document.documentElement.dataset.uiTheme,'default');
});
test('save persists selection and cancel returns to the last saved theme',()=>{
  const app=setup('mui');assert.equal(app.select.value,'mui');
  app.select.value='chakra';app.select.listeners.change();
  app.form.listeners.submit({preventDefault(){}});
  assert.equal(app.memory.get('litecp.uiTheme'),'chakra');
  app.select.value='heroui';app.select.listeners.change();app.cancel.listeners.click();
  assert.equal(app.select.value,'chakra');
  assert.equal(setup(app.memory.get('litecp.uiTheme')).select.value,'chakra');
});
test('invalid preferences and unavailable storage fall back safely',()=>{
  assert.equal(setup('unknown').select.value,'default');
  const app=setup(null,true);app.select.value='mantine';app.select.listeners.change();
  app.form.listeners.submit({preventDefault(){}});
  assert.match(app.status.textContent,/could not be saved/);
  assert.equal(app.window.LiteCPTheme.apply('untrusted'),'default');
});
test('changes in another tab synchronize the selected theme',()=>{
  const app=setup();app.memory.set('litecp.uiTheme','fluent');
  app.windowEvents.storage({key:'litecp.uiTheme'});
  assert.equal(app.select.value,'fluent');
  app.memory.clear();app.windowEvents.storage({key:null});
  assert.equal(app.select.value,'default');
});
test('every preset has styles and appearance has a routable settings entry',()=>{
  const css=fs.readFileSync('web/appearance.css','utf8');
  for(const theme of setup().window.LiteCPTheme.themes){if(theme.id!=='default')assert.ok(css.includes(`[data-ui-theme="${theme.id}"]`));}
  assert.match(fs.readFileSync('web/settings-tabs.js','utf8'),/id:"appearance"/);
  assert.match(fs.readFileSync('web/app.js','utf8'),/"developer","appearance"/);
  assert.match(fs.readFileSync('web/index.html','utf8'),/appearance\.css\?v=4\.0\.61/);
});
