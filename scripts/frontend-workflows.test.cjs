const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const tick = () => new Promise(resolve => setImmediate(resolve));

function deferred() {
  let resolve;
  const promise = new Promise(done => { resolve = done; });
  return { promise, resolve };
}

function panel(script, api, options = {}) {
  const nodes = new Map(), created = [], navigations = [];
  class Element {
    constructor(tag = 'div') {
      this.tagName = tag.toUpperCase();
      this.dataset = {};
      this.listeners = {};
      this.attributes = {};
      this.value = '';
      this.innerHTML = '';
      this.textContent = '';
      this.disabled = false;
      const classes = new Set();
      this.classList = {
        add: (...names) => names.forEach(name => classes.add(name)),
        remove: (...names) => names.forEach(name => classes.delete(name)),
        contains: name => classes.has(name)
      };
    }
    querySelector(selector) { return element(selector); }
    querySelectorAll() { return []; }
    setAttribute(name, value) { this.attributes[name] = value; }
    append() {}
    prepend() {}
    before() {}
    after() {}
    remove() {}
    focus() { document.activeElement = this; }
    scrollIntoView() {}
    cloneNode() { return new Element(this.tagName); }
    addEventListener(name, listener) { this.listeners[name] = listener; }
  }
  function element(selector) {
    if (!nodes.has(selector)) nodes.set(selector, new Element());
    return nodes.get(selector);
  }
  const document = {
    activeElement: null,
    querySelector: element,
    createElement(tag) { const result = new Element(tag); created.push(result); return result; }
  };
  const location = { hash: options.hash || '#dashboard' };
  const features = { render() {}, navigate(page) { navigations.push(page); }, activity() {} };
  const cp = {
    api,
    escapeHTML: value => String(value).replace(/[&<>"']/g, char => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[char])),
    formatDateTime: value => value,
    state: () => ({ sites: [{ id: 'site1', domain: 'example.test', appType: 'regular' }] }),
    navigate(page) { location.hash = '#' + page; features.navigate(page); },
    refresh: async () => features.render(cp.state())
  };
  const window = { LiteCP: cp, LiteCPFeatures: features, addEventListener() {} };
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, '..', 'web', script), 'utf8'), {
    window, document, location, confirm: () => false, fetch: options.fetch
  }, { filename: script });
  return { cp, features, document, element, created, navigations };
}

const activities = title => ({ items: [{ title, detail: 'Details', createdAt: 'Today' }], pages: 1, total: 1 });
const catalogue = {
  enabled: { node: false, python: true, ruby: false },
  versions: { node: ['22'], python: ['3.13'], ruby: ['3.4'] },
  items: [{ siteId: 'site1', domain: 'example.test', status: 'stopped', application: {
    language: 'python', version: '3.13', command: 'python app.py', setupCommand: ''
  } }]
};

test('Recent Activities loads when navigation uses pushState without a hashchange', async () => {
  const calls = [];
  const ui = panel('activities.js', async url => {
    calls.push(url);
    return url.startsWith('/api/activities?') ? activities('Signed in') : { days: 15 };
  });
  ui.cp.navigate('activities');
  await tick();
  assert.deepEqual(calls, ['/api/activities?page=1&perPage=50', '/api/activity-policy']);
  assert.match(ui.element('#allActivityRows').innerHTML, /Signed in/);
  assert.equal(ui.element('#activityRetention').value, '15');
  assert.deepEqual(ui.navigations, ['activities']);
});

test('Recent Activities ignores an older response and preserves a focused policy choice', async () => {
  const requests = [];
  const ui = panel('activities.js', url => {
    if (url === '/api/activity-policy') return Promise.resolve({ days: 0 });
    const pending = deferred(); requests.push(pending); return pending.promise;
  });
  ui.cp.navigate('activities');
  ui.cp.navigate('activities');
  ui.element('#activityRetention').value = '365';
  ui.document.activeElement = ui.element('#activityRetention');
  requests[1].resolve(activities('Newest event'));
  await tick();
  requests[0].resolve(activities('Stale event'));
  await tick();
  assert.match(ui.element('#allActivityRows').innerHTML, /Newest event/);
  assert.doesNotMatch(ui.element('#allActivityRows').innerHTML, /Stale event/);
  assert.equal(ui.element('#activityRetention').value, '365');
});

test('Application Hosting loads on navigation and shares an in-flight refresh', async () => {
  const pending = deferred();
  let calls = 0;
  const ui = panel('application-hosting.js', () => { calls++; return pending.promise; });
  ui.cp.navigate('applications');
  ui.cp.navigate('applications');
  const waiting = ui.element('#applicationsRefresh').onclick();
  let completed = false;
  waiting.then(() => { completed = true; });
  await tick();
  assert.equal(calls, 1);
  assert.equal(completed, false);
  pending.resolve(structuredClone(catalogue));
  await waiting;
  assert.match(ui.element('#applicationList').innerHTML, /example\.test/);
  assert.deepEqual(ui.navigations, ['applications', 'applications']);
});

test('editing an application preserves an intentionally empty setup command', async () => {
  const ui = panel('application-hosting.js', async () => structuredClone(catalogue));
  ui.cp.navigate('applications');
  await tick();
  const page = ui.created.find(node => node.dataset.pageView === 'applications');
  await page.listeners.click({ target: { closest: selector => selector === '[data-app-edit]' ? { dataset: { appEdit: 'site1' } } : null } });
  assert.equal(ui.element('#applicationSetup').value, '');
  assert.equal(ui.element('#applicationCommand').value, 'python app.py');
  assert.equal(ui.element('#applicationWebsite').value, 'site1');
  assert.equal(ui.element('#applicationVersion').value, '3.13');
});

