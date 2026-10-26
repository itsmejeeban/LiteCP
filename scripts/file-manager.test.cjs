const test = require('node:test');
const assert = require('node:assert/strict');
const { normalizePath, kind, prepareEntries } = require('../web/file-manager.js');

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
