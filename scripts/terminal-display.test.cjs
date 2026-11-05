const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync(require('node:path').join(__dirname,'../web/terminal.js'),'utf8');
function setup(requestFullscreen){
  const classes=new Set(),button={},feedback={textContent:''},status={textContent:'Use SSH'},listeners={};
  const panel={requestFullscreen,classList:{contains:c=>classes.has(c),toggle(c){if(classes.has(c)){classes.delete(c);return false;}classes.add(c);return true;}}};
  let resized=0,focused=0;
  const document={fullscreenElement:null,getElementById:()=>button,addEventListener:(name,fn)=>listeners[name]=fn};
  const code=source.slice(source.indexOf('  function toggleExpandedTerminal()'),source.indexOf("  document.addEventListener('fullscreenchange'"));
  vm.runInNewContext(code,{document,panel,feedback,status,resize:()=>resized++,terminal:{focus:()=>focused++}});
  return {button,feedback,status,classes,listeners,counts:()=>({resized,focused})};
}
test('denied native fullscreen falls back without overwriting connection status',async()=>{
  const h=setup(async()=>{throw Error('not granted');});await h.button.onclick();
  assert.ok(h.classes.has('terminal-expanded'));assert.equal(h.button.textContent,'Exit full screen');
  assert.equal(h.status.textContent,'Use SSH');assert.match(h.feedback.textContent,/browser window/);
  h.listeners.keydown({key:'Escape'});assert.equal(h.classes.size,0);assert.equal(h.button.textContent,'Full screen');
  assert.deepEqual(h.counts(),{resized:2,focused:2});
});
test('expanded mode exits with its button',async()=>{
  const h=setup(async()=>{throw Error('unsupported');});await h.button.onclick();await h.button.onclick();
  assert.equal(h.classes.size,0);assert.equal(h.feedback.textContent,'');
});
test('native fullscreen success does not enable fallback',async()=>{
  const h=setup(async()=>{});await h.button.onclick();assert.equal(h.classes.size,0);assert.equal(h.status.textContent,'Use SSH');
});
