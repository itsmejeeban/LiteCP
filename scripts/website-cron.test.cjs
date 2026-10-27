const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

function deferred() {
  let resolve, reject;
  const promise = new Promise((done, fail) => { resolve = done; reject = fail; });
  return { promise, resolve, reject };
}
function panel(api, confirm = async () => true) {
  const roots = new Map();
  let dialog;
  class Element {
    constructor(tag = 'div') {
      this.tagName = tag; this.children = new Map(); this.listeners = new Map();
      this.dataset = {}; this.value = ''; this.textContent = ''; this.disabled = false; this.hidden = false;
      this.checked = false; this.open = false; this.controls = []; this.elements = {};
      if (tag === 'form') for (const key of ['minute', 'hour', 'day', 'month', 'weekday', 'preset', 'name', 'command', 'enabled']) this.elements[key] = new Element('input');
    }
    set innerHTML(value) { this.html = value; this.children.clear(); this.controls = [new Element('button')]; }
    get innerHTML() { return this.html || ''; }
    querySelector(selector) {
      if (!this.children.has(selector)) this.children.set(selector, new Element(selector === 'form' ? 'form' : 'div'));
      return this.children.get(selector);
    }
    querySelectorAll() { return this.controls; }
    addEventListener(name, callback) {
      if (!this.listeners.has(name)) this.listeners.set(name, []);
      this.listeners.get(name).push(callback);
    }
    async dispatch(name, event) { await Promise.all((this.listeners.get(name) || []).map(callback => callback(event))); }
    showModal() { this.open = true; }
    close() { this.open = false; }
    append() {}
    prepend() {}
    setAttribute() {}
    focus() { this.focused = true; }
  }
  const document = {
    body: new Element(),
    createElement(tag) { const element = new Element(tag); if (tag === 'dialog') dialog = element; return element; },
    querySelector(selector) { if (!roots.has(selector)) roots.set(selector, new Element()); return roots.get(selector); },
    querySelectorAll() { return []; }
  };
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, '..', 'web', 'website-cron.js'), 'utf8'), {
    document,
    window: { LiteCP: { api, escapeHTML: value => String(value).replace(/</g, '&lt;'), state: () => ({ sites: [] }) }, LiteCPFeatures: { render() {} }, LiteCPDialogs: { confirm } }
  });
  function click(selector, dataset = {}) {
    return dialog.dispatch('click', { target: { closest: value => value === selector ? { dataset } : null } });
  }
  return {
    dialog,
    open: id => document.querySelector('#sitesTable').dispatch('click', {
      target: { closest: selector => selector === '[data-site-cron-jobs]' ? { dataset: { siteCronJobs: id } } : null }
    }),
    close: () => click('[data-cron-close]'),
    click,
    fields: () => dialog.querySelector('form').elements,
    submit: () => dialog.dispatch('submit', { preventDefault() {} }),
    preset: value => {
      const control = dialog.querySelector('form').elements.preset;
      control.value = value;
      return dialog.dispatch('change', { target: control });
    }
  };
}
const list = (id, items = []) => ({ domain: id + '.test', directory: '/public/' + id, timezone: 'UTC', phpCommand: '/usr/bin/php', appCron: { available: false }, items });
const job = id => ({ id, name: 'Daily job', schedule: '0 0 * * *', command: '/usr/bin/php task.php', enabled: true });

test('cron popup ignores old lists after switching websites and closing', async () => {
  const requests = [deferred(), deferred(), deferred()]; let request = 0;
  const ui = panel(() => requests[request++].promise);
  const first = ui.open('A'); await ui.close(); const second = ui.open('B');
  requests[1].resolve(list('B', [job('new-B')])); await second;
  requests[0].resolve(list('A', [job('old-A')])); await first;
  assert.match(ui.dialog.querySelector('[data-cron-list]').innerHTML, /new-B/);
  assert.doesNotMatch(ui.dialog.querySelector('[data-cron-list]').innerHTML, /old-A/);
  assert.equal(ui.dialog.querySelector('[data-cron-domain]').textContent, 'B.test');
  assert.equal(ui.dialog.querySelector('[data-cron-context]').textContent, 'Time zone: UTC');
  const third = ui.open('C'); await ui.close(); requests[2].resolve(list('C', [job('closed-C')])); await third;
  assert.equal(ui.dialog.querySelector('[data-cron-list]').innerHTML, '');
});

test('cron creation sends selected schedule, command and enabled state; invalid schedules do not write', async () => {
  const writes = [];
  const ui = panel(async (url, options) => { if (!options) return list('A'); writes.push({ url, ...options }); return {}; });
  await ui.open('A'); await ui.preset('weekly');
  assert.equal(ui.dialog.querySelector('[data-cron-expression]').textContent, '0 0 * * 0');
  ui.fields().command.value = '/usr/bin/php /public/A/report.php';
  ui.fields().name.value = ' Weekly report ';
  ui.fields().enabled.checked = false;
  ui.fields().minute.value = '99'; await ui.submit();
  assert.equal(writes.length, 0); assert.match(ui.dialog.querySelector('[data-cron-status]').textContent, /valid minute/);
  ui.fields().minute.value = '*/61'; await ui.submit(); assert.equal(writes.length, 0);
  ui.fields().minute.value = '5/2'; await ui.submit(); assert.equal(writes.length, 0);
  assert.match(ui.dialog.querySelector('[data-cron-status]').textContent, /valid minute/);
  ui.fields().minute.value = '0'; await ui.submit();
  assert.equal(writes.length, 1); assert.equal(writes[0].url, '/api/website-cron?siteId=A'); assert.equal(writes[0].method, 'POST');
  assert.deepEqual(JSON.parse(JSON.stringify(writes[0].body)), { name: 'Weekly report', schedule: '0 0 * * 0', command: '/usr/bin/php /public/A/report.php', enabled: false });
  assert.equal(ui.fields().command.value, '');
});

