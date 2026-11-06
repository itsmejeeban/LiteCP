const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync('web/app.js', 'utf8');
const renderer = source.slice(source.indexOf('  function renderApplicationLogo(site)'), source.indexOf('  function renderSSLStatusIcon(site)'));
const context = {escapeHTML: value => String(value).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]))};
vm.createContext(context);
vm.runInContext(renderer, context);
test('enabled application icons reflect the active execution runtime', () => {
  for (const [language, name] of [['node','Node.js'],['python','Python'],['ruby','Ruby']]) {
    const html = context.renderApplicationLogo({appType:'regular', application:{enabled:true,language,version:'<test>'}});
    assert.ok(html.includes(`runtime-${language}.svg`));
    assert.ok(html.includes(`alt="${name} logo"`));
    assert.ok(html.includes('&lt;test&gt;'));
  }
});
test('disabled or unrecognized runtimes retain the application icon', () => {
  assert.match(context.renderApplicationLogo({appType:'wordpress',application:{enabled:false,language:'node'}}), /logo-wordpress/);
  assert.match(context.renderApplicationLogo({appType:'regular',application:{enabled:true,language:'__proto__'}}), /logo-php/);
  assert.match(context.renderApplicationLogo({appType:'whmcs'}), /logo-whmcs/);
});
