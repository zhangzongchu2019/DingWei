const { chromium } = require('playwright');
const fs = require('fs');
const crypto = require('crypto');
const assert = require('assert');

const RENDERER_PATH = 'renderer-canary-sse.html';
const EXPECTED_RENDERER_SHA256 = '38c79d53a335cb9ec586869c457c8b0a6b70c64914894555d1176b03067c3f92';

(async () => {
  const renderer = fs.readFileSync(RENDERER_PATH);
  assert.strictEqual(
    crypto.createHash('sha256').update(renderer).digest('hex'),
    EXPECTED_RENDERER_SHA256,
    'test must run against the exact deployed renderer source artifact',
  );

  const browser = await chromium.launch({ headless: true });
  try {
    const context = await browser.newContext();
    const identity = {
      page_id: 'g2-focus-page',
      page_token: 'g2-focus-token',
      code: 'alice-developer-0000-G2focus1',
    };
    await context.addInitScript(value => {
      sessionStorage.setItem('dw-view-page:alice-developer-0000', JSON.stringify(value));
    }, identity);

    let statusRequests = 0;
    let resumeRequests = 0;
    let lockedResumeResponses = 0;
    let inputRequests = 0;
    await context.route('http://renderer-g2.test/**', async route => {
      const request = route.request();
      const path = new URL(request.url()).pathname;
      if (path === '/renderer') {
        return route.fulfill({ contentType: 'text/html', body: renderer });
      }
      if (path === '/status') {
        statusRequests += 1;
        await new Promise(resolve => setTimeout(resolve, 200));
        return route.fulfill({ contentType: 'application/json', body: JSON.stringify({
          session: 'alice-developer-0000', auth_mode: 'view_page_v2', readonly: true,
          session_state: 'readonly',
        }) });
      }
      if (path === '/page/resume') {
        resumeRequests += 1;
        const canWrite = !(resumeRequests > 1 && resumeRequests % 2 === 0);
        if (!canWrite) lockedResumeResponses += 1;
        return route.fulfill({ contentType: 'application/json', body: JSON.stringify({
          page_id: identity.page_id, code: identity.code,
          state: canWrite ? 'unlocked' : 'locked', can_write: canWrite,
        }) });
      }
      if (path === '/input') {
        inputRequests += 1;
        return route.fulfill({ status: 403, contentType: 'application/json', body: JSON.stringify({ ok: false }) });
      }
      if (path === '/events') return route.abort();
      return route.fulfill({ status: 404, body: 'not found' });
    });

    const page = await context.newPage();
    page.setDefaultTimeout(60000);
    await page.goto('http://renderer-g2.test/renderer', { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => stats.authMode === 'view_page_v2' && stats.readonly === false && pageIdentityUnique);

    await page.locator('#humanInput').focus();
    await page.evaluate(() => {
      window.__focusLosses = 0;
      window.__disabledTransitions = [];
      window.__lockBadgeTransitions = [];
      humanInput.addEventListener('blur', () => { window.__focusLosses += 1; });
      new MutationObserver(() => {
        window.__disabledTransitions.push({ disabled: humanInput.disabled, at: performance.now() });
      }).observe(humanInput, { attributes: true, attributeFilter: ['disabled'] });
      new MutationObserver(() => {
        window.__lockBadgeTransitions.push(lockBadge.classList.contains('readonly'));
      }).observe(lockBadge, { attributes: true, attributeFilter: ['class'] });
    });

    const expected = '0123456789abcdef'.repeat(10);
    await page.locator('#humanInput').pressSequentially(expected, { delay: 200 });
    const focus = await page.locator('#humanInput').evaluate(input => ({
      value: input.value,
      active: document.activeElement === input,
      disabled: input.disabled,
      selectionStart: input.selectionStart,
      selectionEnd: input.selectionEnd,
      focusLosses: window.__focusLosses,
      disabledTransitions: window.__disabledTransitions,
      lockBadgeTransitions: window.__lockBadgeTransitions,
    }));
    assert.ok(statusRequests >= 6, `expected at least six real status refreshes, got ${statusRequests}`);
    assert.ok(resumeRequests >= 6, `expected at least six page identity refreshes, got ${resumeRequests}`);
    assert.ok(lockedResumeResponses >= 3,
      `expected at least three locked identity responses, got ${lockedResumeResponses}`);
    assert.ok(focus.lockBadgeTransitions.includes(true),
      'locked identity responses never reached the readonly renderer branch');
    assert.ok(focus.lockBadgeTransitions.includes(false),
      'unlocked identity responses never restored the writable renderer branch');
    assert.strictEqual(focus.value, expected, 'continuous input lost, duplicated, or reordered characters');
    assert.strictEqual(focus.active, true, 'status refresh stole input focus');
    assert.strictEqual(focus.disabled, false, 'input ended disabled while focused');
    assert.strictEqual(focus.selectionStart, expected.length, 'selection start did not follow typed input');
    assert.strictEqual(focus.selectionEnd, expected.length, 'selection end did not follow typed input');
    assert.strictEqual(focus.focusLosses, 0, 'input blurred during status refresh');
    assert.deepStrictEqual(focus.disabledTransitions.filter(item => item.disabled), [],
      'input became disabled during continuous typing');
    console.log(`G2 focus PASS: sha256=${EXPECTED_RENDERER_SHA256} statusRefreshes=${statusRequests} identityRefreshes=${resumeRequests} lockedResponses=${lockedResumeResponses} exactChars=${expected.length}`);

    await page.locator('#sendBtn').click();
    await page.locator('.error').filter({ hasText: 'POST /input failed: HTTP 403' }).waitFor();
    assert.strictEqual(inputRequests, 1, 'UI submission did not issue exactly one rejected input request');
    assert.strictEqual(await page.locator('#humanInput').inputValue(), expected,
      'rejected input was cleared instead of remaining available to the user');
    console.log(`G2 UI 403 PASS: inputRequests=${inputRequests} visibleError=POST /input failed: HTTP 403`);

    await page.evaluate(() => applyStatusData({ auth_mode: 'view_page_v2', session_state: 'running' }));
    assert.strictEqual(await page.locator('#taskProgress').isVisible(), true,
      'task progress was not visible in running state');
    assert.strictEqual(await page.locator('#taskProgress').evaluate(element => element.hidden), false,
      'task progress retained the hidden property in running state');
    await page.evaluate(() => applyStatusData({ auth_mode: 'view_page_v2', session_state: 'idle' }));
    assert.strictEqual(await page.locator('#taskProgress').isVisible(), false,
      'task progress remained visible after leaving running state');
    assert.strictEqual(await page.locator('#taskProgress').evaluate(element => element.hidden), true,
      'task progress did not restore the hidden property after leaving running state');
    console.log('G2 running UI PASS: running=visible idle=hidden');

    await context.close();

    const isolationContext = await browser.newContext();
    let isolationCreated = 0;
    let isolationInputs = 0;
    const isolationPages = new Map();
    await isolationContext.route('http://renderer-isolation.test/**', async route => {
      const request = route.request();
      const path = new URL(request.url()).pathname;
      if (path === '/renderer') {
        return route.fulfill({ contentType: 'text/html', body: renderer });
      }
      if (path === '/status') {
        return route.fulfill({ contentType: 'application/json', body: JSON.stringify({
          session: 'alice-developer-0000', auth_mode: 'view_page_v2', readonly: true,
          session_state: 'readonly',
        }) });
      }
      if (path === '/page') {
        isolationCreated += 1;
        const value = {
          page_id: `isolated-page-${isolationCreated}`,
          page_token: `isolated-token-${isolationCreated}`,
          code: `alice-developer-0000-I${String(isolationCreated).padStart(7, '0')}`,
          state: 'locked',
        };
        isolationPages.set(value.page_id, value);
        return route.fulfill({ status: 201, contentType: 'application/json', body: JSON.stringify(value) });
      }
      if (path === '/page/resume') {
        const body = request.postDataJSON();
        const value = isolationPages.get(body.page_id);
        if (!value || body.page_token !== value.page_token) {
          return route.fulfill({ status: 403, contentType: 'application/json', body: JSON.stringify({ ok: false }) });
        }
        return route.fulfill({ contentType: 'application/json', body: JSON.stringify({
          page_id: value.page_id, code: value.code, state: value.state,
          can_write: value.state === 'unlocked',
        }) });
      }
      if (path === '/input') {
        const body = request.postDataJSON();
        const value = isolationPages.get(body.page_id);
        const ok = Boolean(value && value.state === 'unlocked' && body.page_token === value.page_token);
        if (ok) isolationInputs += 1;
        return route.fulfill({ status: ok ? 200 : 403, contentType: 'application/json', body: JSON.stringify({ ok }) });
      }
      if (path === '/events') return route.abort();
      return route.fulfill({ status: 404, body: 'not found' });
    });

    const isolatedA = await isolationContext.newPage();
    const isolatedB = await isolationContext.newPage();
    await Promise.all([
      isolatedA.goto('http://renderer-isolation.test/renderer', { waitUntil: 'domcontentloaded' }),
      isolatedB.goto('http://renderer-isolation.test/renderer', { waitUntil: 'domcontentloaded' }),
    ]);
    await Promise.all([isolatedA, isolatedB].map(page =>
      page.waitForFunction(() => pageIdentityUnique && pageIdentity?.page_id)));
    const initialIdentities = await Promise.all([isolatedA, isolatedB].map(page => page.evaluate(() => ({
      page_id: pageIdentity.page_id,
      page_token: pageIdentity.page_token,
    }))));
    assert.notStrictEqual(initialIdentities[0].page_id, initialIdentities[1].page_id,
      'independent tabs received the same page id');
    assert.notStrictEqual(initialIdentities[0].page_token, initialIdentities[1].page_token,
      'independent tabs received the same page token');

    isolationPages.get(initialIdentities[0].page_id).state = 'unlocked';
    await Promise.all([isolatedA, isolatedB].map(page => page.evaluate(() => ensurePageIdentity())));
    await isolatedA.waitForFunction(() => stats.readonly === false);
    await isolatedB.waitForFunction(() => stats.readonly === true);
    await isolatedA.evaluate(() => submitInput('g2-browser-isolation-a'));
    await assert.rejects(
      isolatedB.evaluate(() => submitInput('g2-browser-isolation-b-must-reject')),
      /当前页面尚未解锁/,
      'locked second tab accepted browser input',
    );
    assert.strictEqual(isolationInputs, 1, 'unlocking one tab changed the other tab write authority');
    assert.strictEqual(isolationPages.get(initialIdentities[0].page_id).state, 'unlocked');
    assert.strictEqual(isolationPages.get(initialIdentities[1].page_id).state, 'locked');
    console.log('G2 #3 renderer-half PASS: distinct id/token, A unlocked+writable, B locked+rejected');

    isolationPages.get(initialIdentities[1].page_id).state = 'unlocked';
    await isolatedB.evaluate(() => ensurePageIdentity());
    await isolatedB.waitForFunction(() => stats.readonly === false);
    await Promise.all([
      isolatedA.evaluate(() => submitInput('g2-multipage-a')),
      isolatedB.evaluate(() => submitInput('g2-multipage-b')),
    ]);
    assert.strictEqual(isolationInputs, 3, 'independently unlocked pages did not remain simultaneously writable');
    assert.strictEqual(isolationPages.get(initialIdentities[0].page_id).state, 'unlocked');
    assert.strictEqual(isolationPages.get(initialIdentities[1].page_id).state, 'unlocked');
    console.log('G2 #4 renderer-half PASS: same owner pages independently unlocked and simultaneously writable');
    await isolationContext.close();

    const cloneContext = await browser.newContext();
    const clonedIdentity = {
      page_id: 'cloned-grant-page', page_token: 'cloned-grant-token',
      code: 'alice-developer-0000-C1oned01',
    };
    await cloneContext.addInitScript(value => {
      sessionStorage.setItem('dw-view-page:alice-developer-0000', JSON.stringify(value));
    }, clonedIdentity);
    let cloneCreated = 0;
    let cloneInputs = 0;
    await cloneContext.route('http://renderer-clone.test/**', async route => {
      const request = route.request();
      const path = new URL(request.url()).pathname;
      if (path === '/renderer') return route.fulfill({ contentType: 'text/html', body: renderer });
      if (path === '/status') {
        return route.fulfill({ contentType: 'application/json', body: JSON.stringify({
          session: 'alice-developer-0000', auth_mode: 'view_page_v2', readonly: true, session_state: 'readonly',
        }) });
      }
      if (path === '/page/resume') {
        const body = request.postDataJSON();
        const original = body.page_id === clonedIdentity.page_id;
        return route.fulfill({ contentType: 'application/json', body: JSON.stringify({
          page_id: body.page_id,
          code: original ? clonedIdentity.code : `alice-developer-0000-C${cloneCreated}`,
          state: original ? 'unlocked' : 'locked', can_write: original,
        }) });
      }
      if (path === '/page') {
        cloneCreated += 1;
        return route.fulfill({ status: 201, contentType: 'application/json', body: JSON.stringify({
          page_id: `clone-replacement-${cloneCreated}`,
          page_token: `clone-replacement-token-${cloneCreated}`,
          code: `alice-developer-0000-C${String(cloneCreated).padStart(7, '0')}`, state: 'locked',
        }) });
      }
      if (path === '/input') {
        cloneInputs += 1;
        return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ ok: true }) });
      }
      if (path === '/events') return route.abort();
      return route.fulfill({ status: 404, body: 'not found' });
    });

    const cloneA = await cloneContext.newPage();
    const cloneB = await cloneContext.newPage();
    await Promise.all([
      cloneA.goto('http://renderer-clone.test/renderer', { waitUntil: 'domcontentloaded' }),
      cloneB.goto('http://renderer-clone.test/renderer', { waitUntil: 'domcontentloaded' }),
    ]);
    await Promise.all([cloneA, cloneB].map(page =>
      page.waitForFunction(() => pageIdentityUnique && pageIdentity?.page_id)));
    const cloneStates = await Promise.all([cloneA, cloneB].map(page => page.evaluate(() => ({
      page_id: pageIdentity.page_id,
      page_token: pageIdentity.page_token,
      readonly: stats.readonly,
    }))));
    assert.notStrictEqual(cloneStates[0].page_id, cloneStates[1].page_id,
      'cloned sessionStorage identity remained shared across tabs');
    assert.notStrictEqual(cloneStates[0].page_token, cloneStates[1].page_token,
      'cloned page token remained shared across tabs');
    assert.strictEqual(cloneStates.filter(state => state.page_id === clonedIdentity.page_id).length, 1,
      'exactly one tab did not retain the original grant identity');
    assert.strictEqual(cloneStates.filter(state => state.readonly === false).length, 1,
      'exactly one cloned tab did not retain write authority');
    const cloneAttempts = await Promise.allSettled([
      cloneA.evaluate(() => submitInput('g2-clone-a')),
      cloneB.evaluate(() => submitInput('g2-clone-b')),
    ]);
    assert.strictEqual(cloneAttempts.filter(result => result.status === 'fulfilled').length, 1,
      'both cloned tabs wrote after BroadcastChannel uniqueness settled');
    assert.strictEqual(cloneInputs, 1, 'cloned grant produced more than one accepted browser submission');
    console.log('G2 #11 renderer-half PASS: cloned id/token split, exactly one grant and one accepted input');
    await cloneContext.close();
  } finally {
    await browser.close();
  }
})().catch(error => {
  console.error(error);
  process.exit(1);
});
