const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const flush = async () => { for (let i=0;i<8;i++) await Promise.resolve(); };
function fixture(options={}) {
  const nodes = new Map(), events = {}, timers = new Map(), requests = [];
  let nextTimer=1;
  const element = () => ({
    value:'',dataset:{},hidden:false,disabled:false,textContent:'',html:'',
    set innerHTML(value) {this.html=value;if(this.siteSelect){const values=[...value.matchAll(/<option value="([^"]*)"/g)].map(m=>m[1]);if(!values.includes(this.value))this.value=values[0]||'';}},
    get innerHTML(){return this.html;},
    append(){},replaceChildren(){this.html='';},
    querySelector(selector){if(!nodes.has(selector))nodes.set(selector,element());return nodes.get(selector);},
    querySelectorAll(){return [element(),element()];}
  });
  const page=element();
  for (const [selector,value] of [['[data-range]','7'],['[data-group]','daily'],['[data-dimension]','pages'],['[data-size]','10']])page.querySelector(selector).value=value;
  page.querySelector('[data-site]').siteSelect=true;
  const document={hidden:false,createElement:element,querySelector:()=>element(),addEventListener:(name,fn)=>events[name]=fn};
  const location={hash:'#website-traffic'};
  const metric={requests:10,pageviews:7,bytes:5000,requestBytes:200,errors:1,bots:2,duration:.5,maxDuration:.2};
  const report={summary:metric,points:[{time:'2026-11-01',...metric}],rows:[{name:'<img src=x onerror=alert(1)>',...metric}],page:1,pages:2,size:10,total:11,updated:'2026-11-01T01:00:00Z',started:'2026-11-01T00:00:00Z',malformed:0};
  const live={entries:[{time:'2026-11-01T01:00:00Z',path:'/<script>alert(1)</script>',method:'GET',status:200,bytes:123,duration:.1,referrer:'example.org',device:'Mobile',browser:'Safari',os:'iOS',address:'192.0.2.0/24',country:'Unavailable'}],limit:200,updated:report.updated};
  let sites=[{id:'one',domain:'one.example'},{id:'two',domain:'two.example'}];
  const cp={escapeHTML:value=>String(value).replaceAll('&','&amp;').replaceAll('<','&lt;').replaceAll('>','&gt;').replaceAll('"','&quot;'),state:()=>({sites}),
    api:async url=>{requests.push(url);if(options.api)return options.api(url,report,live);return url.includes('view=live')?live:report;},
    navigate:id=>{location.hash='#'+id;window.LiteCPFeatures.navigate(id);}
  };
  const window={LiteCP:cp,LiteCPFeatures:{render(){},navigate(){}},addEventListener:(name,fn)=>events[name]=fn};
  vm.runInNewContext(fs.readFileSync('web/website-traffic.js','utf8'),{window,document,location,URLSearchParams,Date,Number,Math,JSON,encodeURIComponent,setInterval:fn=>{const id=nextTimer++;timers.set(id,fn);return id;},clearInterval:id=>timers.delete(id),setTimeout:fn=>{fn();return 0;},clearTimeout(){}});
  return {nodes,document,window,location,events,timers,requests,report,live,setSites:value=>{sites=value;},tick:async()=>{for(const fn of timers.values())fn();await flush();}};
}

test('renders reports and live access with escaped request data',async()=>{
  const f=fixture();await flush();
  assert.equal(f.requests.length,2);
  assert.match(f.nodes.get('[data-cards]').innerHTML,/Estimated pageviews/);
  assert.equal((f.nodes.get('[data-charts]').innerHTML.match(/<svg/g)||[]).length,4);
  assert.match(f.nodes.get('[data-rows]').innerHTML,/&lt;img/);
  assert.doesNotMatch(f.nodes.get('[data-rows]').innerHTML,/<img/);
  assert.match(f.nodes.get('[data-live-rows]').innerHTML,/&lt;script&gt;/);
  assert.match(f.nodes.get('[data-live-rows]').innerHTML,/192.0.2.0\/24/);
  assert.equal(f.nodes.get('[data-prev]').disabled,true);
  assert.equal(f.nodes.get('[data-next]').disabled,false);
});

test('live polling pauses and stops on hidden pages and navigation',async()=>{
  const f=fixture();await flush();
  await f.tick();assert.equal(f.requests.length,3);
  f.nodes.get('[data-pause]').onclick();await f.tick();assert.equal(f.requests.length,3);
  assert.equal(f.nodes.get('[data-pause]').textContent,'Resume');
  f.nodes.get('[data-pause]').onclick();await flush();assert.equal(f.requests.length,4);
  f.document.hidden=true;f.events.visibilitychange();assert.equal(f.timers.size,0);
  f.document.hidden=false;f.events.visibilitychange();await flush();assert.equal(f.timers.size,1);
  f.location.hash='#websites';f.events.hashchange();assert.equal(f.timers.size,0);
});

test('site switches discard stale asynchronous reports',async()=>{
  let release;
  const f=fixture({api:(url,report,live)=>url.includes('view=live')?Promise.resolve(live):new Promise(resolve=>{release=()=>resolve(report);})});
  f.nodes.get('[data-site]').value='two';f.nodes.get('[data-site]').onchange();
  release();await flush();
  assert.equal(f.nodes.get('[data-cards]').innerHTML,'');
  await f.tick();
  assert.match(f.requests.at(-2),/siteId=two/);
});

test('custom dates, report pagination and live filters reach their endpoints',async()=>{
  const f=fixture();await flush();
  f.nodes.get('[data-next]').onclick();await flush();assert.ok(f.requests.some(url=>new URLSearchParams(url.split('?')[1]).get('page')==='2'));
  f.nodes.get('[data-range]').value='custom';f.nodes.get('[data-from]').value='2026-10-01';f.nodes.get('[data-to]').value='2026-10-31';f.nodes.get('[data-range]').onchange();await flush();
  assert.ok(f.requests.some(url=>url.includes('from=2026-10-01')&&url.includes('to=2026-10-31')));
  f.nodes.get('[data-live-filter]').value='example.org';f.nodes.get('[data-live-filter]').oninput();await flush();
  assert.match(f.requests.at(-1),/filter=example.org/);
});

test('missing websites produce a useful empty state and no API requests',async()=>{
  const f=fixture();await flush();f.requests.length=0;f.setSites([]);f.window.LiteCPFeatures.render({sites:[]});await f.tick();
  assert.equal(f.requests.length,0);assert.equal(f.nodes.get('[data-refresh]').disabled,true);
  assert.match(f.nodes.get('[data-status]').textContent,/Add a website/);
});
