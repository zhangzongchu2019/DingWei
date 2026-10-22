// Real renderer, local-only HTTP fixture; model failure must not rewrite ACKs.
const assert = require('assert');
const {finishRegression, launchChromium, regressionCollector, startRendererServer} = require('./renderer-regression-selfhost');
(async () => {
  const fixture = await startRendererServer();
  let browser;
  try {
    browser = await launchChromium();
    const page = await browser.newPage({viewport: {width: 1280, height: 900}});
    const errors = [];
    page.on('pageerror', e => errors.push(e.message));
    await page.route('**/status', r => r.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({session: 'local-fixture', state: 'idle', readonly: true, can_write: false})}));
    await page.route('**/events', r => r.fulfill({status: 200, contentType: 'text/event-stream', body: ''}));
    await page.goto(fixture.url + '?mock=1&unlocked=1', {waitUntil: 'domcontentloaded'});
    const checks = regressionCollector();
    const reset = () => page.evaluate(() => { clearInterval(statusTimer); resetConversation(); finishTimelineReplay(false); });
    const input = id => page.evaluate(id => handleIncomingEvent({kind: 'input_posted', text: 'local input ' + id, message_id: 'msg-' + id, delivery_id: id, stage: 'cli_injected'}), id);
    const failure = (id, reason = 'session_unavailable', extra = {}) => page.evaluate(({id,reason,extra}) => handleIncomingEvent({kind: 'state_change', delivery_id: id, text: '', data: {change:'turn_completed',outcome:'failed',reason,detail:'PRIVATE_UNTRUSTED_ERROR_<script>',...extra}}), {id,reason,extra});
    const snap = id => page.evaluate(id => {
      const entry = c1InputCards.get(c1DeliveryCards.get(deliveryMergeKey(id)));
      return {nodes: timeline.children.length, notes: entry?.footer?.querySelectorAll('.model-admission-failure').length || 0,
        text: entry?.footer?.querySelector('.model-admission-failure')?.textContent || '', stage:entry?.stageIdx,
        stageText:entry?.stageEl?.textContent, body:timeline.textContent, pending:pendingModelAdmissionFailures.size,
        assistant:document.querySelectorAll('.assistant').length, dock:document.querySelectorAll('#activityList > .activity-card').length};
    }, id);
    await reset(); await input('d-one');
    const before = await snap('d-one'); await failure('dw-d-one'); const after = await snap('d-one');
    await checks.check('VISIBLE_SESSION_UNAVAILABLE', async () => assert.strictEqual(after.text,'模型未接收：会话不可用'));
    await checks.check('ACK_STAGE_UNCHANGED', async () => {assert.strictEqual(after.stage,before.stage); assert.strictEqual(after.stageText,before.stageText);});
    await checks.check('NO_INVENTED_REPLY_OR_CARD', async () => {assert.strictEqual(after.nodes,before.nodes);assert.strictEqual(after.assistant,0);assert.strictEqual(after.dock,0);});
    await checks.check('NO_PRIVATE_ERROR_PAYLOAD', async () => assert.ok(!after.body.includes('PRIVATE_UNTRUSTED')));
    await failure('d-one');
    await checks.check('REPLAY_ONE_ANNOTATION', async () => assert.strictEqual((await snap('d-one')).notes,1));
    await failure('d-other');
    await checks.check('WRONG_DELIVERY_NO_CROSS_BINDING', async () => assert.strictEqual((await snap('d-one')).notes,1));
    await reset(); await failure('d-before'); await input('d-before');
    await checks.check('FAILURE_BEFORE_INPUT_VISIBLE', async () => assert.strictEqual((await snap('d-before')).notes,1));
    await checks.check('PENDING_CONSUMED', async () => assert.strictEqual((await snap('d-before')).pending,0));
    await reset(); await input('d-unknown'); await failure('d-unknown','model_admission_unknown');
    await checks.check('UNKNOWN_DISTINCT_FROM_NOT_ADMITTED', async () => assert.strictEqual((await snap('d-unknown')).text,'模型接收结果未知，请核实后再处理'));
    for (const [name, reason, extra] of [['success','session_unavailable',{outcome:'completed'}],['other-reason','generic-error',{}],['other-change','session_unavailable',{change:'heartbeat'}]]) {
      await reset(); await input(name); await failure(name,reason,extra);
      await checks.check('IGNORE_' + name, async () => assert.strictEqual((await snap(name)).notes,0));
    }
    await reset(); await failure('stale'); await reset(); await input('stale');
    await checks.check('RESET_CLEARS_PENDING', async () => assert.strictEqual((await snap('stale')).notes,0));
    await page.evaluate(() => {
      for(let i=0;i<1100;i++) observeModelAdmissionFailure({kind:'state_change',delivery_id:'absent-'+i,data:{change:'turn_completed',outcome:'failed',reason:'session_unavailable'}});
    });
    await checks.check('PENDING_BOUNDED', async () => assert.strictEqual((await snap('stale')).pending,1000));
    await checks.check('NO_PAGE_ERRORS', async () => assert.deepStrictEqual(errors,[]));
    if (process.env.STALE_SCREENSHOT) {
      await reset(); await input('screenshot'); await failure('screenshot'); await page.screenshot({path:process.env.STALE_SCREENSHOT,fullPage:true});
    }
    checks.finish(); console.log('MODEL_ADMISSION_BROWSER_PASS checks=15');
  } finally {
    if(browser) await browser.close(); await fixture.close();
  }
})().catch(finishRegression);
