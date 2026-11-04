const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const tick=()=>new Promise(resolve=>setImmediate(resolve));
function setup(){
 const document={activeElement:null};
 class Element{
  constructor(){this.children=new Map();this.listeners={};this.attributes={};this.value='';this.isConnected=true;this.disabled=false;this.hidden=false;this.textContent='';this.innerHTML='';}
  querySelector(selector){if(!this.children.has(selector))this.children.set(selector,new Element());const element=this.children.get(selector);if(selector==='form')element.checkValidity=()=>{const input=this.querySelector('[data-dialog-input]');input.validationMessage=input.error||(input.required&&!input.value?'Enter a value.':input.type==='email'&&input.value&&!/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(input.value)?'Enter a valid email address.':'');return !input.validationMessage;};return element;}
  setAttribute(key,value){this.attributes[key]=value;}removeAttribute(key){delete this.attributes[key];}
  setCustomValidity(value){this.error=value;}
  addEventListener(key,fn){(this.listeners[key]??=[]).push(fn);}
  dispatch(key){const event={preventDefault(){this.prevented=true;},stopPropagation(){this.stopped=true;}};(this.listeners[key]||[]).forEach(fn=>fn(event));return event;}
  focus(){document.activeElement=this;}
  showModal(){this.open=true;}close(){this.open=false;this.dispatch('close');}remove(){this.isConnected=false;}
 }
 const dialogs=[];document.createElement=()=>{const dialog=new Element();dialogs.push(dialog);return dialog;};document.body={append(){}};
 const original=new Element();document.activeElement=original;
 const events=new Map();
 const window={addEventListener(name,fn){if(!events.has(name))events.set(name,new Set());events.get(name).add(fn);},removeEventListener(name,fn){events.get(name)?.delete(fn);}};
 vm.runInNewContext(fs.readFileSync('web/dialogs.js','utf8'),{window,document});
 return {api:window.LiteCPDialogs,document,original,dialogs,events,navigate:()=>[...(events.get('hashchange')||[])].forEach(fn=>fn()),current:()=>dialogs.find(dialog=>dialog.open)};
}

test('navigation dismisses only an opted-in informational prompt and removes its listener',async()=>{
 const p=setup(),result=p.api.confirm('Configure webmail first',{dismissOnNavigation:true});
 p.navigate();assert.equal(await result,false);assert.equal(p.current(),undefined);assert.equal(p.events.get('hashchange').size,0);
 const destructive=p.api.confirm('Delete database?');p.navigate();assert.ok(p.current());
 p.current().querySelector('[data-dialog-cancel]').dispatch('click');assert.equal(await destructive,false);
});
test('destructive confirmation focuses Cancel and Escape cancels without accepting',async()=>{
 const p=setup(),result=p.api.confirm('Delete the selected database?'),dialog=p.current();
 assert.equal(dialog.className,'ui-dialog ui-dialog-danger');assert.equal(p.document.activeElement,dialog.querySelector('[data-dialog-cancel]'));
 assert.equal(dialog.querySelector('[data-dialog-submit]').textContent,'Delete permanently');
 const event=dialog.dispatch('cancel');assert.equal(await result,false);assert.equal(event.prevented,true);assert.equal(event.stopped,true);assert.equal(p.document.activeElement,p.original);
});
test('email prompt validates inline and returns the entered value only after submission',async()=>{
 const p=setup(),result=p.api.prompt('SSL renewal notices','',{title:'Enable SSL certificate',label:'Renewal email',type:'email',required:true,confirmText:'Enable SSL'}),dialog=p.current(),input=dialog.querySelector('[data-dialog-input]');
 let done=false;result.then(()=>done=true);input.value='invalid';dialog.querySelector('form').dispatch('submit');await tick();
 assert.equal(done,false);assert.equal(dialog.querySelector('[data-dialog-error]').hidden,false);assert.equal(input.attributes['aria-invalid'],'true');
 input.value='admin@example.test';input.dispatch('input');dialog.querySelector('form').dispatch('submit');assert.equal(await result,'admin@example.test');
});
test('blank optional input is distinct from Cancel',async()=>{
 const p=setup(),blank=p.api.prompt('Destination folder','');p.current().querySelector('form').dispatch('submit');assert.equal(await blank,'');
 const cancelled=p.api.prompt('Restore path','original');p.current().querySelector('[data-dialog-close]').dispatch('click');assert.equal(await cancelled,null);
});
test('queued requests do not replace an active dialog or accept each other',async()=>{
 const p=setup(),one=p.api.confirm('Restart services?'),two=p.api.confirm('Delete archive?');assert.equal(p.dialogs.length,1);
 p.current().querySelector('[data-dialog-cancel]').dispatch('click');assert.equal(await one,false);assert.equal(p.dialogs.length,2);
 assert.equal(p.current().querySelector('[data-dialog-description]').textContent,'Delete archive?');p.current().querySelector('form').dispatch('submit');assert.equal(await two,true);
});
test('typed reboot confirmation requires an exact hostname and displays message as text',async()=>{
 const p=setup(),message='<img src=x onerror=alert(1)>',result=p.api.prompt(message,'',{match:'test-server',required:true}),dialog=p.current(),input=dialog.querySelector('[data-dialog-input]');
 assert.equal(dialog.querySelector('[data-dialog-description]').textContent,message);assert.equal(dialog.innerHTML.includes(message),false);
 input.value='wrong';dialog.querySelector('form').dispatch('submit');assert.equal(dialog.open,true);assert.match(dialog.querySelector('[data-dialog-error]').textContent,/test-server/);
 input.value='test-server';input.dispatch('input');dialog.querySelector('form').dispatch('submit');assert.equal(await result,'test-server');
});
test('first-party panel scripts do not invoke native browser prompts or confirmations',()=>{
 for(const name of fs.readdirSync('web').filter(name=>name.endsWith('.js'))){const source=fs.readFileSync('web/'+name,'utf8');assert.doesNotMatch(source,/(?<![.\w])(?:confirm|prompt|alert)\s*\(/,name);}
});
