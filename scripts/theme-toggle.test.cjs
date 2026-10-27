const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('web/features.js','utf8');
function setup(saved,systemDark){
 const button={attributes:{},listeners:{},setAttribute(key,value){this.attributes[key]=value;},addEventListener(key,fn){this.listeners[key]=fn;},innerHTML:''};
 const stored=new Map(saved?[['litecp-theme',saved]]:[]),media={matches:systemDark,addEventListener(_,fn){this.change=fn;}};
 const editorThemes=[],document={documentElement:{dataset:{}}};
 const scope={document,localStorage:{getItem:key=>stored.get(key),setItem:(key,value)=>stored.set(key,value)},matchMedia:()=>media,editor:{setTheme:value=>editorThemes.push(value)},$:selector=>selector==='.topbar-actions'?{insertAdjacentHTML(){}}:button};
 const start=source.indexOf('  // Persistent, device-local theme selection.'),end=source.indexOf('  $(".activity-panel")',start);
 assert.ok(start>=0&&end>start);vm.runInNewContext(source.slice(start,end),scope);
 return {button,stored,media,document,editorThemes};
}
test('theme icon toggles both modes, remembers choice and follows it after reload',()=>{
 const p=setup('dark',false);assert.equal(p.document.documentElement.dataset.theme,'dark');assert.match(p.button.innerHTML,/<circle/);assert.equal(p.button.attributes['aria-label'],'Switch to light mode');
 p.button.listeners.click();assert.equal(p.stored.get('litecp-theme'),'light');assert.match(p.button.innerHTML,/20.9 13/);assert.equal(p.button.attributes['aria-pressed'],'false');assert.equal(p.editorThemes.at(-1),'ace/theme/tomorrow');
 p.button.listeners.click();assert.equal(p.stored.get('litecp-theme'),'dark');assert.equal(setup(p.stored.get('litecp-theme'),false).document.documentElement.dataset.theme,'dark');
});
test('system changes apply until the user explicitly chooses a mode',()=>{
 const p=setup(null,true);assert.equal(p.document.documentElement.dataset.theme,'dark');assert.equal(p.stored.size,0);
 p.media.matches=false;p.media.change();assert.equal(p.document.documentElement.dataset.theme,'light');
 p.button.listeners.click();p.media.matches=false;p.media.change();assert.equal(p.document.documentElement.dataset.theme,'dark');
});
