const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const code=fs.readFileSync('web/update-notice.js','utf8');
async function render(result,ok=true){
 const label={textContent:''},notice={hidden:true,querySelector:()=>label,classList:{toggle(_name,hidden){notice.hidden=hidden;}}};
 const install={hidden:true,disabled:true,addEventListener(){}};
 const elements={'#updateNotice':notice,'#appView':{classList:{contains:()=>false}},'#installPanelUpdate':install};
 vm.runInNewContext(code,{document:{querySelector:s=>elements[s]},MutationObserver:class{observe(){}},fetch:async()=>({ok,json:async()=>result}),Date,setInterval(){}});
 await new Promise(resolve=>setImmediate(resolve));return {notice,label,install};
}
test('new stable release displays a warning with the exact version and Settings Update link',async()=>{
 const {notice,label,install}=await render({checked:true,available:true,latest:'v0.5.2'});
 assert.equal(install.hidden,false);assert.equal(install.disabled,false);
 assert.equal(notice.hidden,false);assert.equal(label.textContent,'LiteCP 0.5.2 version available for update');assert.equal(notice.href,'#settings/update');
});
test('current version, unavailable checks and malformed versions do not show a warning',async()=>{
 for(const result of [{checked:true,available:false,latest:'v0.5.1'},{checked:false,available:true,latest:'v0.5.2'},{checked:true,available:true,latest:'<script>'}])assert.equal((await render(result)).notice.hidden,true);
 assert.equal((await render({},false)).notice.hidden,true);
 assert.equal((await render({},false)).install.hidden,true);
 assert.equal((await render({checked:true,available:false})).install.hidden,true);
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

test('panel updater checks backup capacity before downtime and copies recovery data before binary replacement',()=>{
 const worker=fs.readFileSync('scripts/update-panel.sh','utf8');
 assert.match(worker,/data_kib=\$\(du -sk -- "\$data_dir"/);
 assert.match(worker,/available_kib < data_kib \+ reserve_kib/);
 assert.match(worker,/check_recovery_space "\$work"[\s\S]*?\nsystemctl stop litecp\r?\ncp -a "\$data_dir" "\$work\/panel-data"[\s\S]*?\nbinary_install_started=1\r?\ninstall -m 0755/);
 assert.match(worker,/if \[\[ "\$binary_install_started" == 1 \]\]; then\r?\n\s+cp --preserve=mode "\$work\/previous-litecp"/);
});

test('panel updater applies restore point retention only after recording verified success',()=>{
 const worker=fs.readFileSync('scripts/update-panel.sh','utf8');
 const success=worker.slice(worker.indexOf('for attempt in $(seq 1 45)'));
 assert.match(success,/check-privilege-services\.sh[\s\S]*?status running "Applying restore point retention\."[\s\S]*?trap - ERR[\s\S]*?panel-restore-points\.py" finish "\$\{work##\*\/\}"[\s\S]*?status success/);
 assert.equal((worker.match(/panel-restore-points\.py/g)||[]).length,1);
});

test('network fetch failures explain connection and certificate checks',async()=>{
 const message={textContent:''},notice={querySelector:()=>({}),classList:{toggle(){}}};
 const elements={'#updateNotice':notice,'#appView':{classList:{contains:()=>false}},'#panelUpdateMessage':message};
 vm.runInNewContext(code,{document:{querySelector:s=>elements[s]},MutationObserver:class{observe(){}},TypeError,fetch:async()=>{throw new TypeError('Failed to fetch');},Date,setInterval(){}});
 await new Promise(resolve=>setImmediate(resolve));
 assert.match(message.textContent,/connection and the panel HTTPS certificate/);
});
