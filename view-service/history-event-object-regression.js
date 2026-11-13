// Replays the prodops-redacted real first history page through direct View and
// the production Console page with four simultaneous renderer iframes.
const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const fs = require('node:fs');
const http = require('node:http');
const path = require('node:path');
const { chromium } = require('playwright');

const ROOT = path.resolve(__dirname, '..');
const FIXTURE = path.join(__dirname, 'testdata/console-history-first-page-20261112.json');
const METADATA = path.join(__dirname, 'testdata/console-history-first-page-20261112.metadata.json');
const EXPECTED_FIXTURE_SHA256 = '1a92da897c3cfbba7cf2641e4bfbbccdf94b823ed98b1efcdbad48d794085a9a';
const EXPECTED_METADATA_SHA256 = '2f7d8e88a05773442c7a31bed30ffa4ec66f1a451305879524c0ee911082acd7';
const SESSION = 'opus50tc2k.zzc';
const SESSIONS = [SESSION, 'ts3opus55.zzc', 'opuszzcgmail.zzc', 'dataconnector.zzc'];
const REPLAY_BASELINE = process.env.HISTORY_REPLAY_BASELINE === '1';
const VERSIONS = REPLAY_BASELINE
  ? [['r57-baseline', '329eb9f9c45d2d785a0429b01eaa0fa5264fe15d'], ['r59-baseline', '8528ed8cf7e43c1bec97102927c92975ed714609']]
  : [['r59-fixed', null]];

function sha256(bytes) {
  return crypto.createHash('sha256').update(bytes).digest('hex');
}
function gitShow(commit, file) {
  return require('node:child_process').execFileSync('git', ['show', `${commit}:${file}`], { cwd: ROOT });
}
const fixtureBytes = fs.readFileSync(FIXTURE);
const metadataBytes = fs.readFileSync(METADATA);
assert.equal(sha256(fixtureBytes), EXPECTED_FIXTURE_SHA256, 'prodops fixture bytes changed');
assert.equal(sha256(metadataBytes), EXPECTED_METADATA_SHA256, 'prodops metadata bytes changed');
const rows = JSON.parse(fixtureBytes);
const metadata = JSON.parse(metadataBytes);
assert.equal(rows.length, 100);
assert.deepEqual(metadata.first_page_seq_range, [3958, 4057]);
const eventGeneration = metadata.generation_row.generation;
const firstPage = {
  ...metadata.first_page_outer_without_events,
  events: rows.map(row => ({
    cursor: { generation: eventGeneration, seq: row.seq },
    event_id: row.event_id,
    received_at: new Date(row.received_at_ms).toISOString(),
    event: row.payload,
  })),
};
assert.deepEqual(Object.keys(firstPage).sort(), metadata.first_page_outer_field_names.slice().sort());
const assistantRows = rows.map(row => row.payload).filter(event => event.kind === 'assistant_text');
assert.equal(assistantRows.length, 22);
const assistantTextChars = assistantRows.reduce((n, event) => n + event.text.length, 0);
const assistantGroups = new Map();
for (const row of rows) {
  const event = row.payload;
  if (event.kind !== 'assistant_text') continue;
  const key = event.turn_id || event.delivery_id;
  assistantGroups.set(key, (assistantGroups.get(key) || '') + event.text);
}
const expectedAssistantGroups = [...assistantGroups.values()].map(text => text.length > 4000 ? text.slice(0, 1200) : text).sort();
const currentRenderer = fs.readFileSync(path.join(__dirname, 'renderer-canary-sse.html'), 'utf8');
const renderers = Object.fromEntries(VERSIONS.map(([name, commit]) => [name, commit ? gitShow(commit, 'view-service/renderer-canary-sse.html').toString() : currentRenderer]));
const parserSource = `    function parseHistoryEvent(value) {
      let ev = value;
      if (typeof value === 'string') {
        try { ev = JSON.parse(value); } catch (_) { return null; }
      }
      return ev && typeof ev === 'object' && !Array.isArray(ev) ? { ...ev } : null;
    }`;
