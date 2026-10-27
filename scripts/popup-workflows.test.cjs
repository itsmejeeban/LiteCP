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

function popup(kind, api) {
  const roots = new Map();
  let dialog, refreshes = 0;
  class Element {
    constructor(tag = 'div') {
      this.tagName = tag;
      this.dataset = {};
      this.children = new Map();
      this.listeners = new Map();
      this.elements = { username: { value: 'ftp_test' }, password: { value: 'long-test-password' } };
      this.textContent = '';
      this.disabled = false;
      this.open = false;
      this.resets = 0;
      this.classList = { add() {}, remove() {} };
      this.controls = [];
    }
    set innerHTML(value) {
      this.html = value;
      this.children.clear();
      this.controls = [new Element('button')];
    }
    get innerHTML() { return this.html || ''; }
    querySelector(selector) {
      if (!this.children.has(selector)) {
        const child = new Element(selector === 'form' ? 'form' : 'div');
        child.parent = this;
        this.children.set(selector, child);
      }
      return this.children.get(selector);
    }
    querySelectorAll(selector) { return selector.includes('button') ? this.controls : []; }
    addEventListener(name, callback) {
      if (!this.listeners.has(name)) this.listeners.set(name, []);
      this.listeners.get(name).push(callback);
    }
    async dispatch(name, event) {
      const results = (this.listeners.get(name) || []).map(callback => callback(event));
      await Promise.all(results);
    }
    showModal() { this.open = true; }
    close() { this.open = false; }
    append() {}
    prepend() {}
    setAttribute() {}
    reportValidity() { return true; }
    matches(selector) { return selector === this.selector; }
    reset() {
      this.resets++;
      if (this.parent) this.parent.dispatch('reset', { target: this });
    }
  }
  const document = {
    body: new Element('body'),
    createElement(tag) { const element = new Element(tag); if (tag === 'dialog') dialog = element; return element; },
    querySelector(selector) { if (!roots.has(selector)) roots.set(selector, new Element()); return roots.get(selector); },
    querySelectorAll() { return []; }
  };
  const cp = {
    api, escapeHTML: String, formatDateTime: String, formatBytes: String,
    state: () => ({ sites: [{ id: 'A', domain: 'a.test' }, { id: 'B', domain: 'b.test' }], databases: [] }),
    refresh: async () => { refreshes++; }
  };
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, '..', 'web', `website-${kind}.js`), 'utf8'), {
    document, window: { LiteCP: cp, LiteCPFeatures: { render() {} } }, confirm: () => true,
    FormData: class { constructor(form) { this.values = form.values || {}; } get(name) { return this.values[name] || null; } }
  });
  function click(selector, dataset = {}) {
    return dialog.dispatch('click', { target: { closest: value => value === selector ? { dataset } : null } });
  }
  return {
    dialog, cp,
    open: site => document.querySelector('#sitesTable').dispatch('click', {
      target: { closest: selector => selector === `[data-site-${kind === 'backups' ? 'backup' : 'ftp'}]` ? {
        dataset: kind === 'backups' ? { siteBackup: site } : { siteFtp: site }
      } : null }
    }),
    close: () => click(kind === 'backups' ? '[data-backup-close]' : '[data-ftp-close]'),
    click,
    submit: (selector, values = {}) => {
      const form = kind === 'ftp' ? dialog.querySelector('form') : new Element('form');
      form.selector = selector; form.values = values;
      return dialog.dispatch('submit', { target: form, preventDefault() {} });
    },
    refreshes: () => refreshes
  };
}

const backupList = id => ({ items: [{ id, createdAt: id, size: 1 }] });
const ftpList = id => ({ available: true, protocol: 'FTPS', host: id, port: 21,
  directory: `/public/${id}`, items: [{ id, username: id }] });
const preview = id => ({ id, files: 1, bytes: 1, databases: [] });

