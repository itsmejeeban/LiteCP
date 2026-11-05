const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('web/theme.js','utf8');
function fixture(saved,fail=false){
 const listeners={},events={},attrs={},data=new Map(saved?[['litecp.colorMode',saved]]:[]);
 const button={setAttribute(k,v){attrs[k]=v},addEventListener(k,v){events[k]=v}};
 const document={documentElement:{dataset:{}},querySelector(s){return s==='#colorModeToggle'?button:null},querySelectorAll(){return []},addEventListener(k,v){listeners[k]=v}};
 const window={addEventListener(k,v){listeners[k]=v},dispatchEvent(){}};
 vm.runInNewContext(source,{document,window,Event:class{},localStorage:{getItem(k){if(fail)throw Error();return data.get(k)},setItem(k,v){if(fail)throw Error();data.set(k,v)}}});
 listeners.DOMContentLoaded();return {document,window,events,attrs,data,listeners};
}
test('dark is the default, including unavailable or invalid browser storage',()=>{
 for(const mode of [undefined,'system','invalid'])assert.equal(fixture(mode).document.documentElement.dataset.theme,'dark');
 assert.equal(fixture('light',true).document.documentElement.dataset.theme,'dark');
});
test('icon toggles both modes and persists the choice across reloads and appearance previews',()=>{
 const f=fixture();assert.equal(f.attrs['aria-label'],'Switch to light mode');f.events.click();
 assert.equal(f.document.documentElement.dataset.theme,'light');assert.equal(f.data.get('litecp.colorMode'),'light');assert.equal(f.attrs['aria-label'],'Switch to dark mode');
 f.window.LiteCPTheme.apply('default');assert.equal(f.document.documentElement.dataset.theme,'light');
 assert.equal(fixture('light').document.documentElement.dataset.theme,'light');
 f.events.click();assert.equal(f.document.documentElement.dataset.theme,'dark');
});
test('switch remains usable without storage and editor follows color mode',()=>{
 const f=fixture(null,true);f.events.click();assert.equal(f.document.documentElement.dataset.theme,'light');
 const features=fs.readFileSync('web/features.js','utf8');assert.match(features,/litecp-color-mode/);assert.match(features,/ace\/theme\/chrome/);
 const html=fs.readFileSync('web/index.html','utf8');assert.match(html,/id="colorModeToggle"/);assert.match(fs.readFileSync('web/documentation.js','utf8'),/link.before\(modeToggle\)/);
});
