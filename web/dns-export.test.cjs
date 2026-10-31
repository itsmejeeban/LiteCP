const test=require('node:test');
const assert=require('node:assert/strict');
const {zone}=require('./dns-export.js');
test('exports mail records with absolute names and MX target',()=>{
 const text=zone('example.com',[
 {type:'A',name:'mail.example.com',value:'192.0.2.1'},
 {type:'MX',name:'example.com',value:'10 mail.example.com.'},
 {type:'TXT',name:'example.com',value:'v=spf1 mx -all'},
 {type:'TXT',name:'_dmarc.example.com',value:'v=DMARC1; p=none'}]);
 assert.match(text,/mail\.example\.com\. 3600 IN A 192\.0\.2\.1/);
 assert.match(text,/example\.com\. 3600 IN MX 10 mail\.example\.com\./);
 assert.match(text,/IN TXT "v=spf1 mx -all"/);
});
test('chunks long DKIM without losing content',()=>{
 const value='v=DKIM1; p='+'A'.repeat(600);
 const line=zone('example.com',[{type:'TXT',name:'default._domainkey.example.com',value}]).split('\n').find(x=>x.includes('IN TXT'));
 const chunks=[...line.matchAll(/"([^"]*)"/g)].map(x=>x[1]);
 assert.equal(chunks.join(''),value);assert.ok(chunks.every(x=>x.length<=255));
});
test('escapes quotes backslashes and record-injection newlines',()=>{
 const text=zone('example.com',[{type:'TXT',name:'example.com',value:'a"b\\c\nBAD'}]);
 assert.ok(text.includes('a\\"b\\\\c\\010BAD'));assert.ok(!text.includes('\nBAD'));
});
test('rejects unsafe owner names and malformed MX',()=>{
 assert.throws(()=>zone('example.com',[{type:'TXT',name:'bad\nIN',value:'x'}]));
 assert.throws(()=>zone('example.com',[{type:'MX',name:'example.com',value:'70000 mail.example.com'}]));
});
