const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

// Exercise the actual jobs renderer without initializing source-connection forms.
const source = fs.readFileSync(__dirname + '/live-migration.js', 'utf8');
const renderer = source.slice(source.indexOf('  const active=job=>'), source.indexOf('  async function loadJobs()'));

function render(job, items=[job], hash='#migrations/pending') {
  const nodes = new Map();
  const element = selector => {
    if (!nodes.has(selector)) nodes.set(selector, {});
    return nodes.get(selector);
  };
  const context = vm.createContext({
    input: job,
    items,
    $: element,
    page: { querySelector: element },
    document: { querySelectorAll: () => [] },
    location: { hash },
    esc: value => String(value ?? '').replace(/[&<>"']/g, char => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[char])),
    cp: { formatBytes: value => String(value) }
  });
  vm.runInContext('let jobs=items; const expandedJobs=new Set(),accountSelections=new Map();\n' + renderer + '\nrenderJobs(); globalThis.retryAction=pendingRetryAction(input);', context);
  return { html: element('#liveJobs').innerHTML, retryAction: context.retryAction };
}

const healthReview = {
  id: 'old-job', status: 'restored-review', account: 'source',
  source: { platform: 'cpanel', host: 'source.example.test' },
  accounts: [{ id: 'source', domain: 'example.test' }],
  credentialsRetained: true, nativeReady: true, cpanelWebsiteId: 'deleted-site'
};

test('post-restore review offers a fresh capture when source credentials remain', () => {
  const result = render(healthReview, [healthReview], '#migrations/completed');
  assert.match(result.html, /data-live-retry="old-job">Retry capture<\/button>/);
  assert.equal(result.retryAction, 'restore', 'bulk retry must retain the existing health-check path');
});

test('an older failure moves to history when the same source website has a newer attempt', () => {
  const old={...healthReview,id:'failed-attempt',status:'failed',createdAt:'2026-11-03T23:26:00Z'};
  const latest={...old,id:'new-attempt',status:'awaiting-overwrite',createdAt:'2026-11-03T23:53:00Z'};
  const result=render(old,[old,latest]);
  assert.doesNotMatch(result.html,/data-job-id="failed-attempt"/);
  assert.match(result.html,/data-job-id="new-attempt"/);
  assert.equal(result.retryAction,null);
  const history=render(old,[old,latest],'#migrations/completed');
  assert.match(history.html,/data-job-id="failed-attempt"/);
  assert.match(history.html,/Previous attempt/);
  assert.match(history.html,/data-live-open-job="new-attempt"/);
});

test('overwrite confirmation is visible outside the collapsed details and cannot auto-retry', () => {
  const job={...healthReview,status:'awaiting-overwrite'};
  const result=render(job);
  assert.match(result.html,/Backup copied and verified/);
  assert.ok(result.html.indexOf('data-live-overwrite=')<result.html.indexOf('class="live-job-details"'));
  assert.equal((result.html.match(/data-live-overwrite=/g)||[]).length,1);
  assert.equal(result.retryAction,null);
  assert.doesNotMatch(result.html,/data-live-retry="/);
});

test('a different source server or a cancelled newer attempt does not hide an unresolved failure', () => {
  const old={...healthReview,status:'failed',createdAt:'2026-11-03T23:26:00Z'};
  for(const change of [{source:{...old.source,host:'different.example.test'}},{status:'cancelled'},{status:'connected'}]){
    const newer={...old,id:'different-attempt',createdAt:'2026-11-03T23:53:00Z',...change};
    assert.match(render(old,[old,newer]).html,/data-job-id="old-job"/);
  }
});

test('fresh capture remains available after artifacts are cleaned and disappears without credentials', () => {
  const cleaned = { ...healthReview, artifactsCleaned: true };
  assert.match(render(cleaned, [cleaned], '#migrations/completed').html, /data-live-retry="old-job"/);
  const forgotten = { ...healthReview, credentialsRetained: false };
  const result = render(forgotten, [forgotten], '#migrations/completed');
  assert.doesNotMatch(result.html, /data-live-retry="/);
});

test('source pages show only jobs belonging to that panel',()=>{
  const cpanel={...healthReview,status:'connected'};
  const plesk={...cpanel,id:'plesk-job',source:{platform:'plesk',host:'other.test'}};
  const items=[cpanel,plesk];
  const result=render(cpanel,items,'#migrations/cpanel');
  assert.match(result.html,/data-job-id="old-job"/);
  assert.doesNotMatch(result.html,/data-job-id="plesk-job"/);
});

test('post-restore review does not widen the committed-site health retry guard', () => {
  for (const change of [{ cpanelWebsiteId: '' }, { source: { platform: 'plesk', host: 'source.example.test' } }, { artifactsCleaned: true }]) {
    const job = { ...healthReview, ...change };
    const result = render(job, [job], '#migrations/completed');
    assert.equal(result.retryAction, null);
    assert.doesNotMatch(result.html, /data-live-auto="/);
    assert.match(result.html, /data-live-retry="old-job"/);
  }
});

test('cPanel backups missing required SQL do not offer another restore of the same files', () => {
  const job = { ...healthReview, status: 'restore-review', message: 'Automatic restore stopped: configuration .env has no matching database dump.' };
  const result = render(job);
  assert.equal(result.retryAction, null);
  assert.doesNotMatch(result.html, /data-live-auto="/);
  assert.match(result.html, /Verify the required database exists/);
  assert.match(result.html, /data-live-retry="old-job">Retry capture/);
  assert.equal(render({ ...job, warnings: ['Some account databases could not be exported.'] }).retryAction, 'capture');
  assert.equal(render({ ...job, message: 'Restore needs compatibility review.' }).retryAction, 'restore');
});

test('a confirmed incomplete cPanel export offers recapture even if the missing schema is outside app configuration', () => {
  const job = { ...healthReview, status: 'restore-review', message: 'The source cPanel account backup is missing one or more database dumps.',
    warnings: ['Some account databases could not be exported. Check the source export log before restoring.'] };
  for (const status of ['review', 'captured', 'restore-review', 'awaiting-overwrite']) {
    const result = render({ ...job, status });
    assert.equal(result.retryAction, 'capture');
    assert.match(result.html, /data-live-retry="old-job">Retry capture/);
    assert.doesNotMatch(result.html, /data-live-(?:auto|overwrite)="/);
  }
  assert.equal(render({ ...job, credentialsRetained: false }).retryAction, null);
});

test('missing source database maps show repair guidance and keep recapture available', () => {
  const job = { ...healthReview, status: 'failed', nativeReady: false,
    message: 'cPanel capture blocked: the account is missing from the source cPanel database map; repair the map on the source before retrying. No archive was created.' };
  const result = render(job);
  assert.equal(result.retryAction, 'capture');
  assert.match(result.html, /Repair the account’s database map/);
  assert.match(result.html, /data-live-retry="old-job">Retry capture/);
  assert.equal(render({ ...job, message: 'Temporary SSH timeout.' }).retryAction, 'capture');
});

const missingMap = {
  ...healthReview, status: 'failed', cpanelWebsiteId: '', nativeReady: false,
  message: 'cPanel capture blocked: the account is missing from the source cPanel database map.'
};
const missingDump = {
  ...missingMap, status: 'restore-review', nativeReady: true,
  message: 'Automatic restore stopped: configuration .env has no matching database dump.'
};

test('empty-database retries appear only for eligible individual cPanel jobs', () => {
  for (const job of [missingMap, missingDump]) {
    const { html } = render(job);
    assert.match(html, /data-live-retry-empty="old-job"[^>]*>Retry with empty databases/);
    assert.ok(html.indexOf('data-live-retry-empty=') < html.indexOf('class="live-job-details"'));
    assert.match(html, /data-live-retry="old-job">Retry capture/);
  }
  for (const change of [
    { status: 'awaiting-overwrite' },
    { credentialsRetained: false },
    { cpanelWebsiteId: 'existing-site' },
    { preview: { databases: ['mysql/source.sql'] } },
    { warnings: ['Some account databases could not be exported.'] },
    { source: { ...missingDump.source, platform: 'plesk' } }
  ]) {
    assert.doesNotMatch(render({ ...missingDump, ...change }).html, /data-live-retry-empty/);
  }
});

test('a failed empty-database retry stays visible while its previous attempt remains in history', () => {
  const previous = { ...missingDump, createdAt: '2026-11-03T23:26:00Z' };
  const replacement = {
    ...missingMap, id: 'new-job', recreateEmptyDatabases: true, createdAt: '2026-11-03T23:53:00Z',
    message: 'The account contains databases that require export.'
  };
  const pending = render(previous, [previous, replacement]).html;
  assert.doesNotMatch(pending, /data-job-id="old-job"/);
  assert.match(pending, /data-job-id="new-job"/);
  const history = render(previous, [previous, replacement], '#migrations/completed').html;
  assert.match(history, /data-live-open-job="new-job"/);
  assert.doesNotMatch(history, /data-live-retry-empty/);
});

test('explicit confirmation is required before retrying the selected account with empty databases', async () => {
  const handler = source.slice(source.indexOf('  async function retryWithEmptyDatabases'), source.indexOf('  function pickAccount'));
  const calls = [];
  let consent = false;
  const context = vm.createContext({
    jobs: [missingMap],
    retryingJobs: new Set(),
    retryPendingBusy: false,
    canRetryWithEmptyDatabases: job => job?.credentialsRetained,
    renderJobs() {},
    async loadJobs() {},
    window: { LiteCPDialogs: { async confirm(message) {
      assert.match(message, /example\.test.*contain no data/);
      return consent;
    } } },
    cp: { async api(path, options) {
      calls.push([path, options]);
      return { status: 'queued' };
    }, showToast() {}, handleError(error) { throw error; } }
  });
  vm.runInContext(handler, context);
  await context.retryWithEmptyDatabases('old-job');
  assert.equal(calls.length, 0);
  consent = true;
  await context.retryWithEmptyDatabases('old-job');
  assert.deepEqual(JSON.parse(JSON.stringify(calls)), [['/api/live-migrations', {
    method: 'POST',
    body: { action: 'retry', id: 'old-job', confirm: true, recreateEmptyDatabases: true }
  }]]);
});
