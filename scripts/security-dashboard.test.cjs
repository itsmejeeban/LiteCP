const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

test('failed refresh clears previous security observations and allows retry', async () => {
  const source = fs.readFileSync('web/security-dashboard.js', 'utf8');
  const start = source.indexOf(' let pending=null,generation=0;');
  const end = source.indexOf(" page.querySelector('button').onclick", start);
  const status = {}, button = {}, checks = { stale: true, replaceChildren() { this.stale = false; } };
  let reject;
  const context = {
    page: { querySelector: selector => selector === 'button' ? button : selector === '[data-security-status]' ? status : checks },
    cp: { api: () => new Promise((_, fail) => { reject = fail; }) }
  };
  vm.runInNewContext(source.slice(start, end) + ';this.refresh=load;', context);
  const request = context.refresh();
  assert.equal(checks.stale, false);
  assert.equal(button.disabled, true);
  reject(new Error('Connection unavailable'));
  await request;
  assert.equal(button.disabled, false);
  assert.match(status.textContent, /Unable to check security status.*Connection unavailable/);
});