assert(currentRenderer.includes(parserSource), 'history event object parser missing from current renderer');
const mutation = process.env.MUTATE_HISTORY_EVENT_OBJECT || '';
const historySSEMutation = process.env.MUTATE_HISTORY_SSE_INPUT_BUFFER === '1';
const parsedRendererUnderTest = REPLAY_BASELINE ? null : mutation === 'object'
  ? currentRenderer.replace(parserSource, `    function parseHistoryEvent(value) {
      if (typeof value !== 'string') return null;
      try { const ev = JSON.parse(value); return ev && typeof ev === 'object' && !Array.isArray(ev) ? { ...ev } : null; } catch (_) { return null; }
    }`)
  : mutation === 'string'
    ? currentRenderer.replace(parserSource, `    function parseHistoryEvent(value) {
      if (typeof value === 'string') return null;
      return value && typeof value === 'object' && !Array.isArray(value) ? { ...value } : null;
    }`)
    : currentRenderer;
const sseBufferCondition = 'historyBootstrapPending && !historyApplying && (ev?._history || ev?._history_gap || (ev?._hubSeq && ev?._hubEpoch))';
assert(currentRenderer.includes(sseBufferCondition), 'history/SSE bootstrap buffer guard missing');
const rendererUnderTest = historySSEMutation
  ? parsedRendererUnderTest.replace(sseBufferCondition, 'historyBootstrapPending && !historyApplying && (ev?._history || ev?._history_gap)')
  : parsedRendererUnderTest;
if (historySSEMutation) assert.notEqual(rendererUnderTest, parsedRendererUnderTest, 'SSE input buffer mutation did not apply');
const go = fs.readFileSync(path.join(ROOT, 'internal/m8/console_auth.go'), 'utf8');
const htmlStart = 'const consolePageHTML = `';
const htmlEnd = '`\n\nfunc (h *Hub) HandleConsoleAgents';
const start = go.indexOf(htmlStart);
const end = go.indexOf(htmlEnd, start + htmlStart.length);
assert(start >= 0 && end > start, 'Console HTML extraction markers changed');
const consoleHTML = go.slice(start + htmlStart.length, end);
let currentRendererName = 'r59';
let currentResponse = null;
let historyEventMode = 'object';
let replayHistoryAsSSE = false;
const historyRequests = [];
const eventStreams = new Map();
const server = http.createServer((req, res) => {
  const url = new URL(req.url, 'http://127.0.0.1');
  const match = url.pathname.match(/^\/view\/([^/]+)(?:\/(.*))?$/);
  if (url.pathname === '/console') {
    res.writeHead(200, { 'content-type': 'text/html; charset=utf-8', 'cache-control': 'no-store' });
    res.end(consoleHTML);
    return;
  }
  if (url.pathname === '/api/console/agents') {
    res.writeHead(200, { 'content-type': 'application/json' });
    res.end(JSON.stringify({ agents: SESSIONS.map(full_name => ({ agent_id: `id-${full_name}`, name: full_name, full_name, origin: 'selfhost', group_code: '', online: true })) }));
    return;
  }
  if (url.pathname === '/api/console/platform') {
    res.writeHead(200, { 'content-type': 'application/json' });
    res.end('{"role":"owner","actions":[],"items":[]}');
    return;
  }
  if (match) {
    const name = decodeURIComponent(match[1]);
    const endpoint = match[2];
    if (!endpoint) {
      res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' });
      res.end(name === SESSION && !REPLAY_BASELINE ? rendererUnderTest : renderers[currentRendererName]);
      return;
    }
    if (endpoint === 'history') {
      historyRequests.push({ renderer: currentRendererName, name, path: url.pathname, query: url.search });
      let body = name === SESSION ? currentResponse : {
        generation: `synthetic-${name}`, high_water: 1,
        events: [{ cursor: { generation: `synthetic-${name}`, seq: 1 }, event_id: `synthetic-${name}`,
          received_at: new Date().toISOString(), event: {
            kind: 'assistant_text', text: `SYNTHETIC_HISTORY_${name}`, session_id: `synthetic-${name}`,
            turn_id: `synthetic-turn-${name}`, delivery_id: `synthetic-delivery-${name}`,
            origin_delivery_id: `synthetic-origin-${name}`, cursor: '001', ts: 1794494101, data: {},
          } }], gaps: [], has_older: false, before_seq: 1, gap_before: false,
      };
      if (name === SESSION && historyEventMode === 'string') body = { ...body, events: body.events.map(row => ({ ...row, event: JSON.stringify(row.event) })) };
      if (name === SESSION && historyEventMode === 'invalid-row') {
        const bad = { cursor: { generation: eventGeneration, seq: 4000 }, event_id: 'invalid-event-fixture', received_at: new Date().toISOString(), event: null };
        body = { ...body, events: body.events.slice(0, 20).concat(bad, body.events.slice(20)) };
      }
      res.writeHead(200, { 'content-type': 'application/json', 'cache-control': 'no-store' });
      res.end(JSON.stringify(body));
      return;
    }
    if (endpoint === 'events') {
      res.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-cache', connection: 'keep-alive' });
      res.write(': fixture stream open\n\n');
      if (name === SESSION && replayHistoryAsSSE) {
        for (const row of (currentResponse?.events || [])) {
          const event = row.event?.kind === 'user_input' ? row.event : { kind: 'control', text: '' };
          res.write(`id: ${eventGeneration}-${row.cursor.seq}\ndata: ${JSON.stringify(event)}\n\n`);
        }
      }
      eventStreams.set(name, res);
      req.on('close', () => eventStreams.delete(name));
      return;
    }
    if (['status', 'tasks', 'page'].includes(endpoint)) {
      res.writeHead(200, { 'content-type': 'application/json' });
      res.end(JSON.stringify({ session: name, state: 'idle', readonly: true, auth_mode: 'legacy', owner: 'fixture', model: 'fixture' }));
      return;
    }
  }
  res.writeHead(404);
  res.end('not found');
});

