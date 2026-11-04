const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');

function panel(api) {
  let dialog;
  class Element {
    constructor(tag) { this.children = new Map(); this.listeners = {}; this.disabled = false; this.open = false; this.value = ''; this.classList = { remove() {}, add() {} }; if (tag === 'form') this.elements = { username: new Element(), password: new Element() }; }
    querySelector(key) { if (!this.children.has(key)) this.children.set(key, new Element(key === 'form' ? 'form' : 'button')); return this.children.get(key); }
    querySelectorAll() { return [this.querySelector('form').elements.username, this.querySelector('form').elements.password, this.querySelector('[type=submit]'), this.querySelector('[data-ftp-close]')]; }
    addEventListener(type, fn) { (this.listeners[type] ||= []).push(fn); }
    async dispatch(type, event) { await Promise.all((this.listeners[type] || []).map(fn => fn(event))); }
    showModal() { this.open = true; }
    close() { this.open = false; }
    reportValidity() { return true; }
    reset() { dialog.dispatch('reset', {}); this.elements.username.value = ''; this.elements.password.value = ''; }
    focus() {}
  }
  const table = new Element(); const credentials = [];
  const document = { body: { append() {} }, createElement() { return dialog = new Element(); }, querySelector() { return table; }, querySelectorAll() { return []; } };
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, '../web/website-ftp.js'), 'utf8'), { document, window: { LiteCP: { api, escapeHTML: String, state: () => null, showOneTimeCredentials: (...args) => credentials.push(args) }, LiteCPPasswordCopy: { enhance() {} }, LiteCPFeatures: { render() {} } } });
  return { dialog, credentials, open: () => table.dispatch('click', { target: { closest: () => ({ dataset: { siteFtp: 'selected' } }) } }), submit: () => dialog.dispatch('submit', { preventDefault() {}, target: dialog.querySelector('form') }) };
}
const connection = available => ({ available, items: [], host: 'server.test', port: 21, protocol: 'FTPS', directory: '/home/selected' });

test('FTP cannot create before connection loads or when service is unavailable', async () => {
  let resolve; let writes = 0;
  const ui = panel((url, options) => options ? (++writes, Promise.resolve({})) : new Promise(done => resolve = done));
  const opening = ui.open();
  assert.equal(ui.dialog.querySelector('form').elements.username.disabled, true);
  await ui.submit(); assert.equal(writes, 0);
  resolve(connection(false)); await opening; await ui.submit();
  assert.equal(writes, 0);
  assert.equal(ui.dialog.querySelector('[data-ftp-close]').disabled, false);
});

test('FTP successful creation shows credentials without a fallible list refresh', async () => {
  let reads = 0;
  const ui = panel(async (url, options) => { if (options) return {}; if (++reads > 1) throw Error('list unavailable'); return connection(true); });
  await ui.open();
  const fields = ui.dialog.querySelector('form').elements;
  fields.username.value = 'ftp_test'; fields.password.value = 'test-only-password';
  await ui.submit();
  assert.equal(reads, 1); assert.equal(ui.credentials.length, 1);
  assert.equal(ui.credentials[0][2].find(row => row[0] === 'Password')[1], 'test-only-password');
  assert.equal(ui.dialog.open, false);
});

test('failed connection keeps FTP writes disabled', async () => {
  let writes = 0;
  const ui = panel(async (url, options) => { if (options) { writes++; return {}; } throw Error('offline'); });
  await ui.open(); await ui.submit();
  assert.equal(writes, 0);
  assert.equal(ui.dialog.querySelector('[data-ftp-status]').textContent, 'offline');
});
