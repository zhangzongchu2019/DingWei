// Covers old/new Hub and renderer combinations, exact primary lifecycle routing,
// the default retry transport contract, and one-cut mutants at business assertions.
const assert = require('assert');
const fs = require('fs');
const path = require('path');
const {execFileSync} = require('child_process');
const {finishRegression, launchChromium, regressionCollector, startRendererServer} = require('./renderer-regression-selfhost');

const ROOT = path.resolve(__dirname, '..');
const OLD_RENDERER_COMMIT = 'a0e28049';
const OLD_RENDERER = execFileSync('git', ['show', `${OLD_RENDERER_COMMIT}:view-service/renderer-canary-sse.html`], {cwd: ROOT, encoding: 'utf8'});
const NEW_RENDERER = fs.readFileSync(path.join(__dirname, 'renderer-canary-sse.html'), 'utf8');
const ROLE_GUARD = "if (ev.kind === 'delivery_update' && !hasExactPrimaryDeliveryTarget(ev)) {\n        rememberUnmatchedPrimaryDeliveryUpdate(ev);\n        return;\n      }";
if (!NEW_RENDERER.includes(ROLE_GUARD)) throw new Error('retry role guard anchor missing');
const MUTANT_NO_ROLE_GUARD = NEW_RENDERER.replace(ROLE_GUARD, '// mutant: role guard removed');

async function openScenario(browser, renderer, name) {
  const fixture = await startRendererServer(renderer);
  const page = await browser.newPage({viewport: {width: 1280, height: 900}});
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.route('**/status', route => route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({session: 'local-fixture', state: 'idle', readonly: false, can_write: true})}));
  await page.route('**/events', route => route.fulfill({status: 200, contentType: 'text/event-stream', body: ''}));
  let controlMode = 'accepted';
  const controlBodies = [];
  await page.route('**/control', async route => {
    controlBodies.push(route.request().postDataJSON());
    if (controlMode === 'unknown') return route.abort();
    await route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({ok: controlMode !== 'rejected'})});
  });
  await page.goto(fixture.url + '?mock=1&unlocked=1', {waitUntil: 'domcontentloaded'});
  await page.evaluate(() => {
    clearInterval(statusTimer);
    applyStatusData({session: 'local-fixture'});
    resetConversation();
    finishTimelineReplay(false);
  });
  return {fixture, page, errors, controlBodies, name, setControlMode: mode => { controlMode = mode; }};
}

async function closeScenario(scenario) {
  await scenario.page.close();
  await scenario.fixture.close();
}

async function emit(page, event) {
  await page.evaluate(value => handleIncomingEvent(value), event);
}

function primaryInput(id) {
  return {schema_version: 'delivery.v2', kind: 'input_posted', message_id: 'msg-' + id,
    delivery_id: id, payload: 'body ' + id, stage: 'helper_queued', cancellable: false,
    delivery: {role: 'primary'}};
}

function lifecycle(id, role, observedId = id) {
  return {schema_version: 'delivery.v2', kind: 'delivery_update', message_id: 'msg-' + id,
    observed_delivery_id: observedId, delivery: role ? {role} : undefined,
    delivery_stage: 'helper_queued', delivery_status: 'unconfirmed_retryable',
    reason: 'unconfirmed_retryable', cancellable: false, ack_protocol: 2, processing_mode: 'driver'};
}

async function rowState(page, id) {
  const row = page.locator(`.waiting-item[data-delivery-id="${id}"]`);
  return {count: await row.count(), buttons: await row.locator('.waiting-retry-btn').count(), text: await row.innerText().catch(() => '')};
}

