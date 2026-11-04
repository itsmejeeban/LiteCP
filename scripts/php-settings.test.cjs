const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
test('PHP settings locks all editor fields until save completes',async()=>{
 const source=fs.readFileSync('web/php-settings.js','utf8');let finish,submitted;
 const controls=Array.from({length:4},()=>({disabled:false}));const saveButton={disabled:false};
 const editor={elements:{namedItem:()=>({value:'256M'}),custom:{value:''}},querySelectorAll:()=>controls,querySelector:()=>saveButton};
 const select={disabled:false,options:[{}]},picker={querySelector:()=>saveButton},status={textContent:''};
 const context=vm.createContext({editor,select,picker,status,labels:{memory_limit:'Memory'},encodeURIComponent,cp:{state:()=>({sites:[{id:'qa'}]}),api:async(url,options)=>{submitted=options.body;await new Promise(done=>finish=done);}}});
 vm.runInContext(`let busy=false,active='qa';${source.slice(source.indexOf(' editor.onsubmit='),source.indexOf(" document.querySelector('#sitesTable')"))}`,context);
 const pending=editor.onsubmit({preventDefault(){}});
 assert.ok(controls.every(c=>c.disabled));assert.equal(select.disabled,true);assert.equal(submitted.settings.memory_limit,'256M');
 finish();await pending;assert.ok(controls.every(c=>!c.disabled));assert.equal(select.disabled,false);assert.equal(status.textContent,'PHP settings saved.');
});
test('PHP settings unlocks fields after failed save and reports error',async()=>{
 const source=fs.readFileSync('web/php-settings.js','utf8');const controls=[{disabled:false}];const button={disabled:false};
 const editor={elements:{custom:{value:''}},querySelectorAll:()=>controls,querySelector:()=>button};const select={options:[{}]},status={};
 const context=vm.createContext({editor,select,picker:{querySelector:()=>button},status,labels:{},encodeURIComponent,cp:{state:()=>({sites:[{id:'qa'}]}),api:async()=>{throw Error('runtime failed');}}});
 vm.runInContext(`let busy=false,active='qa';${source.slice(source.indexOf(' editor.onsubmit='),source.indexOf(" document.querySelector('#sitesTable')"))}`,context);
 await editor.onsubmit({preventDefault(){}});assert.equal(controls[0].disabled,false);assert.equal(status.textContent,'runtime failed');
});
