const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync(__dirname+'/file-manager.js','utf8');
const start=source.indexOf('    const onEscape=()=>{if(dialog.contains(uploadForm))cancelUpload();};');
const end=source.indexOf("    uploadForm.querySelector",start);
function harness(){
 const listeners=new Map(),form={};let cancellations=0;
 const dialog={open:true,contains:node=>node===form,addEventListener(type,fn){if(!listeners.has(type))listeners.set(type,new Set());listeners.get(type).add(fn)},removeEventListener(type,fn){listeners.get(type)?.delete(fn)}};
 vm.runInNewContext(source.slice(start,end),{dialog,uploadForm:form,cancelUpload(){cancellations++}});
 return {dialog,emit(type){for(const fn of [...(listeners.get(type)||[])])fn()},get cancellations(){return cancellations},get listeners(){return listeners}};
}
test('queued picker close does not cancel an active upload',()=>{
 const h=harness();h.emit('close');assert.equal(h.cancellations,0);assert.equal(h.listeners.get('close').size,1);
});
test('real close still cancels upload after a stale close event',()=>{
 const h=harness();h.emit('close');h.dialog.open=false;h.emit('close');assert.equal(h.cancellations,1);assert.equal(h.listeners.get('close').size,0);assert.equal(h.listeners.get('cancel').size,0);
});
test('Escape still cancels the current upload',()=>{
 const h=harness();h.emit('cancel');assert.equal(h.cancellations,1);
});
