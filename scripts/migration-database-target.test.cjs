const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const source = fs.readFileSync(path.join(__dirname, '../web/features.js'), 'utf8');
const selectorCode = source.slice(source.indexOf('  function syncMigrationTargetDatabases('), source.indexOf('  $("#migrationUploadForm").addEventListener("submit"'));
const submitStart = source.indexOf('  $("#migrationImportForm").addEventListener("submit"');
const submitCode = source.slice(submitStart, source.indexOf('  $("#migrationDiscardButton").addEventListener', submitStart));

function fixture(databases, options = {}) {
  const nodes = new Map();
  const node = key => {
    if (!nodes.has(key)) nodes.set(key, {
      value: '', dataset: {}, innerHTML: '', textContent: '', disabled: false,
      required: false, handlers: {}, addEventListener(type, fn) { this.handlers[type] = fn; },
      focus() { this.focused = true; }
    });
    return nodes.get(key);
  };
  const state = {databases};
  const form = node('#migrationImportForm');
  form.elements = {
    siteId: node('#migrationSite'), sql: node('#migrationSQL'),
    databaseID: node('#migrationTargetDatabase'),
    root: node('#migrationRoot')
  };
  form.elements.siteId.value = 'site-one';
  form.elements.sql.value = 'mysql/source.sql';
  form.elements.root.value = 'account/homedir/public_html';
  const requests = [], confirmations = [], messages = [];
  let refreshes = 0;
  const context = vm.createContext({
    $: node, cp: {state: () => state, api: async (url, request) => {
      requests.push(JSON.parse(JSON.stringify({url, ...request})));
      return {note: 'Import completed'};
    }, refresh: async () => { refreshes++; }},
    esc: value => String(value).replaceAll('&', '&amp;').replaceAll('"', '&quot;').replaceAll('<', '&lt;').replaceAll('>', '&gt;'),
    window: {LiteCPDialogs: {confirm: async text => { confirmations.push(text); return options.confirm !== false; }}},
    output: (id, value) => messages.push(value), busy: async (_, action) => action(),
    FormData: class {
      constructor(form) { this.values = Object.fromEntries(Object.entries(form.elements).filter(([, element]) => !element.disabled).map(([name, element]) => [name, element.value])); }
      get(name) { return this.values[name] ?? null; }
    }
  });
  vm.runInContext('let preview = {id:"stage-one"};\n' + selectorCode + submitCode + '\nthis.sync = syncMigrationTargetDatabases;', context);
  context.sync();
  return {node, form, state, requests, confirmations, messages, sync: () => context.sync(),
    submit: () => form.handlers.submit({currentTarget: form, preventDefault() {}}),
    refreshes: () => refreshes};
}

const databases = () => [
  {id: 'primary', siteId: 'site-one', name: 'primary_db'},
  {id: 'secondary', siteId: 'site-one', name: 'secondary_db'},
  {id: 'foreign', siteId: 'site-two', name: 'foreign_db'},
  {id: 'standalone', name: 'standalone_db'}
];

test('target choices include every database linked to the selected website only', () => {
  const ui = fixture(databases());
  const target = ui.form.elements.databaseID;
  assert.match(target.innerHTML, /primary_db/);
  assert.match(target.innerHTML, /secondary_db/);
  assert.doesNotMatch(target.innerHTML, /foreign_db|standalone_db/);
  assert.equal(target.value, '');
  assert.equal(target.disabled, false);
  assert.equal(target.required, true);
});

test('database IDs and names are safely escaped in options', () => {
  const ui = fixture([{id: 'db"<id>', siteId: 'site-one', name: '<img src=x onerror=alert(1)>'}]);
  const html = ui.form.elements.databaseID.innerHTML;
  assert.match(html, /db&quot;&lt;id&gt;/);
  assert.match(html, /&lt;img/);
  assert.doesNotMatch(html, /<img/);
});

test('changing websites clears the previous database selection', () => {
  const ui = fixture(databases());
  ui.form.elements.databaseID.value = 'secondary';
  ui.form.elements.siteId.value = 'site-two';
  ui.form.elements.siteId.handlers.change();
  assert.equal(ui.form.elements.databaseID.value, '');
  assert.match(ui.form.elements.databaseID.innerHTML, /foreign_db/);
  assert.doesNotMatch(ui.form.elements.databaseID.innerHTML, /secondary_db/);
});

test('refresh preserves a valid selection but clears removed targets', () => {
  const ui = fixture(databases());
  ui.form.elements.databaseID.value = 'secondary';
  ui.sync();
  assert.equal(ui.form.elements.databaseID.value, 'secondary');
  ui.state.databases = ui.state.databases.filter(database => database.id !== 'secondary');
  ui.sync();
  assert.equal(ui.form.elements.databaseID.value, '');
});

test('files-only mode disables SQL credentials and submits no database target', async () => {
  const ui = fixture(databases());
  ui.form.elements.databaseID.value = 'secondary';
  ui.form.elements.sql.value = '';
  ui.form.elements.sql.handlers.change();
  assert.equal(ui.form.elements.databaseID.disabled, true);
  assert.equal(ui.form.elements.databaseID.required, false);
  await ui.submit();
  assert.equal(ui.requests[0].body.databaseID, '');
  assert.equal(Object.hasOwn(ui.requests[0].body, 'password'), false);
  assert.equal(ui.requests[0].body.sql, '');
});

test('SQL import requires a selected database linked to this website', async () => {
  for (const databaseID of ['', 'foreign', 'missing', 'standalone']) {
    const ui = fixture(databases());
    ui.form.elements.databaseID.value = databaseID;
    await ui.submit();
    assert.equal(ui.requests.length, 0);
    assert.equal(ui.confirmations.length, 0);
    assert.match(ui.messages.at(-1), /belonging to the selected website/);
  }
});

test('a website without linked databases explains the prerequisite and blocks SQL import', async () => {
  const ui = fixture([]);
  assert.equal(ui.form.elements.databaseID.disabled, true);
  assert.match(ui.node('#migrationDatabaseHint').textContent, /No databases are linked/);
  await ui.submit();
  assert.equal(ui.requests.length, 0);
});

test('import submits the explicitly selected secondary database without credentials', async () => {
  const ui = fixture(databases());
  ui.form.elements.databaseID.value = 'secondary';
  await ui.submit();
  assert.deepEqual(ui.requests, [{url: '/api/migrations/import', method: 'POST', body: {
    id: 'stage-one', siteId: 'site-one', root: 'account/homedir/public_html',
    sql: 'mysql/source.sql', databaseID: 'secondary'
  }}]);
  assert.match(ui.confirmations[0], /secondary_db/);
  assert.equal(ui.refreshes(), 1);
});

test('cancelling confirmation leaves data and database selection untouched', async () => {
  const ui = fixture(databases(), {confirm: false});
  ui.form.elements.databaseID.value = 'secondary';
  await ui.submit();
  assert.equal(ui.requests.length, 0);
});