test('Documentation loads its manual through direct panel navigation', async () => {
  const manual = [{ title: 'Websites', page: 'websites', topics: [{ title: 'Create website', steps: ['Click Add website.'] }] }];
  let fetched = 0;
  const ui = panel('documentation.js', async () => {}, { fetch: async url => {
    assert.equal(url, '/manual.json'); fetched++; return { ok: true, json: async () => manual };
  } });
  ui.cp.navigate('documentation');
  await tick();
  assert.equal(fetched, 1);
  assert.equal(ui.element('#manualCount').textContent, '1 of 1 sections');
  assert.match(ui.element('#manualSections').innerHTML, /Click Add website\./);
});

// Run the real app functions without its unrelated login/dashboard bootstrap.
function editorWorkflow(api) {
  const nodes = new Map();
  const element = selector => {
    if (!nodes.has(selector)) nodes.set(selector, { value: '', textContent: '', disabled: false,
      classList: { add() {}, remove() {} } });
    return nodes.get(selector);
  };
  element('#fileSiteSelect').value = 'site1';
  const errors = [];
  const app = fs.readFileSync(path.join(__dirname, '..', 'web', 'app.js'), 'utf8');
  const functions = app.slice(app.indexOf('  async function openEditor('), app.indexOf('  async function createDatabase('));
  const editor = vm.runInNewContext(`let editorFilePath='',editorSiteID='',editorRequest=0,editorSaving=false,editorOriginal='';
    ${functions}
    ({openEditor,closeEditor,saveEditor,snapshot:()=>({path:editorFilePath,site:editorSiteID,saving:editorSaving})})`, {
    $: element, api, window: {}, encodeURIComponent, confirm: () => true,
    loadFiles: async () => {}, showToast() {}, handleError: error => errors.push(error.message)
  });
  return { ...editor, element, errors };
}

test('file editor discards a response after the website selection changes', async () => {
  const pending = deferred();
  const editor = editorWorkflow(() => pending.promise);
  const opening = editor.openEditor('config.php');
  editor.element('#fileSiteSelect').value = 'site2';
  pending.resolve({ content: 'private site1 content' });
  await opening;
  assert.equal(editor.snapshot().path, '');
  assert.equal(editor.element('#editorContent').value, '');
});

test('file editor saves to its captured website and stays open during the save', async () => {
  const pending = deferred(), writes = [];
  const editor = editorWorkflow((url, options) => {
    if (!options) return Promise.resolve({ content: 'original' });
    writes.push({ url, body: options.body });
    return pending.promise;
  });
  await editor.openEditor('config.php');
  editor.element('#fileSiteSelect').value = 'site2';
  editor.element('#editorContent').value = 'edited';
  const event = { preventDefault() {}, currentTarget: {} };
  const saving = editor.saveEditor(event);
  editor.closeEditor();
  await editor.saveEditor(event);
  assert.equal(writes.length, 1);
  assert.equal(writes[0].body.siteId, 'site1');
  assert.equal(writes[0].body.path, 'config.php');
  assert.equal(writes[0].body.content, 'edited');
  assert.equal(editor.snapshot().path, 'config.php');
  assert.equal(editor.snapshot().saving, true);
  pending.resolve({});
  await saving;
  assert.equal(editor.snapshot().saving, false);
  assert.deepEqual(editor.errors, []);
});

test('file editor keeps the newest open request when responses arrive out of order', async () => {
  const requests = [deferred(), deferred()];
  let index = 0;
  const editor = editorWorkflow(() => requests[index++].promise);
  const first = editor.openEditor('first.php'), second = editor.openEditor('second.php');
  requests[1].resolve({ content: 'newest content' });
  await second;
  requests[0].resolve({ content: 'stale content' });
  await first;
  assert.equal(editor.snapshot().path, 'second.php');
  assert.equal(editor.element('#editorContent').value, 'newest content');
});

test('mailbox creation sends one validated backend action without a separate domain change', async () => {
  const app = fs.readFileSync(path.join(__dirname, '..', 'web', 'app.js'), 'utf8');
  const functions = app.slice(app.indexOf('  async function createMailbox('), app.indexOf('  async function createMailAlias('));
  const calls = [], errors = [];
  const create = vm.runInNewContext(`${functions};createMailbox`, {
    FormData: class { constructor(form) { this.values = form.values; } get(name) { return this.values[name]; } },
    api: async (url, options) => { calls.push({ url, body: options.body }); throw new Error('Invalid password'); },
    handleError: error => errors.push(error.message), renderAll() {}, showToast() {}
  });
  await create({ preventDefault() {}, currentTarget: { values: {
    domain: 'Example.TEST ', localPart: ' Admin ', password: 'short', quotaMb: '100'
  } } });
  assert.equal(calls.length, 1);
  assert.equal(calls[0].url, '/api/mail/mailboxes');
  assert.equal(calls[0].body.domain, 'example.test');
  assert.equal(calls[0].body.localPart, 'admin');
  assert.deepEqual(errors, ['Invalid password']);
});
