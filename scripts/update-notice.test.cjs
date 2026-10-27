const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const code=fs.readFileSync('web/update-notice.js','utf8');
async function render(result,ok=true){
 const label={textContent:''},notice={hidden:true,querySelector:()=>label,classList:{toggle(_name,hidden){notice.hidden=hidden;}}};
 const elements={'#updateNotice':notice,'#appView':{classList:{contains:()=>false}},'#refreshButton':{addEventListener(){}}};
 vm.runInNewContext(code,{document:{querySelector:s=>elements[s]},MutationObserver:class{observe(){}},fetch:async()=>({ok,json:async()=>result}),Date,setInterval(){}});
 await new Promise(resolve=>setImmediate(resolve));return {notice,label};
}
test('new stable release displays a warning with the exact version and safe release link',async()=>{
 const {notice,label}=await render({checked:true,available:true,latest:'v0.5.2'});
 assert.equal(notice.hidden,false);assert.equal(label.textContent,'LiteCP 0.5.2 version available for update');assert.equal(notice.href,'https://github.com/itsmejeeban/LiteCP/releases/tag/v0.5.2');
});
test('current version, unavailable checks and malformed versions do not show a warning',async()=>{
 for(const result of [{checked:true,available:false,latest:'v0.5.1'},{checked:false,available:true,latest:'v0.5.2'},{checked:true,available:true,latest:'<script>'}])assert.equal((await render(result)).notice.hidden,true);
 assert.equal((await render({},false)).notice.hidden,true);
});
