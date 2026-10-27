const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('web/features.js','utf8');
test('dark mode initializes without reading saved light preferences or device appearance',()=>{
 for(const preference of ['light','dark','system',null]){
  const document={documentElement:{dataset:{theme:'light'}}};
  vm.runInNewContext(fs.readFileSync('web/theme.js','utf8'),{document,localStorage:{getItem(){throw Error('Saved theme must not be read');}},matchMedia(){throw Error('Device appearance must not be read');}});
  assert.equal(document.documentElement.dataset.theme,'dark',preference);
 }
 assert.match(fs.readFileSync('web/index.html','utf8'),/<html lang="en" data-theme="dark">/);
});
test('panel and editor stay dark with no light-mode toggle or search setting',()=>{
 const start=source.indexOf('  // LiteCP uses one consistent dark theme,'),end=source.indexOf('  $(".activity-panel")',start);
 assert.ok(start>=0&&end>start);
 const themes=[],document={documentElement:{dataset:{theme:'light'}}};
 vm.runInNewContext(source.slice(start,end),{document,editor:{setTheme:value=>themes.push(value)}});
 assert.equal(document.documentElement.dataset.theme,'dark');assert.deepEqual(themes,['ace/theme/tomorrow_night']);
 assert.doesNotMatch(source,/#themeToggle|prefers-color-scheme|themePreference/);
 assert.doesNotMatch(fs.readFileSync('web/panel-search.js','utf8'),/#themeToggle|Light \/ dark mode/);
});
