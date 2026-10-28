const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
test('panel text assets contain no corrupted UTF-8 punctuation',()=>{
 for(const name of fs.readdirSync('web')){
  if(!/\.(js|css|html|json)$/.test(name))continue;
  const text=new TextDecoder('utf-8',{fatal:true}).decode(fs.readFileSync(path.join('web',name)));
  assert.doesNotMatch(text,/\u00c2[\u0080-\u00bf]|\u00c3[\u0080-\u00bf\u2014]|\u00e2\u20ac|\ufffd/,name+' contains corrupted text');
 }
});
