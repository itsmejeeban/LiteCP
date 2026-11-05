const test=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const path=require('node:path');
const source=fs.readFileSync(path.join(__dirname,'../web/app.js'),'utf8');
function harness(){
  const controls={'#logTarget':{value:'php'},'#logViewer code':{textContent:''},'#logStatus':{textContent:''},'#copyLogs':{disabled:true},'#downloadLogs':{disabled:true}};
  const pending=[];
  const code=source.slice(source.indexOf('  async function loadLogs()'),source.indexOf('  function toggleSidebar()'));
  const load=vm.runInNewContext('let logRequest=0,displayedLogs=null;'+code+';loadLogs',{$:key=>controls[key],api:()=>new Promise((resolve,reject)=>pending.push({resolve,reject}))});
  return {controls,pending,load};
}
test('older source success cannot overwrite the current source',async()=>{
  const h=harness();const old=h.load();h.controls['#logTarget'].value='mail';const current=h.load();
  h.pending[1].resolve({lines:['mail entry']});await current;
  h.pending[0].resolve({lines:['old PHP entry']});await old;
  assert.equal(h.controls['#logViewer code'].textContent,'mail entry');
  assert.match(h.controls['#logStatus'].textContent,/Updated/);
  assert.equal(h.controls['#copyLogs'].disabled,false);
});
test('older request errors cannot overwrite current empty results',async()=>{
  const h=harness();const old=h.load();const current=h.load();
  h.pending[1].resolve({lines:[]});await current;
  h.pending[0].reject(Error('old failure'));await old;
  assert.equal(h.controls['#logViewer code'].textContent,'No recent entries for this source.');
  assert.doesNotMatch(h.controls['#logStatus'].textContent,/failed/);
  assert.equal(h.controls['#downloadLogs'].disabled,true);
});
test('current errors remain visible and logs are rendered as plain text',async()=>{
  const h=harness();const failed=h.load();h.pending[0].reject(Error('unavailable'));await failed;
  assert.match(h.controls['#logViewer code'].textContent,/unavailable/);
  assert.match(h.controls['#logStatus'].textContent,/Refresh failed/);
  const retry=h.load();h.pending[1].resolve({lines:['<script>alert(1)</script>']});await retry;
  assert.equal(h.controls['#logViewer code'].textContent,'<script>alert(1)</script>');
});

test('copy and download export displayed entries with a source-specific filename',async()=>{
  const code=source.slice(source.indexOf('  async function copyLogs()'),source.indexOf('  function toggleSidebar()'));
  const writes=[],blobs=[],revoked=[];let clicked=false,removed=false,timer;
  const link={click(){clicked=true;},remove(){removed=true;}};
  const actions=vm.runInNewContext('let displayedLogs={target:"php",text:"first\\nsecond"};'+code+';({copyLogs,downloadLogs})',{
    navigator:{clipboard:{writeText:async text=>writes.push(text)}},showToast(){},
    Blob:class{constructor(parts){blobs.push(parts.join(''));}},
    URL:{createObjectURL:()=> 'blob:test',revokeObjectURL:url=>revoked.push(url)},
    document:{createElement:()=>link,body:{append(){}}},setTimeout:fn=>{timer=fn;}
  });
  await actions.copyLogs();actions.downloadLogs();
  assert.deepEqual(writes,['first\nsecond']);assert.deepEqual(blobs,['first\nsecond\n']);
  assert.match(link.download,/^litecp-php-.*\.log$/);assert.equal(clicked,true);assert.equal(removed,true);
  timer();assert.deepEqual(revoked,['blob:test']);
});
