const test=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const path=require('node:path');
const source=fs.readFileSync(path.join(__dirname,'../web/app.js'),'utf8');
function harness(){
  const controls={'#logTarget':{value:'php'},'#logViewer code':{textContent:''},'#logStatus':{textContent:''}};
  const pending=[];
  const code=source.slice(source.indexOf('  async function loadLogs()'),source.indexOf('  function toggleSidebar()'));
  const load=vm.runInNewContext('let logRequest=0;'+code+';loadLogs',{$:key=>controls[key],api:()=>new Promise((resolve,reject)=>pending.push({resolve,reject}))});
  return {controls,pending,load};
}
test('older source success cannot overwrite the current source',async()=>{
  const h=harness();const old=h.load();h.controls['#logTarget'].value='mail';const current=h.load();
  h.pending[1].resolve({lines:['mail entry']});await current;
  h.pending[0].resolve({lines:['old PHP entry']});await old;
  assert.equal(h.controls['#logViewer code'].textContent,'mail entry');
  assert.match(h.controls['#logStatus'].textContent,/Updated/);
});
test('older request errors cannot overwrite current empty results',async()=>{
  const h=harness();const old=h.load();const current=h.load();
  h.pending[1].resolve({lines:[]});await current;
  h.pending[0].reject(Error('old failure'));await old;
  assert.equal(h.controls['#logViewer code'].textContent,'No recent entries for this source.');
  assert.doesNotMatch(h.controls['#logStatus'].textContent,/failed/);
});
test('current errors remain visible and logs are rendered as plain text',async()=>{
  const h=harness();const failed=h.load();h.pending[0].reject(Error('unavailable'));await failed;
  assert.match(h.controls['#logViewer code'].textContent,/unavailable/);
  assert.match(h.controls['#logStatus'].textContent,/Refresh failed/);
  const retry=h.load();h.pending[1].resolve({lines:['<script>alert(1)</script>']});await retry;
  assert.equal(h.controls['#logViewer code'].textContent,'<script>alert(1)</script>');
});