test('cron edit retains its identity and rejects multiline commands', async () => {
  const writes = [];
  const ui = panel(async (url, options) => { if (!options) return list('A', [job('job-A')]); writes.push(options); return {}; });
  await ui.open('A'); await ui.click('[data-cron-edit]', { cronEdit: 'job-A' });
  assert.equal(ui.fields().command.value, '/usr/bin/php task.php');
  ui.fields().command.value = 'echo one\necho two'; await ui.submit(); assert.equal(writes.length, 0);
  ui.fields().command.value = 'echo one\n'; await ui.submit(); assert.equal(writes.length, 0);
  assert.match(ui.dialog.querySelector('[data-cron-status]').textContent, /one command line/);
  ui.fields().command.value = 'echo one && echo two'; await ui.preset('hourly'); await ui.submit();
  assert.equal(writes.length, 1); assert.equal(writes[0].method, 'PUT');
  assert.equal(writes[0].body.id, 'job-A'); assert.equal(writes[0].body.schedule, '0 * * * *');
});

test('delayed removal confirmation cannot target a newly opened website', async () => {
  const answer = deferred(), writes = [];
  const ui = panel(async (url, options) => { if (!options) return list(url.endsWith('A') ? 'A' : 'B', [job('job-A')]); writes.push({ url, options }); return {}; }, () => answer.promise);
  await ui.open('A'); const removal = ui.click('[data-cron-remove]', { cronRemove: 'job-A' });
  await ui.close(); await ui.open('B'); answer.resolve(true); await removal;
  assert.equal(writes.length, 0);
});

test('old mutation completion cannot unlock a new popup mutation', async () => {
  const pending = [deferred(), deferred()], writes = [];
  const ui = panel((url, options) => { if (!options) return Promise.resolve(list(url.endsWith('A') ? 'A' : 'B')); writes.push(url); return pending[writes.length - 1].promise; });
  await ui.open('A'); ui.fields().command.value = 'echo A'; const first = ui.submit();
  ui.dialog.close(); await ui.open('B'); ui.fields().command.value = 'echo B'; const second = ui.submit();
  const activeButton = ui.dialog.controls[0];
  pending[0].resolve({}); await first;
  assert.equal(activeButton.disabled, true); assert.match(ui.dialog.querySelector('[data-cron-status]').textContent, /Saving/);
  pending[1].resolve({}); await second;
  assert.equal(activeButton.disabled, false); assert.equal(ui.dialog.querySelector('[data-cron-status]').textContent, 'Cron job created.');
  assert.deepEqual(writes, ['/api/website-cron?siteId=A', '/api/website-cron?siteId=B']);
});

test('beginner schedules hide custom timing fields and Custom reveals them', async () => {
 const ui=panel(async()=>list('A'));await ui.open('A');
 assert.equal(ui.dialog.querySelector('[data-cron-custom]').hidden,true);
 await ui.preset('custom');assert.equal(ui.dialog.querySelector('[data-cron-custom]').hidden,false);
 await ui.preset('daily');assert.equal(ui.dialog.querySelector('[data-cron-custom]').hidden,true);
 assert.equal(ui.dialog.querySelector('[data-cron-expression]').textContent,'0 0 * * *');
});

test('default application cron is managed inside the cron popup', async () => {
  const writes = [];
  let enabled = false;
  const ui = panel(async (url, options) => {
    if (url === '/api/sites/cron') { writes.push(options); enabled = options.body.enabled; return { ok: true }; }
    return { ...list('A'), appCron: { available: true, name: 'WordPress', enabled, schedule: '*/5 * * * *', command: '/public/A/wp-cron.php' } };
  });
  await ui.open('A');
  assert.equal(ui.dialog.querySelector('[data-cron-app]').hidden, false);
  assert.equal(ui.dialog.querySelector('[data-cron-app-title]').textContent, 'WordPress default cron');
  assert.equal(ui.dialog.querySelector('[data-cron-app-toggle]').textContent, 'Enable');
  await ui.click('[data-cron-app-toggle]');
  assert.equal(writes.length, 1);
  assert.deepEqual(JSON.parse(JSON.stringify(writes[0].body)), { id: 'A', enabled: true });
  assert.equal(ui.dialog.querySelector('[data-cron-app-toggle]').textContent, 'Disable');
});

test('Run test executes the saved job and displays output as plain text',async()=>{
 const writes=[];const ui=panel(async(url,options)=>{if(!options)return list('A',[job('saved')]);writes.push({url,...options});return {success:true,message:'Completed successfully',output:'<script>example</script>',durationMs:120};});
 await ui.open('A');await ui.click('[data-cron-run]',{cronRun:'saved'});
 assert.equal(writes.length,1);assert.equal(writes[0].url,'/api/website-cron/run?siteId=A');assert.equal(writes[0].body.id,'saved');
 assert.equal(ui.dialog.querySelector('[data-cron-output]').textContent,'<script>example</script>');
 assert.equal(ui.dialog.querySelector('[data-cron-result]').hidden,false);
});
test('cancelling Run test does not execute the command',async()=>{
 let writes=0;const ui=panel(async(url,options)=>{if(options)writes++;return list('A',[job('saved')]);},async()=>false);
 await ui.open('A');await ui.click('[data-cron-run]',{cronRun:'saved'});assert.equal(writes,0);
});
