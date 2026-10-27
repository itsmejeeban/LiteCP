const test = require('node:test');
const assert = require('node:assert/strict');
const { normalizePath, kind, prepareEntries,editableFile,htmlFile,extractableFile } = require('../web/file-manager.js');

test('address and destination paths stay relative to the website root', () => {
  assert.equal(normalizePath('/public/wp-content/uploads/'), 'wp-content/uploads');
  assert.equal(normalizePath('/public'), '');
  assert.equal(normalizePath(''), '');
  assert.equal(normalizePath('assets/icons'), 'assets/icons');
  for (const path of ['../etc', 'assets/../../etc', 'assets\\secret', 'assets/\0secret', 'assets/\nsecret']) {
    assert.throws(() => normalizePath(path));
  }
});

const entries = [
  {name:'file10.txt',path:'file10.txt',type:'file',size:10,modified:'2026-10-20'},
  {name:'file2.txt',path:'file2.txt',type:'file',size:20,modified:'2026-10-21'},
  {name:'.env',path:'.env',type:'file',size:30,modified:'2026-10-22'},
  {name:'uploads',path:'uploads',type:'directory',size:0,modified:'2026-10-19'}
];

test('filtering and natural sorting preserve directories first and source data', () => {
  const original = structuredClone(entries);
  assert.deepEqual(prepareEntries(entries,'',false,'name',false).map(x=>x.name), ['uploads','file2.txt','file10.txt']);
  assert.deepEqual(prepareEntries(entries,'FILE',true,'size',true).map(x=>x.name), ['file2.txt','file10.txt']);
  assert.equal(prepareEntries(entries,'',true,'modified',true)[0].name, 'uploads');
  assert.equal(prepareEntries(entries,'',true,'modified',true)[1].name, '.env');
  assert.deepEqual(prepareEntries(entries,'no-match',true,'name',false), []);
  assert.deepEqual(entries, original);
});

test('file type labels use the final extension and support folders', () => {
  assert.equal(kind({name:'uploads',type:'directory'}), 'Folder');
  assert.equal(kind({name:'backup.TAR.GZ',type:'file'}), 'Archive');
  assert.equal(kind({name:'photo.WEBP',type:'file'}), 'Image');
  assert.equal(kind({name:'index.php',type:'file'}), 'PHP file');
  assert.equal(kind({name:'LICENSE',type:'file'}), 'File');
});

test('right-click edit targets text files while media stays view-only',()=>{
 for(const name of ['index.php','page.html','style.css','app.js','settings.json','README'])assert.equal(editableFile({name,type:'file'}),true,name);
 for(const name of ['photo.jpg','image.png','document.pdf','archive.zip','font.woff2'])assert.equal(editableFile({name,type:'file'}),false,name);
 assert.equal(editableFile({name:'folder',type:'directory'}),false);
 assert.equal(htmlFile({name:'page.HTML',type:'file'}),true);
 assert.equal(htmlFile({name:'index.php',type:'file'}),false);
 for(const name of ['archive.zip','archive.tar','archive.tar.gz','archive.tgz','archive.tar.bz2','archive.tbz2'])assert.equal(extractableFile({name,type:'file'}),true,name);
});

test('home-based paths reject other accounts and retain public_html', () => {
  const home='/home/litecp/example.test';
  assert.equal(normalizePath(home+'/public_html/index.php',home),'public_html/index.php');
  assert.equal(normalizePath(home,home),'');
  assert.equal(normalizePath('public_html',home),'public_html');
  for(const value of ['/etc/passwd','/home/litecp/other.test',home+'2/public_html',home+'/../other.test'])assert.throws(()=>normalizePath(value,home));
});

test('permission sorting compares octal modes in both directions', () => {
  const files=[{name:'b',type:'file',mode:'0750'},{name:'c',type:'file',mode:'0600'},{name:'a',type:'file',mode:'0640'}];
  assert.deepEqual(prepareEntries(files,'',true,'mode',false).map(x=>x.mode),['0600','0640','0750']);
  assert.deepEqual(prepareEntries(files,'',true,'mode',true).map(x=>x.mode),['0750','0640','0600']);
});

test('search discards responses from a previous website and older request', async () => {
  const fs=require('node:fs'),vm=require('node:vm');
  const source=fs.readFileSync(require('node:path').join(__dirname,'../web/file-manager.js'),'utf8');
  const code=source.slice(source.indexOf('  async function searchFiles()'),source.indexOf('  async function editFile('));
  const pending=[],shown=[],controls={'#cpfmSearchInput':{value:'one'},'#cpfmSearchScope':{value:'all'},'#showHidden':{checked:false}};
  let current='site1';
  const search=vm.runInNewContext('let searchRequest=0;'+code+';searchFiles',{URLSearchParams,$:key=>controls[key],siteID:()=>current,cp:{path:()=>'',api:()=>new Promise(resolve=>pending.push(resolve))},showSearchResults:result=>shown.push(result)});
  const first=search();current='site2';pending.shift()({entries:['wrong site']});await first;assert.equal(shown.length,0);
  const older=search();controls['#cpfmSearchInput'].value='two';const latest=search();
  pending[1]({entries:['latest']});await latest;pending[0]({entries:['stale']});await older;
  assert.equal(shown.length,1);assert.deepEqual(shown[0].entries,['latest']);
});
