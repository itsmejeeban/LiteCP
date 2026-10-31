const test=require('node:test'),assert=require('node:assert/strict'),vm=require('node:vm'),fs=require('node:fs');
function harness(route,configured,aliases=[]){
 class Node{constructor(){this.children=[];this.dataset={};this.attrs={};this.nodes=new Map();this.classList={toggle(){},add(){},contains(){return false}};}addEventListener(){}append(...n){this.children.push(...n)}replaceWith(){}matches(){return false}closest(){return this}setAttribute(k,v){this.attrs[k]=v}getAttribute(k){return this.attrs[k]}removeAttribute(k){delete this.attrs[k]}querySelector(k){if(!this.nodes.has(k))this.nodes.set(k,new Node());return this.nodes.get(k)}}
 const nodes=new Map(),listeners=new Map(),created=[];let prompts=0,calls=0;
 const location={hash:'#'+route};
 const window={LiteCPFeatures:{navigate(){},render(){}},addEventListener(k,fn){if(!listeners.has(k))listeners.set(k,[]);listeners.get(k).push(fn)},LiteCPDialogs:{async confirm(){prompts++;return true}}};
 const cp={escapeHTML:value=>String(value).replaceAll("&","&amp;").replaceAll("<","&lt;").replaceAll('"',"&quot;"),state:()=>({mailAliases:aliases,system:{webmailAvailable:configured},settings:{}}),api:async()=>{calls++;return {configured}},navigate(route){location.hash='#'+route;(listeners.get('hashchange')||[]).forEach(fn=>fn())}};window.LiteCP=cp;
 const document={createElement:()=>{const node=new Node();created.push(node);return node},querySelector(k){if(!nodes.has(k))nodes.set(k,new Node());return nodes.get(k)}};
 vm.runInNewContext(fs.readFileSync(__dirname+'/email-tabs.js','utf8'),{window,document,location});
 return {location,get forwardList(){return created[1].innerHTML},get title(){return document.querySelector('[data-page-view="email"]').querySelector('h1').textContent},get prompts(){return prompts},get calls(){return calls},complete(){(listeners.get('litecp:webmail-status')||[]).forEach(fn=>fn({detail:{configured:true}}))}};
}
const settle=()=>new Promise(resolve=>setImmediate(resolve));
test('unconfigured Email tool prompts and opens setup; completion resumes requested tab',async()=>{const h=harness('email/create-mailbox',false);await settle();assert.equal(h.prompts,1);assert.equal(h.location.hash,'#email/configure-webmail');h.complete();assert.equal(h.location.hash,'#email/create-mailbox');await settle();assert.equal(h.prompts,1)});
test('configured server opens Email tool without setup prompt',async()=>{const h=harness('email/domains',true);await settle();assert.equal(h.location.hash,'#email/domains');assert.equal(h.prompts,0)});
test('Configure Webmail is accessible without prompting itself',async()=>{const h=harness('email/configure-webmail',false);await settle();assert.equal(h.prompts,0);assert.equal(h.calls,0)});

test("Email Forward route opens its own page",async()=>{const h=harness("email/forward",true);await settle();assert.equal(h.title,"Email Forward");assert.equal(h.prompts,0)});

test('Email Forward lists existing rules with Edit and Delete actions',()=>{
 const h=harness('email/forward',true,[{id:'forward-1',source:'sales@example.com',destination:'owner@example.net'}]);
 assert.match(h.forwardList,/sales@example.com/);assert.match(h.forwardList,/owner@example.net/);
 assert.match(h.forwardList,/data-edit-forward="forward-1"/);assert.match(h.forwardList,/data-delete-forward="forward-1"/);
});
test('Email Forward has an empty state',()=>{assert.match(harness('email/forward',true).forwardList,/No email forwards yet/)});
