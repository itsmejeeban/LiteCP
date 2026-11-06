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
test('new stable release displays a warning with the exact version and Settings Update link',async()=>{
 const {notice,label}=await render({checked:true,available:true,latest:'v0.5.2'});
 assert.equal(notice.hidden,false);assert.equal(label.textContent,'LiteCP 0.5.2 version available for update');assert.equal(notice.href,'#settings/update');
});
test('current version, unavailable checks and malformed versions do not show a warning',async()=>{
 for(const result of [{checked:true,available:false,latest:'v0.5.1'},{checked:false,available:true,latest:'v0.5.2'},{checked:true,available:true,latest:'<script>'}])assert.equal((await render(result)).notice.hidden,true);
 assert.equal((await render({},false)).notice.hidden,true);
});
test('update page displays the installed build revision',async()=>{
 const versions={textContent:''};
 const notice={hidden:true,querySelector:()=>({textContent:''}),classList:{toggle(){}}};
 const app={classList:{contains:()=>false}};
 const elements={'#updateNotice':notice,'#appView':app,'#panelUpdateVersions':versions};
 vm.runInNewContext(code,{document:{querySelector:s=>elements[s]},MutationObserver:class{observe(){}},fetch:async()=>({ok:true,json:async()=>({current:'0.5.1',build:'c432478abcdef123',checked:true,available:false})}),Date,setInterval(){}});
 await new Promise(resolve=>setImmediate(resolve));
 assert.equal(versions.textContent,'Installed: 0.5.1 (build c432478abcde)');
});

test('inaccessible release reason is displayed on the update page',async()=>{
 const message={textContent:''},notice={querySelector:()=>({}),classList:{toggle(){}}};
 const elements={'#updateNotice':notice,'#appView':{classList:{contains:()=>false}},'#panelUpdateMessage':message};
 vm.runInNewContext(code,{document:{querySelector:s=>elements[s]},MutationObserver:class{observe(){}},fetch:async()=>({ok:true,json:async()=>({checked:false,message:'No public stable release is accessible.'})}),Date,setInterval(){}});
 await new Promise(resolve=>setImmediate(resolve));
 assert.equal(message.textContent,'No public stable release is accessible.');
});

test('transient updater defines its own private Go cache paths',()=>{
 const worker=fs.readFileSync('scripts/update-panel.sh','utf8');
 assert.match(worker,/export GOPATH="\$work\/go" GOMODCACHE="\$work\/go\/pkg\/mod" GOCACHE="\$work\/go-build"/);
 assert.ok(worker.indexOf('export GOPATH=')<worker.indexOf('go build -p=1'));
});

test('network fetch failures explain connection and certificate checks',async()=>{
 const message={textContent:''},notice={querySelector:()=>({}),classList:{toggle(){}}};
 const elements={'#updateNotice':notice,'#appView':{classList:{contains:()=>false}},'#panelUpdateMessage':message};
 vm.runInNewContext(code,{document:{querySelector:s=>elements[s]},MutationObserver:class{observe(){}},TypeError,fetch:async()=>{throw new TypeError('Failed to fetch');},Date,setInterval(){}});
 await new Promise(resolve=>setImmediate(resolve));
 assert.match(message.textContent,/connection and the panel HTTPS certificate/);
});
