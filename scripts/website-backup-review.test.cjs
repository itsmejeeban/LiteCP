const test=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
test('both restore selectors default to the current website',()=>{
 const source=fs.readFileSync(require('node:path').join(__dirname,'../web/website-backups.js'),'utf8');
 const start=source.indexOf(' function review('),end=source.indexOf(' async function busyAction',start);
 const section={innerHTML:'',classList:{remove(){}}};
 const review=vm.runInNewContext(source.slice(start,end)+';review',{siteID:'selected',preview:null,esc:x=>String(x),cp:{state:()=>({sites:[{id:'other',domain:'other.test'},{id:'selected',domain:'selected.test'}],databases:[]}),formatBytes:()=>''},dialog:{querySelector:()=>section}});
 review({files:1,bytes:10,databases:[],roots:['home']});
 const selectors=Array.from(section.innerHTML.matchAll(/<select name="targetSiteID"[^>]*>(.*?)<\/select>/g),m=>m[1]);
 assert.equal(selectors.length,2);
 for(const options of selectors){assert.match(options,/<option value="selected" selected>/);assert.doesNotMatch(options,/<option value="other" selected>/);}
});
