const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

// Exercise the actual jobs renderer without initializing source-connection forms.
const source = fs.readFileSync(__dirname + '/live-migration.js', 'utf8');
const renderer = source.slice(source.indexOf('  const active=job=>'), source.indexOf('  async function loadJobs()'));

function render(job) {
  const nodes = new Map();
  const element = selector => {
    if (!nodes.has(selector)) nodes.set(selector, {});
    return nodes.get(selector);
  };
  const context = vm.createContext({
    input: job,
    $: element,
    page: { querySelector: element },
    document: { querySelectorAll: () => [] },
    location: { hash: '#migrations/pending' },
    esc: value => String(value ?? '').replace(/[&<>"']/g, char => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[char])),
    cp: { formatBytes: value => String(value) }
  });
  vm.runInContext('let jobs=[input]; const expandedJobs=new Set(),accountSelections=new Map();\n' + renderer + '\nrenderJobs(); globalThis.retryAction=pendingRetryAction(input);', context);
  return { html: element('#liveJobs').innerHTML, retryAction: context.retryAction };
}

const healthReview = {
  id: 'old-job', status: 'restored-review', account: 'source',
  source: { platform: 'cpanel', host: 'source.example.test' },
  accounts: [{ id: 'source', domain: 'example.test' }],
  credentialsRetained: true, nativeReady: true, cpanelWebsiteId: 'deleted-site'
};

test('post-restore review offers a fresh capture when source credentials remain', () => {
  const result = render(healthReview);
  assert.match(result.html, /data-live-retry="old-job">Retry capture<\/button>/);
  assert.equal(result.retryAction, 'restore', 'bulk retry must retain the existing health-check path');
});

test('fresh capture remains available after artifacts are cleaned and disappears without credentials', () => {
  assert.match(render({ ...healthReview, artifactsCleaned: true }).html, /data-live-retry="old-job"/);
  const result = render({ ...healthReview, credentialsRetained: false });
  assert.doesNotMatch(result.html, /data-live-retry="/);
});

test('post-restore review does not widen the committed-site health retry guard', () => {
  for (const change of [{ cpanelWebsiteId: '' }, { source: { platform: 'plesk', host: 'source.example.test' } }, { artifactsCleaned: true }]) {
    const result = render({ ...healthReview, ...change });
    assert.equal(result.retryAction, null);
    assert.doesNotMatch(result.html, /data-live-auto="/);
    assert.match(result.html, /data-live-retry="old-job"/);
  }
});
