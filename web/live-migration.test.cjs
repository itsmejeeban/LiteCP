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
