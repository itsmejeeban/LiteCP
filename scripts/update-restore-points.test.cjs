const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const code = fs.readFileSync('web/update-restore-points.js', 'utf8');
const apiPath = '/api/updates/restore-points';
const points = [{id: 'update-abcd1234', version: '6.0.14', savedAt: '2026-11-10T12:00:00Z', sizeBytes: 3000, state: 'verified'}];

function fixture({accepted = true, warning = ''} = {}) {
  const calls = [], prompts = [];
  const save = {}, limit = {}, refresh = {}, status = {};
  const list = {querySelectorAll: () => [], addEventListener(_event, listener) { this.click = listener; }};
  const form = {
    elements: {limit}, querySelector: () => save,
    addEventListener(_event, listener) { this.submit = listener; }
  };
  const elements = {
    '#updateRestoreRetentionForm': form,
    '#refreshUpdateRestorePoints': refresh,
    '#updateRestorePointsStatus': status,
    '#updateRestorePointsList': list
  };
  const app = {classList: {contains: () => false}};
  const card = {querySelector: selector => elements[selector]};
  refresh.addEventListener = () => {};
  const cp = {
    api: async (url, options) => {
      calls.push([url, options?.method, options?.body?.limit]);
      if (options?.method === 'PUT') return {limit: options.body.limit, points: [], warning};
      if (options?.method === 'DELETE') return {limit: 3, points: []};
      return {limit: 3, points};
    },
    escapeHTML: String, formatBytes: () => '3 KB', formatDateTime: () => 'Nov 10'
  };
  vm.runInNewContext(code, {
    window: {
      LiteCP: cp, LiteCPFeatures: {}, addEventListener() {},
      LiteCPDialogs: {confirm: async (message, options) => {
        prompts.push([message, options.danger]);
        return accepted;
      }}
    },
    document: {querySelector: selector => selector === '#appView' ? app : card},
    location: {hash: '#settings/update'},
    MutationObserver: class {observe() {}}
  });
  return {calls, prompts, list, status, form, limit};
}

const settled = () => new Promise(resolve => setImmediate(resolve));

test('lists saved update restore points and asks before permanently deleting one', async () => {
  const ui = fixture();
  await settled();
  assert.equal(ui.limit.value, 3);
  assert.match(ui.list.innerHTML, /LiteCP 6\.0\.14/);
  assert.match(ui.list.innerHTML, /3 KB/);
  const button = {dataset: {deletePoint: 'update-abcd1234'}};
  ui.list.click({target: {closest: () => button}});
  await settled();
  assert.deepEqual(ui.prompts, [['Permanently delete restore point update-abcd1234 and its recovery files?', true]]);
  assert.deepEqual(ui.calls, [[apiPath, undefined, undefined], [apiPath + '/update-abcd1234', 'DELETE', undefined]]);
  assert.equal(ui.status.textContent, 'Restore point deleted.');
});

test('lowering the limit requires confirmation and displays retention warnings', async () => {
  for (const accepted of [false, true]) {
    const ui = fixture({accepted, warning: 'Cleanup needs attention.'});
    await settled();
    ui.limit.valueAsNumber = 0;
    ui.form.submit({preventDefault() {}});
    await settled();
    assert.equal(ui.prompts.length, 1);
    if (accepted) {
      assert.deepEqual(ui.calls.at(-1), [apiPath, 'PUT', 0]);
      assert.equal(ui.status.textContent, 'Cleanup needs attention.');
    } else {
      assert.equal(ui.calls.length, 1);
      assert.equal(ui.limit.value, 3);
    }
  }
});
