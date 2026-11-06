// Real renderer browser regression for cancellation phases and retryable unknown deliveries.
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
    await page.route('**/status', r => r.fulfill({status:200,contentType:'application/json',body:JSON.stringify({session:'local-fixture',state:'idle',readonly:false,can_write:true})}));
    await page.route('**/events', r => r.fulfill({status:200,contentType:'text/event-stream',body:''}));
    await page.goto(fixture.url + '?mock=1&unlocked=1', {waitUntil:'domcontentloaded'});
    const checks = regressionCollector();
    await page.evaluate(() => {
      clearInterval(statusTimer);
      window.retryCalls=[];
      window.C2_CONTROL_TRANSPORT={send:async msg=>{window.retryCalls.push(msg);return {ok:true};}};
      resetConversation(); finishTimelineReplay(false);
    });
    const emit = ev => page.evaluate(ev => handleIncomingEvent(ev), ev);
    const input = (id, extra={}) => emit({schema_version:'delivery.v2',kind:'input_posted',message_id:'msg-'+id,delivery_id:id,text:'body '+id,stage:'helper_queued',...extra});
    await input('queued', {cancellable:true});
    await checks.check('CANCELLABLE_PHASE_HAS_DELETE', async()=>assert.strictEqual(await page.locator('.waiting-item[data-delivery-id="queued"] .waiting-cancel-btn').count(),1));
    await input('taken', {cancellable:false});
    await checks.check('TAKEN_PHASE_HAS_NO_DELETE', async()=>assert.strictEqual(await page.locator('.waiting-item[data-delivery-id="taken"] .waiting-cancel-btn').count(),0));
    await page.locator('.waiting-item[data-delivery-id="queued"] .waiting-cancel-btn').click();
    await emit({schema_version:'delivery.v2',kind:'delivery_update',message_id:'msg-queued',observed_delivery_id:'queued',delivery_stage:'helper_queued',reason:'cancel_not_cancelable',cancellable:false});
    await checks.check('REFUSED_CANCEL_EXPLAINS_WHY', async()=>assert.ok((await page.locator('.waiting-item[data-delivery-id="queued"]').innerText()).includes('已交给 Agent，无法撤回')));
    await input('unknown');
    await emit({schema_version:'delivery.v2',kind:'delivery_update',message_id:'msg-unknown',observed_delivery_id:'unknown',delivery_stage:'helper_queued',reason:'model_admission_unknown',cancellable:false,ack_protocol:2,processing_mode:'driver'});
    await checks.check('UNKNOWN_HAS_CLEAR_COPY', async()=>assert.ok((await page.locator('.waiting-item[data-delivery-id="unknown"]').innerText()).includes('Agent 正忙：消息已交出但未确认被处理，请看它的下一条回复')));
    await emit({schema_version:'delivery.v2',kind:'delivery_update',message_id:'msg-unknown',observed_delivery_id:'unknown',delivery_stage:'helper_queued',delivery_status:'unconfirmed_retryable',reason:'unconfirmed_retryable',cancellable:false,ack_protocol:2,processing_mode:'driver'});
    await checks.check('TURN_END_STOPS_SPINNER_AND_OFFERS_RETRY', async()=>{
      const row=page.locator('.waiting-item[data-delivery-id="unknown"]');
      assert.strictEqual(await row.locator('.prov-spin').count(),0);
      assert.ok((await row.innerText()).includes('未确认（可重发）'));
      assert.strictEqual(await row.locator('.waiting-retry-btn').count(),1);
    });
    await page.locator('.waiting-item[data-delivery-id="unknown"] .waiting-retry-btn').click();
    await checks.check('RETRY_ONLY_ONCE', async()=>{
      assert.strictEqual(await page.evaluate(()=>retryCalls.filter(x=>x.kind==='retry').length),1);
      assert.strictEqual(await page.locator('.waiting-item[data-delivery-id="unknown"] .waiting-retry-btn').count(),0);
      await page.locator('.waiting-item[data-delivery-id="unknown"] .retry-note').waitFor();
      assert.ok((await page.locator('.waiting-item[data-delivery-id="unknown"]').innerText()).includes('已重发'));
    });
    await checks.check('UNKNOWN_LIST_ENTRY_AVAILABLE', async()=>{
      const details=page.locator('#inboxUnknownDetails');
      assert.strictEqual(await details.isHidden(),false);
      await details.locator('summary').click();
      assert.ok((await details.innerText()).includes('body unknown'));
    });
    await checks.check('NO_PAGE_ERRORS', async()=>assert.deepStrictEqual(errors,[]));
    checks.finish(); console.log('DELIVERY_STATUS_BROWSER_PASS checks=8');
  } finally { if(browser) await browser.close(); await fixture.close(); }
})().catch(finishRegression);
