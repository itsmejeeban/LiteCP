const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

function source(name) {
  return fs.readFileSync(path.join(__dirname, '../web', name), 'utf8');
}

function node(initialClasses = []) {
  const classes = new Set(initialClasses);
  const children = new Map();
  const handlers = {};
  return {
    children, handlers, dataset: {}, textContent: '', innerHTML: '', hidden: false,
    classList: {
      add: (...names) => names.forEach(name => classes.add(name)),
      remove: (...names) => names.forEach(name => classes.delete(name)),
      contains: name => classes.has(name),
      toggle(name, enabled) { if (enabled) classes.add(name); else classes.delete(name); }
    },
    setAttribute() {}, removeAttribute() {}, before() {}, scrollIntoView() {},
    addEventListener(type, handler) { handlers[type] = handler; },
    querySelector(selector) {
      if (!children.has(selector)) children.set(selector, node());
      return children.get(selector);
    },
    querySelectorAll() { return []; },
    replaceChildren() { this.innerHTML = ''; this.selected = null; }
  };
}

function folderFixture(api) {
  const page = node();
  page.dataset.migrationSection = 'folder';
  const panel = node();
  const form = panel.querySelector('[data-folder-scan]');
  form.elements = {platform: {value: 'cpanel'}, folder: {value: '/var/backups/migration'}};
  const rows = panel.querySelector('[data-folder-files]');
  rows.querySelector = () => rows.selected;
  const reviews = [], events = [];
  const location = {hash: '#migrations/folder'};
  const cp = {
    api,
    escapeHTML: value => String(value).replaceAll('<', '&lt;').replaceAll('"', '&quot;'),
    formatDateTime: String,
    formatBytes: String
  };
  vm.runInNewContext(source('migration-folder.js'), {
    location, Event,
    window: {LiteCP: cp, LiteCPFeatures: {reviewMigration: preview => reviews.push(preview)}},
    document: {querySelector: () => page, createElement: () => panel, dispatchEvent: event => events.push(event.type), addEventListener() {}}
  });
  return {page, panel, form, rows, reviews, events, location};
}

test('folder backup selection preserves scanned metadata and opens the existing review form', async () => {
  const requests = [];
  const items = [{name: '<unsafe>.tar.gz', size: 81, modifiedAt: '2026-11-10T01:02:03.456789Z'}];
  const preview = {id: 'stage', roots: ['public_html'], databases: ['account.sql']};
  let finishInspection;
  const ui = folderFixture(async (url, options) => {
    requests.push([url, options.method, JSON.parse(JSON.stringify(options.body))]);
    return url.endsWith('/scan') ? {items} : new Promise(resolve => { finishInspection = resolve; });
  });
  assert.equal(ui.panel.hidden, false);
  await ui.form.handlers.submit({preventDefault() {}});
  assert.match(ui.rows.innerHTML, /&lt;unsafe>\.tar\.gz/);
  assert.doesNotMatch(ui.rows.innerHTML, /<unsafe>/);
  const inspect = ui.panel.querySelector('[data-folder-inspect]');
  assert.equal(inspect.disabled, true);
  ui.rows.selected = {value: '0'};
  ui.rows.handlers.change();
  assert.equal(inspect.disabled, false);
  const inspection = inspect.handlers.click();
  const progress = ui.panel.querySelector('[data-folder-progress]');
  assert.equal(progress.hidden, false);
  assert.equal(inspect.disabled, true);
  finishInspection(preview);
  await inspection;
  assert.equal(progress.hidden, true);
  const scan = {platform: 'cpanel', folder: '/var/backups/migration'};
  assert.deepEqual(requests, [
    ['/api/migrations/folder/scan', 'POST', scan],
    ['/api/migrations/folder/restore', 'POST', {...scan, ...items[0]}]
  ]);
  assert.equal(ui.page.dataset.folderReview, 'true');
  assert.deepEqual(ui.reviews, [preview]);
  assert.deepEqual(ui.events, ['migration-show-review']);
});

test('changing the folder invalidates an outstanding scan and its selections', async () => {
  let resolveScan;
  const ui = folderFixture(() => new Promise(resolve => { resolveScan = resolve; }));
  const scan = ui.form.handlers.submit({preventDefault() {}});
  ui.form.elements.folder.value = '/new/folder';
  ui.form.handlers.input();
  resolveScan({items: [{name: 'previous.zip', size: 81, modifiedAt: '2026-11-10T01:02:03Z'}]});
  await scan;
  assert.equal(ui.rows.innerHTML, '');
  assert.equal(ui.panel.querySelector('[data-folder-results]').hidden, true);
  assert.equal(ui.panel.querySelector('[data-folder-inspect]').disabled, true);
  assert.equal(ui.panel.querySelector('[data-folder-scan-button]').disabled, false);
});

test('a staging-space rejection leaves an actionable error beside the inspect button', async () => {
  const detail = 'The staging filesystem needs 8.00 GiB free, but only 3.00 GiB is available.';
  const ui = folderFixture(async url => {
    if (url.endsWith('/scan')) return {items: [{name: 'backup.tar.gz', size: 81, modifiedAt: '2026-11-10T01:02:03Z'}]};
    throw new Error(detail);
  });
  await ui.form.handlers.submit({preventDefault() {}});
  ui.rows.selected = {value: '0'};
  ui.rows.handlers.change();
  const inspect = ui.panel.querySelector('[data-folder-inspect]');
  await inspect.handlers.click();
  assert.equal(ui.panel.querySelector('[data-folder-status]').textContent, `Restore did not start. ${detail}`);
  assert.equal(ui.panel.querySelector('[data-folder-progress]').hidden, true);
  assert.equal(inspect.disabled, false);
  assert.deepEqual(ui.reviews, []);
});

test('folder navigation hides an existing upload review without resetting it', () => {
  const script = source('migration-tabs.js');
  const code = script.slice(script.indexOf('  let lastRoute='), script.indexOf('  const previous='));
  const page = node(), sourceForm = node(), upload = node(), importForm = node(), output = node();
  const location = {hash: '#migrations/upload'};
  const entries = ['cpanel', 'upload', 'folder', 'jobs'].map(part => Object.assign([part, part], {link: node()}));
  const context = vm.createContext({location, entries, page, source: sourceForm, upload, importForm, output,
    jobs: node(), database: node(), toggle: node(), litecp: node(), expand() {}, Event,
    document: {dispatchEvent() {}}
  });
  importForm.dataset.preview = 'previous-upload';
  vm.runInContext(code, context);
  context.sync();
  assert.equal(upload.classList.contains('hidden'), false);
  assert.equal(importForm.classList.contains('hidden'), false);
  location.hash = '#migrations/folder';
  context.sync();
  assert.equal(upload.classList.contains('hidden'), true);
  assert.equal(importForm.classList.contains('hidden'), true);
  assert.equal(output.classList.contains('migration-output-hidden'), true);
  assert.equal(importForm.dataset.preview, 'previous-upload');
  location.hash = '#migrations/upload';
  context.sync();
  assert.equal(importForm.classList.contains('hidden'), false);
  assert.equal(output.classList.contains('migration-output-hidden'), false);
  assert.equal(importForm.dataset.preview, 'previous-upload');
});