for (const kind of ['backups', 'ftp']) {
  const result = kind === 'backups' ? backupList : ftpList;
  const listSelector = kind === 'backups' ? '[data-backup-list]' : '[data-ftp-list]';
  const statusSelector = kind === 'backups' ? '[data-backup-status]' : '[data-ftp-status]';

  test(`${kind}: a closed/reopened popup ignores the previous website's list`, async () => {
    const requests = [deferred(), deferred()];
    let index = 0;
    const ui = popup(kind, () => requests[index++].promise);
    const first = ui.open('A');
    await ui.close();
    const second = ui.open('B');
    requests[1].resolve(result('B'));
    await second;
    requests[0].resolve(result('A'));
    await first;
    assert.match(ui.dialog.querySelector(listSelector).innerHTML, /B/);
    assert.doesNotMatch(ui.dialog.querySelector(listSelector).innerHTML, />A</);
    if (kind === 'backups') assert.match(ui.dialog.querySelector(listSelector).innerHTML, /siteId=B/);
    else assert.match(ui.dialog.querySelector('[data-ftp-connection]').textContent, /\/public\/B/);
  });

  test(`${kind}: reopening the same website rejects old errors and closed responses`, async () => {
    const requests = [deferred(), deferred(), deferred()];
    let index = 0;
    const ui = popup(kind, () => requests[index++].promise);
    const first = ui.open('A');
    await ui.close();
    const second = ui.open('A');
    requests[1].resolve(result('new'));
    await second;
    requests[0].reject(new Error('Old request failed'));
    await first;
    assert.equal(ui.dialog.querySelector(statusSelector).textContent, '');
    const third = ui.open('B');
    await ui.close();
    requests[2].resolve(result('closed'));
    await third;
    assert.equal(ui.dialog.querySelector(listSelector).innerHTML, '');
  });

  for (const outcome of ['success', 'error']) test(`${kind}: an old mutation ${outcome} cannot refresh or unlock the newer popup's mutation`, async () => {
    const pending = [deferred(), deferred()], writes = [];
    const ui = popup(kind, (url, options) => {
      if (!options) return Promise.resolve(result(url.includes('siteId=A') ? 'A' : 'B'));
      writes.push({ url, options });
      return pending[writes.length - 1].promise;
    });
    await ui.open('A');
    const act = () => kind === 'backups' ? ui.click('[data-backup-create]') : ui.submit('[data-ftp-form]');
    const first = act();
    // Closing through normal controls is blocked while busy; reopening can still
    // happen when another UI action replaces the popup. Simulate that replacement.
    ui.dialog.close();
    await ui.open('B');
    const second = act();
    const currentControl = ui.dialog.controls[0];
    const activeStatus = ui.dialog.querySelector(statusSelector).textContent;
    if (outcome === 'error') pending[0].reject(new Error('Old mutation failed'));
    else pending[0].resolve({});
    await first;
    assert.equal(currentControl.disabled, true);
    assert.equal(ui.dialog.querySelector(statusSelector).textContent, activeStatus);
    assert.equal(ui.refreshes(), 0);
    assert.match(writes[0].url, /siteId=A/);
    assert.match(writes[1].url, /siteId=B/);
    pending[1].resolve({});
    await second;
    assert.equal(currentControl.disabled, false);
    assert.match(ui.dialog.querySelector(statusSelector).textContent, kind === 'backups' ? /Backup ready/ : /FTP account updated/);
  });
}

for (const action of ['inspect', 'upload', 'restore']) {
  test(`backups: obsolete ${action} completion cannot replace review or refresh the current website`, async () => {
    const pending = deferred(), writes = [];
    let delaying = false;
    const ui = popup('backups', (url, options) => {
      if (!options) return Promise.resolve(backupList(url.includes('siteId=A') ? 'A' : 'B'));
      writes.push({ url, options });
      return delaying ? pending.promise : Promise.resolve(preview('stage-A'));
    });
    await ui.open('A');
    if (action === 'restore') await ui.click('[data-backup-inspect]', { backupInspect: 'A' });
    delaying = true;
    const work = action === 'inspect' ? ui.click('[data-backup-inspect]', { backupInspect: 'A' }) :
      action === 'upload' ? ui.submit('[data-backup-upload]', { backup: { size: 1 } }) :
        ui.submit('[data-backup-files]', { targetSiteID: 'A' });
    ui.dialog.close();
    await ui.open('B');
    pending.resolve(preview('stale-stage-A'));
    await work;
    assert.equal(ui.dialog.querySelector('[data-backup-review]').innerHTML, '');
    assert.equal(ui.dialog.querySelector('[data-backup-status]').textContent, '');
    assert.equal(ui.refreshes(), 0);
    assert.ok(writes.every(write => write.url.includes('siteId=A')));
  });
}
