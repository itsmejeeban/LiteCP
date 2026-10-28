const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
function settingsUI(hash='#dashboard'){
 const nodes=new Map(),events={},oldNavigations=[];const location={hash};
 class Element{
  constructor(){this.children=[];this.attributes={};this.dataset={};this.handlers={};this.classes=new Set();this.classList={add:(...values)=>values.forEach(value=>this.classes.add(value)),remove:value=>this.classes.delete(value),contains:value=>this.classes.has(value),toggle:(value,force)=>{const on=force===undefined?!this.classes.has(value):force;if(on)this.classes.add(value);else this.classes.delete(value);return on;}};}
  set className(value){this.classes=new Set(value.split(' '));}get className(){return [...this.classes].join(' ');}
  append(...children){for(const child of children){if(child.parentElement)child.parentElement.children=child.parentElement.children.filter(value=>value!==child);child.parentElement=this;this.children.push(child);}}
  before(child){if(this.parentElement){const parent=this.parentElement;child.parentElement=parent;parent.children.splice(parent.children.indexOf(this),0,child);}}
  replaceWith(child){const parent=this.parentElement;parent.children.splice(parent.children.indexOf(this),1,child);child.parentElement=parent;}
  after(child){const parent=this.parentElement;if(parent){child.parentElement=parent;parent.children.splice(parent.children.indexOf(this)+1,0,child);}}
  querySelector(selector){if(selector==='.settings-grid')return node('grid');if(selector==='.environment-card')return node('environment');return node(selector);}
  closest(){return this.parentElement;}
  addEventListener(name,callback){this.handlers[name]=callback;}
  setAttribute(name,value){this.attributes[name]=value;}getAttribute(name){return this.attributes[name];}removeAttribute(name){delete this.attributes[name];}
 }
 function node(selector){if(!nodes.has(selector))nodes.set(selector,new Element());return nodes.get(selector);}
 for(const key of ['#passwordForm','#settingsForm','#timezoneForm','#backupSettingsForm','#hostnameForm','#resolversForm','environment'])node('grid').append(node(key));
 node('remotePanel').classList.add('panel');node('remotePanel').append(node('#backupDestinationsTable'));node('backupGrid').append(node('#backupDestinationForm'),node('remotePanel'));
 node('powerPanel').classList.add('panel');node('powerPanel').append(node('#rebootServer'));node('grid').append(node('powerPanel'));
 const parent=node('[data-page="settings"]');node('nav').append(parent);
 const features={navigate:id=>oldNavigations.push(id)},cp={navigate(route){location.hash='#'+route;features.navigate(route.startsWith('settings/')?'settings':route);}};
 vm.runInNewContext(fs.readFileSync('web/settings-tabs.js','utf8'),{document:{querySelector:node,createElement:()=>new Element()},window:{LiteCP:cp,LiteCPFeatures:features,addEventListener:(name,callback)=>events[name]=callback},location});
 const group=node('nav').children[0],toggle=group.children[0],submenu=group.children[1];
 return {node,group,toggle,submenu,location,navigate:cp.navigate,oldNavigations,panels:()=>node('grid').children.filter(child=>child.dataset.settingsPanel),clickToggle:()=>toggle.handlers.click(),visible:()=>node('grid').children.filter(child=>child.dataset.settingsPanel&&!child.classList.contains('hidden')).map(child=>child.dataset.settingsPanel)};
}
test('Settings expands and collapses eight ordered sub-tabs without changing the open page',()=>{
 const ui=settingsUI();assert.deepEqual(ui.submenu.children.map(link=>link.textContent),['Administrator password','Administrator email','Time zone','Server hostname','DNS resolver','Developer mode','Update','Reboot server']);
 assert.equal(ui.toggle.getAttribute('aria-expanded'),'false');ui.clickToggle();assert.equal(ui.toggle.getAttribute('aria-expanded'),'true');assert.equal(ui.submenu.classList.contains('hidden'),false);assert.equal(ui.location.hash,'#dashboard');
 ui.clickToggle();assert.equal(ui.submenu.classList.contains('hidden'),true);assert.equal(ui.location.hash,'#dashboard');
});
test('sub-tabs show only their own controls and deep links select the correct section',()=>{
 const ui=settingsUI('#settings/dns');assert.deepEqual(ui.visible(),['dns']);assert.equal(ui.toggle.getAttribute('aria-expanded'),'true');
 ui.navigate('settings/timezone');assert.deepEqual(ui.visible(),['timezone']);assert.equal(ui.submenu.children[2].getAttribute('aria-current'),'page');
 ui.clickToggle();ui.navigate('settings/timezone');assert.equal(ui.submenu.classList.contains('hidden'),true);
 ui.navigate('settings');assert.deepEqual(ui.visible(),['password']);
});
test('backup configuration stays outside Settings with its original controls intact',()=>{
 const ui=settingsUI('#settings');
 assert.equal(ui.panels().some(value=>value.dataset.settingsPanel==='backups'),false);
 assert.equal(ui.node('#backupDestinationsTable').parentElement,ui.node('remotePanel'));
 assert.equal(ui.node('.backup-management-grid').classList.contains('backup-run-layout'),true);
});
test('saving a settings sub-tab preserves saved values from other sections',async()=>{
 const source=fs.readFileSync('web/app.js','utf8'),start=source.indexOf('  async function saveSettings('),end=source.indexOf('  async function changePassword(',start);
 for(const [part,expected] of [['email',{email:'new@example.test',timezone:'UTC',backupRetention:14}],['timezone',{email:'saved@example.test',timezone:'Asia/Kolkata',backupRetention:14}],['backups',{email:'saved@example.test',timezone:'UTC',backupRetention:30}]]){
  const calls=[],submit={disabled:false},currentState={settings:{email:'saved@example.test',timezone:'UTC',backupRetention:14}},fields={'#settingsEmail':{value:' new@example.test '},'#settingsTimezone':{value:'Asia/Kolkata'},'#settingsRetention':{value:'30'}};
  const save=vm.runInNewContext(source.slice(start,end)+'\nsaveSettings',{currentState,$:(selector,root)=>root?submit:fields[selector],api:async(path,options)=>calls.push({path,body:options.body}),renderAll(){},showToast(){},handleError(error){throw error;}});
  await save({preventDefault(){},currentTarget:{dataset:{settingsPart:part}}});assert.equal(calls[0].path,'/api/settings');assert.deepEqual(JSON.parse(JSON.stringify(calls[0].body)),expected);assert.equal(submit.disabled,false);
 }
});
test('main navigation retains settings sub-routes while activating the Settings page',()=>{
 const source=fs.readFileSync('web/app.js','utf8'),start=source.indexOf('  function navigate('),end=source.indexOf('  function openSiteModal(',start),location={hash:'#dashboard'},active={},calls=[];
 const views=['dashboard','settings'].map(id=>({dataset:{pageView:id},classList:{toggle:(name,value)=>active[id]=value}}));
 const navigate=vm.runInNewContext(source.slice(start,end)+'\nnavigate',{location,history:{pushState(a,b,value){location.hash=value;}},$$:selector=>selector==='[data-page-view]'?views:[],closeSidebar(){},window:{scrollTo(){},LiteCPFeatures:{navigate:page=>calls.push(page)}},clearInterval(){},logTimer:0,loadLogs(){},loadFiles(){}});
 navigate('settings/hostname');assert.equal(location.hash,'#settings/hostname');assert.equal(active.settings,true);assert.equal(active.dashboard,false);assert.deepEqual(calls,['settings']);
});