(async () => {
  let browser;
  const scenarios = [];
  try {
    browser = await launchChromium();
    const checks = regressionCollector();

    // old Hub + old renderer: both retain the known pre-fix behavior.
    const oldOld = await openScenario(browser, OLD_RENDERER, 'old-hub-old-renderer'); scenarios.push(oldOld);
    await emit(oldOld.page, primaryInput('oo'));
    await emit(oldOld.page, lifecycle('oo', null));
    await checks.check('MATRIX_OLD_HUB_OLD_RENDERER_COMPAT', async () => {
      assert.strictEqual((await rowState(oldOld.page, 'oo')).buttons, 1);
    });

    // new Hub + old renderer: the additive role is ignored by the old renderer;
    // preserve the documented deployment matrix until that renderer is replaced.
    const newOld = await openScenario(browser, OLD_RENDERER, 'new-hub-old-renderer'); scenarios.push(newOld);
    await emit(newOld.page, primaryInput('no'));
    await emit(newOld.page, lifecycle('no', 'copy'));
    await checks.check('MATRIX_NEW_HUB_OLD_RENDERER_OLD_UI', async () => {
      assert.strictEqual((await rowState(newOld.page, 'no')).buttons, 1);
    });
    await newOld.page.locator('.waiting-item[data-delivery-id="no"] .waiting-retry-btn').click();
    await newOld.page.locator('.retry-confirm-dialog [value="retry"]').click();
    await newOld.page.locator('.waiting-item[data-delivery-id="no"] .retry-note').waitFor();
    await checks.check('OLD_RENDERER_CANCEL_ONLY_TRANSPORT_STAYS_FAIL_CLOSED', async () => {
      assert.strictEqual(newOld.controlBodies.length, 0);
      assert.ok((await rowState(newOld.page, 'no')).text.includes('重发未发出'));
    });

    const senderPage = await openScenario(browser, NEW_RENDERER, 'sender-copy-page'); scenarios.push(senderPage);
    await emit(senderPage.page, {schema_version: 'delivery.v2', kind: 'input_posted', message_id: 'copy-msg',
      delivery_id: 'copy-delivery', payload: 'sender-side copy', stage: 'hub_received',
      source: {kind: 'agent', display_name: 'target', trust_decision: 'trusted'}, delivery: {role: 'copy'},
      copy_kind: 'route_observation'});
    await emit(senderPage.page, lifecycle('copy-msg', 'copy', 'copy-delivery'));
    await checks.check('SENDER_COPY_PAGE_NO_PRIMARY_RETRY', async () => {
      assert.strictEqual(await senderPage.page.locator('.waiting-retry-btn').count(), 0);
      assert.strictEqual(await senderPage.page.locator('.waiting-item').count(), 0);
      assert.ok((await senderPage.page.locator('#timeline').innerText()).includes('sender-side copy'));
    });

    // old Hub + new renderer: missing role means no lifecycle mutation or button.
    const oldNew = await openScenario(browser, NEW_RENDERER, 'old-hub-new-renderer'); scenarios.push(oldNew);
    await emit(oldNew.page, primaryInput('on'));
    await emit(oldNew.page, lifecycle('on', null));
    await checks.check('MATRIX_OLD_HUB_NEW_RENDERER_FAIL_CLOSED', async () => {
      const state = await rowState(oldNew.page, 'on');
      assert.strictEqual(state.buttons, 0);
      assert.ok(!state.text.includes('未确认（可重发）'));
    });

    // new Hub + new renderer: copy and wrong IDs do nothing; exact primary works.
    const newNew = await openScenario(browser, NEW_RENDERER, 'new-hub-new-renderer'); scenarios.push(newNew);
    await emit(newNew.page, primaryInput('nn'));
    await emit(newNew.page, lifecycle('nn', 'copy'));
    await emit(newNew.page, lifecycle('nn', 'primary', 'other-delivery'));
    await checks.check('COPY_AND_WRONG_ID_DO_NOT_CHANGE_PRIMARY', async () => {
      const state = await rowState(newNew.page, 'nn');
      assert.strictEqual(state.buttons, 0);
      assert.ok(!state.text.includes('未确认（可重发）'));
    });
    await emit(newNew.page, lifecycle('nn', 'primary'));
    await checks.check('PRIMARY_EXACT_ID_ENABLES_RETRY', async () => {
      assert.strictEqual((await rowState(newNew.page, 'nn')).buttons, 1);
    });
    await newNew.page.evaluate(() => {
      stats.authMode = 'view_page_v2'; stats.readonly = false;
      pageIdentity = {page_id: 'fixture-page', page_token: 'fixture-token'};
      pageIdentityUnique = true; viewRefreshClosed = false;
      prepareViewWrite = async () => {};
    });
    await newNew.page.locator('.waiting-item[data-delivery-id="nn"] .waiting-retry-btn').click();
    await newNew.page.locator('.retry-confirm-dialog [value="retry"]').click();
    await newNew.page.waitForFunction(() => document.querySelector('.waiting-item[data-delivery-id="nn"] .retry-note')?.textContent.includes('已重发'));
    await checks.check('DEFAULT_RETRY_TRANSPORT_BODY_AND_ACCEPTANCE', async () => {
      assert.strictEqual(newNew.controlBodies.length, 1);
      assert.deepStrictEqual(newNew.controlBodies[0], {
        kind: 'retry', delivery_id: 'dw-nn', page_id: 'fixture-page', page_token: 'fixture-token',
        request_id: newNew.controlBodies[0].request_id,
      });
      assert.ok(newNew.controlBodies[0].request_id);
      assert.ok(!Object.hasOwn(newNew.controlBodies[0], 'message_id'));
      assert.ok((await rowState(newNew.page, 'nn')).text.includes('已重发'));
    });

    newNew.setControlMode('rejected');
    await emit(newNew.page, primaryInput('reject'));
    await emit(newNew.page, lifecycle('reject', 'primary'));
    await newNew.page.locator('.waiting-item[data-delivery-id="reject"] .waiting-retry-btn').click();
    await newNew.page.locator('.retry-confirm-dialog [value="retry"]').click();
    await newNew.page.locator('.waiting-item[data-delivery-id="reject"] .retry-note').waitFor();
    await checks.check('DEFAULT_RETRY_EXPLICIT_REJECTION', async () => {
      assert.ok((await rowState(newNew.page, 'reject')).text.includes('重发未受理'));
      assert.strictEqual((await rowState(newNew.page, 'reject')).buttons, 1);
    });

    newNew.setControlMode('unknown');
    await emit(newNew.page, primaryInput('unknown'));
    await emit(newNew.page, lifecycle('unknown', 'primary'));
    await newNew.page.locator('.waiting-item[data-delivery-id="unknown"] .waiting-retry-btn').click();
    await newNew.page.locator('.retry-confirm-dialog [value="retry"]').click();
    await newNew.page.locator('.waiting-item[data-delivery-id="unknown"] .retry-note').waitFor();
    const unknownRequestId = newNew.controlBodies.at(-1).request_id;
    await checks.check('DEFAULT_RETRY_NETWORK_OUTCOME_UNKNOWN', async () => {
      assert.ok((await rowState(newNew.page, 'unknown')).text.includes('重发结果未知'));
      assert.strictEqual((await rowState(newNew.page, 'unknown')).buttons, 1);
    });
    newNew.setControlMode('accepted');
    await newNew.page.locator('.waiting-item[data-delivery-id="unknown"] .waiting-retry-btn').click();
    await newNew.page.locator('.retry-confirm-dialog [value="retry"]').click();
    await newNew.page.waitForFunction(() => document.querySelector('.waiting-item[data-delivery-id="unknown"] .retry-note')?.textContent.includes('已重发'));
    await checks.check('UNKNOWN_RETRY_REUSES_IDEMPOTENCY_KEY', async () => {
      const attempts = newNew.controlBodies.filter(body => body.delivery_id === 'dw-unknown');
      assert.strictEqual(attempts.length, 2);
      assert.deepStrictEqual(attempts.map(body => body.request_id), [unknownRequestId, unknownRequestId]);
      assert.ok(attempts.every(body => !Object.hasOwn(body, 'message_id')));
    });

    // The fixed browser suite must turn red in its business assertion if Hub
    // omits the role field or the renderer's role guard is deleted.
    await checks.check('MUTATION_ROLE_FIELD_DELETION_RED', async () => {
      const state = await rowState(oldNew.page, 'on');
      try { assert.strictEqual(state.buttons, 1, 'BUSINESS_ASSERTION_ROLE_FIELD_REQUIRED'); }
      catch (error) { assert.match(error.message, /BUSINESS_ASSERTION_ROLE_FIELD_REQUIRED/); console.log('  MUTATION_RED role_field_removed at BUSINESS_ASSERTION_ROLE_FIELD_REQUIRED'); return; }
      throw new Error('role field deletion survived');
    });

    const noGuard = await openScenario(browser, MUTANT_NO_ROLE_GUARD, 'renderer-role-guard-deleted'); scenarios.push(noGuard);
    await emit(noGuard.page, primaryInput('mutant'));
    await emit(noGuard.page, lifecycle('mutant', 'copy'));
    await checks.check('MUTATION_ROLE_GUARD_DELETION_RED', async () => {
      const state = await rowState(noGuard.page, 'mutant');
      try { assert.strictEqual(state.buttons, 0, 'BUSINESS_ASSERTION_COPY_MUST_NOT_RETRY'); }
      catch (error) { assert.match(error.message, /BUSINESS_ASSERTION_COPY_MUST_NOT_RETRY/); console.log('  MUTATION_RED renderer_role_guard_removed at BUSINESS_ASSERTION_COPY_MUST_NOT_RETRY'); return; }
      throw new Error('renderer role guard deletion survived');
    });

    await checks.check('NO_PAGE_ERRORS', async () => {
      for (const scenario of scenarios) assert.deepStrictEqual(scenario.errors, [], scenario.name);
    });
    checks.finish();
    console.log('RETRY_ROLE_BROWSER_PASS combinations=4 assertions=14 mutations=2-red');
  } finally {
    for (const scenario of scenarios) await closeScenario(scenario);
    if (browser) await browser.close();
  }
})().catch(finishRegression);
