const test=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
test('first-party panel text is valid UTF-8 without known mojibake sequences',()=>{
 const decoder=new TextDecoder('utf-8',{fatal:true});
 const markers=/\u00c2[\u0080-\u00bf]|\u00e2\u20ac|\u00c3[\u0080-\u00bf]|\u00f0\u0178|\u00ef\u00bf\u00bd/;
 function inspect(dir){
  for(const entry of fs.readdirSync(dir,{withFileTypes:true})){
   if(entry.name==='vendor'||entry.name==='assets')continue;
   const file=path.join(dir,entry.name);
   if(entry.isDirectory()){inspect(file);continue;}
   if(!/\.(js|html|css)$/.test(entry.name))continue;
   const text=decoder.decode(fs.readFileSync(file));
   assert.equal(markers.test(text),false,'Corrupted text in '+path.relative(__dirname,file));
  }
 }
 inspect(__dirname);
});
