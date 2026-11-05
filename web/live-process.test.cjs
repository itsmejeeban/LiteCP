const test=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
function setup(hash='#dashboard') {
 const nodes=new Map();
 const page={dataset:{},querySelector(selector){if(!nodes.has(selector))nodes.set(selector,{value:selector==='[data-sort]'?'cpu-desc':'',textContent:'',innerHTML:''});return nodes.get(selector);}};
 const location={hash};let requests=0,interval=null,previousCalls=0;
 const features={navigate(){previousCalls++;}};
 const cp={escapeHTML:x=>x,formatBytes:x=>String(x),state:()=>null,navigate(){},async api(){requests++;return {processes:[],summary:{cpu:1,memory:2},note:'0 processes',updatedAt:Date.now()};}};
 vm.runInNewContext(fs.readFileSync(__dirname+'/live-process.js','utf8'),{window:{LiteCP:cp,LiteCPFeatures:features,addEventListener(){}},document:{hidden:false,createElement:()=>page,querySelector:()=>({append(){}}),addEventListener(){}},location,setInterval(fn,ms){assert.equal(ms,3000);interval=fn;return 1;},clearInterval(){interval=null;},Date});
 return {location,features,get requests(){return requests;},get interval(){return interval;},get previousCalls(){return previousCalls;}};
}
test('sidebar navigation loads immediately without hashchange and polls until leaving',async()=>{
 const app=setup();assert.equal(app.requests,0);
 app.location.hash='#live-process';app.features.navigate('live-process');await new Promise(setImmediate);
 assert.equal(app.requests,1);assert.equal(app.previousCalls,1);assert.equal(typeof app.interval,'function');
 await app.interval();assert.equal(app.requests,2);
 app.location.hash='#websites';app.features.navigate('websites');assert.equal(app.interval,null);
});
test('direct Live Process URL loads without Refresh',async()=>{const app=setup('#live-process');await new Promise(setImmediate);assert.equal(app.requests,1);assert.equal(typeof app.interval,'function');});

test('empty process searches show a clear message',()=>{
 const source=fs.readFileSync(__dirname+'/live-process.js','utf8');
 const code=source.slice(source.indexOf(' function render()'),source.indexOf(' async function load()'));
 const body={},controls={'[data-sort]':{value:'cpu-desc'},'[data-search]':{value:'missing'},tbody:body};
 const context={page:{querySelector:key=>controls[key]},entries:[],cp:{formatBytes:String},esc:String};
 vm.runInNewContext(code+';render();',context);
 assert.match(body.innerHTML,/No processes match your search/);
 controls['[data-search]'].value='';vm.runInNewContext('render()',context);assert.match(body.innerHTML,/No processes available/);
});

test('process responses from a previous page visit are ignored',async()=>{
 const source=fs.readFileSync(__dirname+'/live-process.js','utf8');
 const code=source.slice(source.indexOf(' async function load()'),source.indexOf(' const features='));
 let resolve;const summary={},location={hash:'#live-process'};
 const context=vm.createContext({busy:false,generation:0,entries:[],timer:null,location,document:{hidden:false},cp:{api:()=>new Promise(done=>resolve=done)},render(){throw Error('stale response rendered');},page:{querySelector:()=>summary},clearInterval(){},setInterval(){}});
 vm.runInContext(code+';this.pending=load();',context);
 location.hash='#dashboard';vm.runInContext('activate()',context);
 location.hash='#live-process';resolve({processes:[{pid:1}]});await context.pending;
 assert.equal(context.entries.length,0);assert.equal(summary.textContent,undefined);
});
