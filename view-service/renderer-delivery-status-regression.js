// Browser regression for unknown delivery copy, retry confirmation, local tracking and expiry.
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
    await page.addInitScript(()=>{window.__unknownUxTestNow=Date.parse('2026-11-11T00:00:00Z');window.__viewUnknownUxNow=()=>window.__unknownUxTestNow;});
    await page.route('**/status', r => r.fulfill({status:200,contentType:'application/json',body:JSON.stringify({session:'local-fixture',state:'idle',readonly:false,can_write:true})}));
    await page.route('**/events', r => r.fulfill({status:200,contentType:'text/event-stream',body:''}));
    await page.goto(fixture.url + '?mock=1&unlocked=1', {waitUntil:'domcontentloaded'});
    const checks = regressionCollector();
    await page.evaluate(() => {
      clearInterval(statusTimer);
      window.retryCalls=[]; window.controlCalls=[]; window.__unknownUxTestNow=Date.parse('2026-11-11T00:00:00Z'); window.__viewUnknownUxNow=()=>window.__unknownUxTestNow;
      window.C2_CONTROL_TRANSPORT={send:async msg=>{window.controlCalls.push(msg);if(msg.kind==='retry')window.retryCalls.push(msg);return {ok:true};}};
      applyStatusData({session:'local-fixture'});
      resetConversation(); finishTimelineReplay(false);
    });
    const emit = ev => page.evaluate(ev => handleIncomingEvent(ev), ev);
    const input = (id, extra={}) => emit({schema_version:'delivery.v2',kind:'input_posted',message_id:'msg-'+id,delivery_id:id,text:'body '+id,stage:'helper_queued',...extra});
    const unknownStatus = (id, extra={}) => emit({schema_version:'delivery.v2',kind:'delivery_update',message_id:'msg-'+id,observed_delivery_id:id,delivery_stage:'helper_queued',delivery_status:'unconfirmed_retryable',reason:'unconfirmed_retryable',cancellable:false,ack_protocol:2,processing_mode:'driver',...extra});

    await input('queued', {cancellable:true});
    await checks.check('CANCELLABLE_PHASE_HAS_DELETE', async()=>assert.strictEqual(await page.locator('.waiting-item[data-delivery-id="queued"] .waiting-cancel-btn').count(),1));
    await input('taken', {cancellable:false});
    await checks.check('TAKEN_PHASE_HAS_NO_DELETE', async()=>assert.strictEqual(await page.locator('.waiting-item[data-delivery-id="taken"] .waiting-cancel-btn').count(),0));
    await page.locator('.waiting-item[data-delivery-id="queued"] .waiting-cancel-btn').click();
    await emit({schema_version:'delivery.v2',kind:'delivery_update',message_id:'msg-queued',observed_delivery_id:'queued',delivery_stage:'helper_queued',reason:'cancel_not_cancelable',cancellable:false});
    await checks.check('REFUSED_CANCEL_EXPLAINS_WHY', async()=>assert.ok((await page.locator('.waiting-item[data-delivery-id="queued"]').innerText()).includes('已交给 Agent，无法撤回')));

    await input('unknown', {queued_at:'2000-01-01T00:00:00Z',text:'target-unknown-body'});
    await emit({schema_version:'delivery.v2',kind:'delivery_update',message_id:'msg-unknown',observed_delivery_id:'unknown',delivery_stage:'helper_queued',reason:'model_admission_unknown',cancellable:false,ack_protocol:2,processing_mode:'driver',occurred_at:'2026-11-11T00:00:00Z'});
    await page.evaluate(()=>{const e=waitingDeliveries.get('unknown');e.queuedClock={mono:performance.now(),elapsed:600000};r40PaintQueuedWait(e);refreshUnknownDeliveryViews();});
    await page.locator('#inboxUnknownDetails summary').click();
    await checks.check('UNKNOWN_COPY_AND_NO_QUEUE_TIMER', async()=>{
      const row=page.locator('.waiting-item[data-delivery-id="unknown"]');
      const text=await row.innerText();
      assert.ok(text.includes('已送达，Agent 忙时无法确认是否已接收'));
      assert.ok(!(text.includes('已排队')||text.includes('Agent 正忙：')));
      assert.strictEqual(await row.locator('.model-queued-wait').count(),0);
      assert.ok((await page.locator('#inboxUnknownList').innerText()).includes('已送达，Agent 忙时无法确认是否已接收'));
      assert.strictEqual(await row.locator('.waiting-retry-btn').count(),0);
    });

    await input('unknown-two');
    await emit({schema_version:'delivery.v2',kind:'delivery_update',message_id:'msg-unknown-two',observed_delivery_id:'unknown-two',delivery_stage:'helper_queued',reason:'model_admission_unknown',cancellable:false,ack_protocol:2,processing_mode:'driver',occurred_at:'2026-11-11T00:00:00Z'});
    await page.evaluate(()=>{
      inboxLagState.state='fresh';
      inboxLagState.record={unknown:5,count:0,age:0,received:performance.now(),deadline:Infinity,clockUncertain:false};
      refreshUnknownDeliveryViews();
    });
    await checks.check('UNKNOWN_SUMMARY_MATCHES_VISIBLE_ROWS', async()=>{
      assert.strictEqual(await page.locator('#inboxUnknownDetails summary').innerText(),'查看未知条目（2）');
      assert.strictEqual(await page.locator('#inboxUnknownList > li').count(),2);
      assert.strictEqual(await page.locator('#inboxUnknownAggregate').innerText(),'helper 上报 5 条；本页明细 2 条');
    });

    const stableButton=page.locator('#inboxUnknownList > li .unknown-untrack-btn').first();
    await stableButton.focus();
    await page.evaluate(()=>{
      window.__unknownUxTrackedButton=document.querySelector('#inboxUnknownList > li .unknown-untrack-btn');
      for(let i=0;i<3;i++){window.__unknownUxTestNow+=1000;refreshUnknownDeliveryViews();}
      inboxLagState.record.unknown=6;
      refreshUnknownDeliveryViews();
    });
    await checks.check('UNKNOWN_LIST_STABLE_NODE_AND_FOCUS', async()=>{
      assert.strictEqual(await stableButton.evaluate(e=>e===window.__unknownUxTrackedButton),true);
      assert.strictEqual(await stableButton.evaluate(e=>document.activeElement===e),true);
      assert.strictEqual(await page.locator('#inboxUnknownAggregate').innerText(),'helper 上报 6 条；本页明细 2 条');
    });
    await page.evaluate(()=>{inboxLagState.record.unknown=5;refreshUnknownDeliveryViews();});

    const callsBeforeUntrack=await page.evaluate(()=>controlCalls.length);
    await page.locator('.waiting-item[data-delivery-id="unknown"] .unknown-untrack-btn').click();
    await checks.check('UNTRACK_IS_LOCAL_ONLY', async()=>{
      assert.strictEqual(await page.locator('.waiting-item[data-delivery-id="unknown"]').isHidden(),true);
      assert.ok(!(await page.locator('#inboxUnknownList').innerText()).includes('target-unknown-body'));
      assert.strictEqual(await page.locator('#inboxUnknownDetails summary').innerText(),'查看未知条目（1）');
      assert.strictEqual(await page.locator('#inboxUnknownList > li').count(),1);
      assert.strictEqual(await page.locator('#inboxUnknownAggregate').innerText(),'helper 上报 5 条；本页明细 1 条');
      assert.strictEqual(await page.evaluate(()=>controlCalls.length),callsBeforeUntrack);
    });

    await input('retryable-status'); await unknownStatus('retryable-status',{reason:'unconfirmed_retryable'});
    await input('retried-status'); await unknownStatus('retried-status',{delivery_status:'retried',reason:''});
    await input('retryable-reason-only'); await unknownStatus('retryable-reason-only',{delivery_status:'',reason:'unconfirmed_retryable'});
    await page.evaluate(()=>{
      for(const id of ['retryable-status','retried-status','retryable-reason-only']){
        const entry=waitingDeliveries.get(id);entry.queuedClock={mono:performance.now(),elapsed:180000};r40PaintQueuedWait(entry);
      }
      refreshUnknownDeliveryViews();
    });
    await checks.check('RETRYABLE_BACKLOG_NO_QUEUED_TIMER', async()=>{
      for(const id of ['retryable-status','retried-status','retryable-reason-only']){
        const row=page.locator(`.waiting-item[data-delivery-id="${id}"]`),text=await row.innerText();
        assert.ok(text.includes('Agent 忙时未能确认接收，可能未送达'),`${id} missing standard unknown copy`);
        assert.ok(!text.includes('已排队')&&!text.includes('尚未注入会话'),`${id} has misleading timer copy`);
        assert.strictEqual(await row.locator('.model-queued-wait').count(),0,`${id} created queue timer`);
        assert.ok((await page.locator('#inboxUnknownList').innerText()).includes('Agent 忙时未能确认接收，可能未送达'));
      }
    });

    await input('retry-default'); await unknownStatus('retry-default',{reason:'model_admission_unknown'});
    const retryDefault=page.locator('.waiting-item[data-delivery-id="retry-default"] .waiting-retry-btn');
    await retryDefault.click();
    let defaultDialogAppeared=false;
    await checks.check('RETRY_DIALOG_COPY_AND_DEFAULT_NO_RETRY', async()=>{
      const dialog=page.locator('.retry-confirm-dialog');
      await page.waitForTimeout(50);
      defaultDialogAppeared=await dialog.count()===1;
      assert.strictEqual(defaultDialogAppeared,true);
      assert.ok((await dialog.innerText()).includes('可能已被处理，重发可能导致重复执行'));
      assert.strictEqual(await dialog.locator('[value="cancel"]').evaluate(e=>document.activeElement===e),true);
      await page.keyboard.press('Enter');
      await dialog.waitFor({state:'detached'});
      assert.strictEqual(await page.evaluate(()=>retryCalls.length),0);
    });
    await input('retry-explicit'); await unknownStatus('retry-explicit');
    await page.locator('.waiting-item[data-delivery-id="retry-explicit"] .waiting-retry-btn').click();
    let explicitDialogAppeared=false;
    await checks.check('RETRY_REQUIRES_EXPLICIT_CONFIRMATION', async()=>{
      explicitDialogAppeared=await page.locator('.retry-confirm-dialog').count()===1;
      assert.strictEqual(explicitDialogAppeared,true);
      if(!explicitDialogAppeared)return;
      await page.locator('.retry-confirm-dialog [value="retry"]').click();
      await page.waitForFunction(()=>retryCalls.length===1);
      assert.strictEqual(await page.evaluate(()=>retryCalls.length),1);
      assert.strictEqual(await page.locator('.waiting-item[data-delivery-id="retry-explicit"] .waiting-retry-btn').count(),0);
      await page.locator('.waiting-item[data-delivery-id="retry-explicit"] .retry-note').waitFor();
      assert.ok((await page.locator('.waiting-item[data-delivery-id="retry-explicit"]').innerText()).includes('已重发'));
    });

    await input('reply-arrived'); await unknownStatus('reply-arrived');
    await page.locator('.waiting-item[data-delivery-id="reply-arrived"] .waiting-retry-btn').click();
    await emit({schema_version:'delivery.v2',kind:'assistant_text',message_id:'reply-msg',delivery_id:'reply-arrived',text:'already replied'});
    await checks.check('REPLY_REMOVES_RETRY_BEFORE_CONFIRM', async()=>{
      assert.strictEqual(await page.locator('.waiting-item[data-delivery-id="reply-arrived"] .waiting-retry-btn').count(),0);
      const dialog=page.locator('.retry-confirm-dialog');
      if(await dialog.count()){await dialog.locator('[value="retry"]').click();await dialog.waitFor({state:'detached'});}
      assert.strictEqual(await page.evaluate(()=>retryCalls.length),1);
    });

    await input('late-admitted'); await unknownStatus('late-admitted');
    await emit({schema_version:'delivery.v2',kind:'delivery_update',message_id:'msg-late-admitted',observed_delivery_id:'late-admitted',delivery_stage:'cli_injected',delivery_status:'injected',turn_id:'turn-late',reason:'',cancellable:false,ack_protocol:2,processing_mode:'driver'});
    await checks.check('LATE_ADMITTED_RESTORES_NORMAL_PROCESSED', async()=>{
      assert.strictEqual(await page.locator('.waiting-item[data-delivery-id="late-admitted"]').count(),0);
      assert.ok((await page.locator('#timeline').innerText()).includes('body late-admitted'));
    });
    await input('late-processed'); await unknownStatus('late-processed');
    await emit({schema_version:'delivery.v2',kind:'delivery_update',message_id:'msg-late-processed',observed_delivery_id:'late-processed',delivery_stage:'turn_processed',delivery_status:'processed',reason:'',cancellable:false,ack_protocol:2,processing_mode:'driver'});
    await checks.check('LATE_PROCESSED_RESTORES_NORMAL_PROCESSED', async()=>{
      assert.strictEqual(await page.locator('.waiting-item[data-delivery-id="late-processed"]').count(),0);
      assert.ok((await page.locator('#timeline').innerText()).includes('body late-processed'));
    });

    await input('expires', {queued_at:'2000-01-01T00:00:00Z'});
    await emit({schema_version:'delivery.v2',kind:'delivery_update',message_id:'msg-expires',observed_delivery_id:'expires',delivery_stage:'helper_queued',reason:'model_admission_unknown',cancellable:false,ack_protocol:2,processing_mode:'driver',occurred_at:'2026-11-11T00:00:00Z'});
    await page.evaluate(()=>{window.__viewUnknownUxNow=()=>Date.parse('2026-11-12T00:00:00.001Z');refreshUnknownDeliveryViews();});
    await checks.check('EXPIRED_HIDDEN_FROM_WAITING_RETAINED_IN_UNKNOWN_LIST', async()=>{
      assert.strictEqual(await page.locator('.waiting-item[data-delivery-id="expires"]').isHidden(),true);
      const details=page.locator('#inboxUnknownDetails');
      if(!(await details.evaluate(e=>e.open)))await details.locator('summary').click();
      const text=await details.innerText();
      assert.ok(text.includes('body expires'));
      assert.ok(text.includes('已超 24 小时'));
    });

    await page.evaluate(()=>{
      resetConversation();inboxLagState.record=null;inboxLagState.state='unknown';
      window.controlCalls=[];refreshUnknownDeliveryViews();
    });
    await input('batch-one'); await unknownStatus('batch-one');
    await input('batch-two'); await unknownStatus('batch-two');
    const batchButton=page.locator('#inboxUnknownUntrackAll');
    await checks.check('BULK_UNTRACK_DEFAULT_CANCEL', async()=>{
      const before=await page.evaluate(()=>controlCalls.length);
      await batchButton.click();
      const dialog=page.locator('.unknown-untrack-all-dialog');
      await dialog.waitFor();
      assert.strictEqual(await dialog.locator('[value="cancel"]').evaluate(e=>document.activeElement===e),true);
      await page.keyboard.press('Enter');
      await dialog.waitFor({state:'detached'});
      assert.strictEqual(await page.locator('#inboxUnknownList > li').count(),2);
      assert.strictEqual(await page.locator('.waiting-item[data-delivery-id^="batch-"]').evaluateAll(es=>es.filter(e=>e.hidden).length),0);
      assert.strictEqual(await page.evaluate(()=>controlCalls.length),before);
    });
    await checks.check('BULK_UNTRACK_CONFIRM_IS_LOCAL', async()=>{
      const before=await page.evaluate(()=>controlCalls.length);
      await batchButton.click();
      const dialog=page.locator('.unknown-untrack-all-dialog');
      await dialog.locator('[value="confirm"]').click();
      await dialog.waitFor({state:'detached'});
      assert.strictEqual(await page.locator('#inboxUnknownList > li').count(),0);
      assert.strictEqual(await page.locator('.waiting-item[data-delivery-id^="batch-"]').evaluateAll(es=>es.filter(e=>e.hidden).length),2);
      assert.strictEqual(await page.evaluate(()=>controlCalls.length),before);
    });

    await page.evaluate(()=>{window.__unknownUxTestNow=Date.parse('2026-11-11T00:00:00Z');window.__viewUnknownUxNow=()=>window.__unknownUxTestNow;resetConversation();inboxLagState.record=null;inboxLagState.state='unknown';refreshUnknownDeliveryViews();});
    await input('persist-one'); await unknownStatus('persist-one');
    await checks.check('UNTRACK_PERSISTS_ACROSS_RELOAD', async()=>{
      const storageKey='dw-view-unknown-untracked-v1:'+encodeURIComponent('local-fixture')+':persist-one';
      await page.locator('.waiting-item[data-delivery-id="persist-one"] .unknown-untrack-btn').click();
      let waitError='';
      try{await page.waitForFunction(key=>localStorage.getItem(key)!==null,storageKey,{timeout:1000});}
      catch(error){waitError=error.message;}
      const storedKeys=await page.evaluate(()=>Array.from({length:localStorage.length},(_,i)=>localStorage.key(i)));
      assert.ok(!waitError&&storedKeys.includes(storageKey),`UNTRACK_PERSISTS_ACROSS_RELOAD: localStorage 标记未写入；wait=${waitError||'key absent'} keys=${JSON.stringify(storedKeys)}`);
      await page.reload({waitUntil:'domcontentloaded'});
      await page.waitForFunction(()=>stats.sessionName==='local-fixture');
      await page.evaluate(()=>{
        clearInterval(statusTimer);window.controlCalls=[];window.retryCalls=[];
        window.C2_CONTROL_TRANSPORT={send:async msg=>{window.controlCalls.push(msg);if(msg.kind==='retry')window.retryCalls.push(msg);return {ok:true};}};
        resetConversation();applyStatusData({session:'local-fixture'});finishTimelineReplay(false);
      });
      await input('persist-one'); await unknownStatus('persist-one');
      const row=page.locator('.waiting-item[data-delivery-id="persist-one"]');
      assert.strictEqual(await row.count(),1);
      assert.strictEqual(await row.isHidden(),true,`session=${await page.evaluate(()=>stats.sessionName)} restored=${JSON.stringify(await page.evaluate(()=>[...locallyUntrackedDeliveries]))} keys=${JSON.stringify(await page.evaluate(()=>Array.from({length:localStorage.length},(_,i)=>localStorage.key(i))))}`);
      assert.strictEqual(await page.locator('#inboxUnknownList').innerText().then(t=>t.includes('body persist-one')),false);
      assert.strictEqual(await page.evaluate(()=>controlCalls.length),0);
    });

    await page.evaluate(()=>{
      const key='dw-view-unknown-untracked-v1:'+encodeURIComponent('local-fixture')+':ttl-old';
      localStorage.setItem(key,String(window.__unknownUxTestNow-30*24*60*60*1000-1));
    });
    await page.reload({waitUntil:'domcontentloaded'});
    await page.waitForFunction(()=>stats.sessionName==='local-fixture');
    await checks.check('UNTRACK_STORAGE_EXPIRES_AFTER_30_DAYS', async()=>{
      assert.strictEqual(await page.evaluate(()=>localStorage.getItem('dw-view-unknown-untracked-v1:'+encodeURIComponent('local-fixture')+':ttl-old')),null);
    });

    await page.evaluate(()=>{
      for(let i=localStorage.length-1;i>=0;i--){const key=localStorage.key(i);if(key?.startsWith('dw-view-unknown-untracked-v1:'))localStorage.removeItem(key);}
      for(let i=0;i<501;i++)localStorage.setItem('dw-view-unknown-untracked-v1:'+encodeURIComponent('local-fixture')+':cap-'+i,String(window.__unknownUxTestNow-i));
    });
    await page.reload({waitUntil:'domcontentloaded'});
    await page.waitForFunction(()=>stats.sessionName==='local-fixture');
    await checks.check('UNTRACK_STORAGE_CAP_500_KEEPS_NEWEST', async()=>{
      const keys=await page.evaluate(()=>Array.from({length:localStorage.length},(_,i)=>localStorage.key(i)).filter(k=>k?.startsWith('dw-view-unknown-untracked-v1:')));
      assert.strictEqual(keys.length,500);
      assert.ok(keys.includes('dw-view-unknown-untracked-v1:'+encodeURIComponent('local-fixture')+':cap-0'));
      assert.ok(!keys.includes('dw-view-unknown-untracked-v1:'+encodeURIComponent('local-fixture')+':cap-500'));
    });
    await checks.check('NO_PAGE_ERRORS', async()=>assert.deepStrictEqual(errors,[]));
    checks.finish(); console.log('DELIVERY_STATUS_BROWSER_PASS checks=20');
  } finally { if(browser) await browser.close(); await fixture.close(); }
})().catch(finishRegression);