async function metrics(frame) {
  return frame.locator('#timeline').evaluate(timeline => {
    const body = Array.from(timeline.querySelectorAll('.turn-assistant')).map(section => {
      const clone = section.cloneNode(true);
      clone.querySelectorAll('.event-meta,.turn-tools,.turn-completed,.longtext-bar').forEach(node => node.remove());
      return clone.textContent || '';
    });
    return { assistantSections: body.length, assistantTextChars: body.join('').length, assistantGroupTexts: body.sort() };
  });
}
async function waitForHistory(frame) {
  await frame.waitForFunction(() => !!document.querySelector('#timeline'), null, { timeout: 10000 });
  await frame.waitForTimeout(2800);
}
async function openSession(page, name) {
  await page.locator(`nav button[title="${name}"]`).click();
  await page.locator(`#panes iframe[title="Agent view: ${name}"]`).waitFor({ state: 'attached', timeout: 10000 });
  await page.waitForFunction(name => Array.from(document.querySelectorAll('#panes iframe')).some(frame => frame.src.includes('/view/' + encodeURIComponent(name))), name);
  return page.frames().find(frame => frame.url().includes('/view/' + encodeURIComponent(name)));
}
async function assertCurrentOutput(frame, location) {
  const actual = await metrics(frame);
  if (REPLAY_BASELINE) {
    assert.equal(actual.assistantSections, 0, `${location}: baseline unexpectedly rendered assistant output`);
  } else {
    assert.deepEqual(actual.assistantGroupTexts, expectedAssistantGroups,
      `${location}: expected all ${assistantRows.length} assistant events across ${expectedAssistantGroups.length} rendered turn groups; got ${JSON.stringify({ sections: actual.assistantSections, chars: actual.assistantTextChars })}`);
  }
  const { assistantGroupTexts, ...summary } = actual;
  return { expectedAssistantEvents: REPLAY_BASELINE ? 0 : assistantRows.length, fixtureAssistantEvents: assistantRows.length, expectedRenderedGroups: REPLAY_BASELINE ? 0 : expectedAssistantGroups.length, expectedAssistantTextChars: REPLAY_BASELINE ? 0 : assistantTextChars, ...summary };
}
async function inspectInputReplay(browser) {
  async function capture(replay) {
    replayHistoryAsSSE = replay;
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    try {
      await page.goto(`http://127.0.0.1:${server.address().port}/view/${encodeURIComponent(SESSION)}`, { waitUntil: 'domcontentloaded' });
      await waitForHistory(page.mainFrame());
      return await page.locator('#timeline').evaluate(timeline => Array.from(timeline.querySelectorAll('.turn-group')).map(group => ({
        key: group.dataset.turnKey,
        segments: Array.from(group.children).filter(node => [...node.classList].some(name => /^turn-(user|assistant|tool|error)$/.test(name)))
          .map(node => ({ kind: [...node.classList].find(name => /^turn-(user|assistant|tool|error)$/.test(name)),
            text: node.querySelector('.event-content')?.textContent || '' })),
      })));
    } finally { await page.close(); }
  }
  currentResponse = firstPage;
  const historyOnly = await capture(false);
  const historyPlusSSE = await capture(true);
  replayHistoryAsSSE = false;
  const expectedInputs = new Map(rows.map(row => row.payload).filter(event => event.kind === 'user_input'
    && event.data?.lifecycle === 'admitted' && event.turn_id).map(event => [event.turn_id, event.text || '']));
  assert.equal(historyOnly.length, 17, 'history-only first page should have 17 turn-groups');
  assert.equal(expectedInputs.size, 16, 'fixture should have 16 admitted input turns');
  const actualUserGroups = historyPlusSSE.filter(group => group.segments.some(segment => segment.kind === 'turn-user'));
  assert.equal(actualUserGroups.length, expectedInputs.size,
    `history+SSE replay: expected ${expectedInputs.size} input turn-groups from fixture, got ${actualUserGroups.length}`);
  assert.equal(historyPlusSSE.length, historyOnly.length,
    `history+SSE replay: duplicated or split turn-groups; history=${historyOnly.length}, combined=${historyPlusSSE.length}`);
  for (const [turn, text] of expectedInputs) {
    const matches = historyPlusSSE.filter(group => group.key === turn);
    assert.equal(matches.length, 1, `history+SSE replay: expected one group for input turn ${turn}, got ${matches.length}`);
    const group = matches[0];
    assert.equal(group.segments.filter(segment => segment.kind === 'turn-user').length, 1,
      `history+SSE replay: expected one input section in turn ${turn}`);
    const userIndex = group.segments.findIndex(segment => segment.kind === 'turn-user');
    const assistantIndex = group.segments.findIndex(segment => segment.kind === 'turn-assistant');
    assert(userIndex >= 0 && assistantIndex > userIndex, `history+SSE replay: input must precede output in turn ${turn}`);
    assert.equal(group.segments[userIndex].text, text, `history+SSE replay: input duplicated or changed in turn ${turn}`);
  }
  const summary = groups => groups.map(group => ({ key: group.key, segments: group.segments.map(segment => segment.kind),
    userChars: group.segments.filter(segment => segment.kind === 'turn-user').reduce((n, segment) => n + segment.text.length, 0),
    assistantChars: group.segments.filter(segment => segment.kind === 'turn-assistant').reduce((n, segment) => n + segment.text.length, 0) }));
  console.log(`INPUT_HISTORY_SSE_DIAGNOSTIC ${JSON.stringify({ historyOnly: summary(historyOnly), historyPlusSSE: summary(historyPlusSSE) })}`);
}
async function runVersion(browser, version) {
  currentRendererName = version;
  const results = [];
  historyRequests.length = 0;
  if (!REPLAY_BASELINE && version === 'r59-fixed') {
    await inspectInputReplay(browser);
    historyRequests.length = 0;
  }
  // Direct View path.
  const direct = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  await direct.goto(`http://127.0.0.1:${server.address().port}/view/${encodeURIComponent(SESSION)}`, { waitUntil: 'domcontentloaded' });
  await waitForHistory(direct.mainFrame());
  results.push({ path: 'direct-view', ...(await assertCurrentOutput(direct.mainFrame(), `${version} direct`)) });
  assert.equal(historyRequests.length, 1);
  assert.equal(historyRequests[0].name, SESSION);
  await direct.close();

  // Actual production Console HTML: four separate iframe renderers, reload,
  // reopen all four, switch a background session back to the foreground.
  historyRequests.length = 0;
  const consolePage = await browser.newPage({ viewport: { width: 1400, height: 900 } });
  await consolePage.goto(`http://127.0.0.1:${server.address().port}/console`, { waitUntil: 'domcontentloaded' });
  let frames = new Map();
  for (const name of SESSIONS) frames.set(name, await openSession(consolePage, name));
  await waitForHistory(frames.get(SESSION));
  const beforeReload = await assertCurrentOutput(frames.get(SESSION), `${version} Console before reload`);
  const wasHidden = !(await frames.get(SESSION).locator('#timeline').isVisible());
  assert(wasHidden, 'first session should be in a hidden iframe after other tabs are opened');
  await consolePage.reload({ waitUntil: 'domcontentloaded' });
  frames = new Map();
  for (const name of SESSIONS) frames.set(name, await openSession(consolePage, name));
  await waitForHistory(frames.get(SESSION));
  const afterReload = await assertCurrentOutput(frames.get(SESSION), `${version} Console after reload`);
  for (const name of SESSIONS) {
    const requests = historyRequests.filter(request => request.name === name);
    assert.equal(requests.length, 2, `${version}: expected ${name} to load history once per Console page load`);
    assert(requests.every(request => request.path === `/view/${encodeURIComponent(name)}/history`), `${version}: wrong history route for ${name}`);
  }
  await consolePage.locator(`#tabs [title="${SESSION}"]`).click();
  assert(await frames.get(SESSION).locator('#timeline').isVisible(), 'switched first iframe must be foreground-visible');
  const foreground = await assertCurrentOutput(frames.get(SESSION), `${version} Console switched foreground`);
  const details = { path: 'console-four-iframe-reload-reopen', sessions: SESSIONS, beforeReload, afterReload, foreground, wasHidden, visibleAfterSwitch: true,
    historyRequests: historyRequests.map(({ renderer, name, path: requestPath, query }) => ({ renderer, name, path: requestPath, query })) };
  await consolePage.close();
  if (!REPLAY_BASELINE) {
    for (const mode of ['string', 'invalid-row']) {
      historyEventMode = mode;
      const compatibility = await browser.newPage({ viewport: { width: 1280, height: 900 } });
      const warnings = [];
      compatibility.on('console', message => { if (message.type() === 'warning') warnings.push(message.text()); });
      await compatibility.goto(`http://127.0.0.1:${server.address().port}/view/${encodeURIComponent(SESSION)}`, { waitUntil: 'domcontentloaded' });
      await waitForHistory(compatibility.mainFrame());
      const shapeResult = await assertCurrentOutput(compatibility.mainFrame(), `${version} ${mode}`);
      if (mode === 'invalid-row') {
        assert(warnings.some(message => message.includes('skipped 1 invalid event row(s)')),
          `${version}: malformed row must be counted and warned; warnings=${JSON.stringify(warnings)}`);
      }
      results.push({ path: `direct-view-${mode}-compatibility`, ...shapeResult });
      await compatibility.close();
    }
    historyEventMode = 'object';
  }
  return results.concat(details);
}

(async () => {
  await new Promise((resolve, reject) => { server.once('error', reject); server.listen(0, '127.0.0.1', resolve); });
  currentResponse = firstPage;
  const browser = await chromium.launch(process.env.PW_CHANNEL ? { channel: process.env.PW_CHANNEL } : {});
  try {
    for (const [name] of VERSIONS) {
      for (const result of await runVersion(browser, name)) console.log(`RESULT ${JSON.stringify({ renderer: name, ...result })}`);
    }
    console.log(`${REPLAY_BASELINE ? 'HISTORY_EVENT_OBJECT_BASELINE_RED' : 'HISTORY_EVENT_OBJECT_OK'} generation=${eventGeneration} seq=3958-4057 events=100 assistant_text=${assistantRows.length} chars=${assistantTextChars} direct_and_console=true`);
  } finally {
    await browser.close();
    server.closeAllConnections?.();
    await new Promise(resolve => server.close(resolve));
  }
})().catch(error => {
  console.error(error.stack || error);
  process.exitCode = 1;
  try { server.closeAllConnections?.(); server.close(); } catch (_) {}
});
