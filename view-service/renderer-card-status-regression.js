// Actual renderer in Chrome; delivery.v2 updates alone own canonical card status.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { finishRegression, launchChromium, regressionCollector, startRendererServer } = require('./renderer-regression-selfhost');
(async () => {
  const fixture = await startRendererServer();
  let browser;
  try {
    browser = await launchChromium();
    const checks = regressionCollector();
    let total = 0;
    for (const flag of [false, true]) {
      const page = await browser.newPage();
      const errors = [];
      page.on('pageerror', error => errors.push(error.message));
      await page.addInitScript(flag => { window.PROVENANCE_ICONS_V1 = flag; }, flag);
      if (process.argv.includes('--remove-guard')) {
        const html = fs.readFileSync(path.join(__dirname,'renderer-canary-sse.html'),'utf8');
        const guard = "      if (entry.event.kind === 'input_posted') return;\n";
        assert.equal(html.split(guard).length,2,'mutation anchor must be unique');
        await page.route(fixture.url+'*',r => r.fulfill({contentType:'text/html',body:html.replace(guard,'')}));
      }
      await page.route('**/status', r => r.fulfill({contentType:'application/json',body:JSON.stringify({session:'fixture',state:'idle',readonly:true,can_write:false})}));
      await page.route('**/events', r => r.fulfill({contentType:'text/event-stream',body:''}));
      await page.goto(fixture.url + '?mock=1&unlocked=1', {waitUntil:'domcontentloaded'});
      for (const kind of ['assistant_text', 'state_change', 'user_input']) {
        for (const linked of [true, false]) {
          await checks.check(`flag=${flag} ${kind} linked=${linked}: pending survives raw event; canonical status reaches same card`, async () => {
            total++;
            const before = await page.evaluate(({kind,linked}) => {
              resetConversation(); finishTimelineReplay(false);
              window.testInput = {kind:'input_posted',schema_version:'delivery.v2',message_id:'primary-source',delivery_id:'dw-dlv-primary',payload:'primary request',stage:'hub_received',delivery:{role:'primary'},cancellable:true};
              window.testUpdate = (stage,extra={}) => handleIncomingEvent({kind:'delivery_update',schema_version:'delivery.v2',message_id:'primary-source',observed_delivery_id:'dw-dlv-primary',delivery_stage:stage,ack_protocol:2,processing_mode:'driver',cancellable:false,...extra});
              handleIncomingEvent(testInput);
              testUpdate('helper_queued');
              handleIncomingEvent({kind,delivery_id:linked?'dw-dlv-primary':'',turn_id:'native-turn',text:kind==='assistant_text'?'native answer':kind==='user_input'?'unlinked echo':'',data:kind==='state_change'?{raw_method:'turn/completed',change:'turn_completed',status:'completed',turn:{id:'native-turn',status:'completed'}}:kind==='assistant_text'?{raw_method:'item/agentMessage/delta',item_id:'native-item',thread_id:'native-thread'}:{lifecycle:'admitted'}});
              return {waiting:waitingDeliveries.size,body:waitingList.textContent};
            }, {kind,linked});
            // Record both pre/post observations even on regression, to expose
            // the missing card index rather than only a changed waiting count.
            const after = await page.evaluate(() => {
              testUpdate('cli_injected',{turn_id:'native-turn'});
              const key = c1DeliveryCards.get(deliveryMergeKey('dw-dlv-primary'));
              const entry = c1InputCards.get(key);
              window.testPrimaryNode = entry?.card;
              testUpdate('turn_processed',{turn_id:'native-turn',turn_outcome:'completed'});
              testUpdate('helper_queued'); // stale phase must not regress terminal
              return {card:!!entry?.card,sameNode:entry?.card===window.testPrimaryNode,content:entry?.content?.textContent,status:entry?.stageEl?.textContent,pending:c1PendingUpdates.size,waiting:waitingDeliveries.size};
            });
            console.log(JSON.stringify({flag,kind,linked,before,after}));
            assert.equal(before.waiting,1,'raw output/admission must not remove canonical pending primary');
            assert.ok(before.body.includes('helper 已排队，未注入'));
            assert.equal(after.card,true,'canonical update must create an indexed primary card');
            assert.equal(after.sameNode,true);
            assert.equal(after.content,'primary request');
            assert.equal(after.status,'回合已结束（完成）');
            assert.equal(after.pending,0,'updates must not be stranded without a card');
            assert.equal(after.waiting,0);
            if (!linked && kind==='user_input') {
              assert.equal(await page.locator('.turn-user .event-content').innerText(),'unlinked echo','ID-less echo stays independent; no text/time guessing');
            }
          });
        }
      }
      await checks.check(`flag=${flag}: legacy pending and output still render`, async () => {
        total++;
        await page.evaluate(flag => {
          resetConversation(); finishTimelineReplay(false);
          handleIncomingEvent({kind:'user_input',text:'legacy request',delivery_id:'legacy-delivery',data:{lifecycle:flag?'admitted':'accepted'},...(flag?{turn_id:'legacy-turn'}:{})});
          handleIncomingEvent({kind:'assistant_text',text:'legacy answer',delivery_id:'legacy-delivery',turn_id:'legacy-turn'});
          flushPendingRenders();
        }, flag);
        await page.waitForTimeout(1000); // existing provenance echo hold
        const legacy = await page.evaluate(() => { flushPendingRenders(); return {waiting:waitingDeliveries.size,text:timeline.textContent}; });
        assert.equal(legacy.waiting,0);
        assert.ok(legacy.text.includes('legacy request'));
        assert.ok(legacy.text.includes('legacy answer'));
      });
      await checks.check(`flag=${flag}: no browser errors`, async () => { total++; assert.deepEqual(errors,[]); });
      await page.close();
    }
    checks.finish();
    console.log(`CARD_STATUS_OK cases=${total} failures=0`);
  } finally {
    if (browser) await browser.close();
    await fixture.close();
  }
})().catch(finishRegression);
