// Browser projection gate; actual CLI/Hub acceptance remains a separate E2E gate.
const assert = require('assert/strict');
const fs = require('fs');
const path = require('path');
const { chromium } = require('playwright');
const html = fs.readFileSync(path.join(__dirname, 'renderer-canary-sse.html'), 'utf8');
let assertions = 0;
function check(value, message) { assertions++; assert.ok(value, message); }
(async () => {
  const browser = await chromium.launch({ headless: true, channel: process.env.PW_CHANNEL || 'chrome' });
  try {
    for (const flag of [false, true]) {
      const page = await browser.newPage();
      const errors = [];
      page.on('pageerror', e => errors.push(e.message));
      await page.addInitScript(flag => { window.PROVENANCE_ICONS_V1 = flag; }, flag);
      await page.route('**/*', async route => {
        const url = new URL(route.request().url());
        if (url.pathname.startsWith('/view/')) return route.fulfill({ contentType:'text/html', body:html });
        if (url.pathname.endsWith('/status')) return route.fulfill({ contentType:'application/json', body:JSON.stringify({ session:'ack-fixture',state:'idle',readonly:true,can_write:false,auth_mode:'legacy',owner:'fixture' }) });
        if (url.pathname.endsWith('/events')) return route.fulfill({ contentType:'text/event-stream', body:'' });
        return route.fulfill({ contentType:'application/json', body:'[]' });
      });
      await page.clock.install({ time:new Date('2026-10-27T06:00:00Z') });
      await page.goto('http://renderer.test/view/ack-fixture', { waitUntil:'domcontentloaded' });
      await page.locator('#humanInput').waitFor({state:'visible'});
      await page.evaluate(() => {
        resetConversation();
        window.ackInput = (id) => handleIncomingEvent({kind:'input_posted',schema_version:'delivery.v2',message_id:'source-'+id,delivery_id:id,payload:'fixture '+id,stage:'hub_received',source:{kind:'agent',trust_decision:'trusted'},delivery:{role:'primary'},cancellable:true});
        window.ackUpdate = (id,phase,extra={}) => handleIncomingEvent({kind:'delivery_update',schema_version:'delivery.v2',message_id:'source-'+id,observed_delivery_id:id,delivery_stage:phase,cancellable:phase==='helper_queued',ack_protocol:2,processing_mode:'driver',ack_generation:1,...extra});
        ackInput('dw-dlv-A');
        ackUpdate('dw-dlv-A','helper_queued',{queued_at:new Date(Date.now()).toISOString()});
      });
      let text = await page.locator('#waitingList').innerText();
      check(text.includes('helper 已排队，未注入'), '可信 queued 必须显示未注入');
      check(!text.includes('已注入执行') && !text.includes('回合已结束'), '入队不得显示模型执行/终态');
      await page.clock.fastForward(31000);
      text = await page.locator('#waitingList').innerText();
      check(text.includes('尚未注入会话') && text.includes('不要按已执行处理'), 'queued 超过30秒须提示未注入及下一步');
      const firstClock = await page.evaluate(() => window.__c2.waitingDeliveries.get('dw-dlv-A').queuedClock);
      await page.evaluate(() => ackUpdate('dw-dlv-A','helper_queued',{queued_at:'2026-10-27T06:00:00Z'}));
      const afterClock = await page.evaluate(() => window.__c2.waitingDeliveries.get('dw-dlv-A').queuedClock);
      check(JSON.stringify(firstClock)===JSON.stringify(afterClock), 'queued 重放不能重置首个时刻与单调计时器');
      check((await page.locator('#waitingList').innerText()).includes('已排队 31 秒'), 'queued 重绘不能清掉已到期等待提示');
      await page.evaluate(() => { window.originalAckWall = Date.now; Date.now = () => originalAckWall() - 3600000; });
      await page.clock.fastForward(1000);
      check((await page.locator('#waitingList').innerText()).includes('已排队 32 秒'), '墙钟回拨不能回拨单调等待时长');
      await page.evaluate(() => { Date.now = originalAckWall; });
      await page.evaluate(() => handleIncomingEvent({kind:'user_input',delivery_id:'dw-dlv-A',turn_id:'raw-turn',text:'fixture dw-dlv-A',data:{lifecycle:'admitted'}}));
      await page.clock.fastForward(1000);
      check((await page.locator('#waitingList').innerText()).includes('未注入'), 'helper 原始 admission 回显不能替代 Hub 三阶段证据');
      await page.evaluate(() => ackUpdate('dw-dlv-A','cli_injected',{turn_id:'actual-turn',occurred_at:'2026-10-27T06:00:32Z'}));
      check(await page.locator('#waitingList .waiting-item').count()===0, '可信 injected 才可移出等待区');
      let timeline = await page.locator('#timeline').innerText();
      check(timeline.includes('已注入执行') && !timeline.includes('回合已结束'), 'injected 必须保持运行中而非提前完成');
      const cards = await page.locator('#timeline > *').count();
      await page.evaluate(() => ackUpdate('dw-dlv-A','turn_processed',{turn_id:'actual-turn',turn_outcome:'completed',occurred_at:'2026-10-27T06:00:33Z'}));
      check((await page.locator('#timeline').innerText()).includes('回合已结束（完成）'), '真实终态显示完成 outcome');
      check(await page.locator('#timeline > *').count()===cards, '终态必须原位更新同一张卡');
      await page.evaluate(() => ackUpdate('dw-dlv-A','helper_queued',{queued_at:'2026-10-27T06:00:00Z'}));
      check((await page.locator('#timeline').innerText()).includes('回合已结束（完成）'), '迟到 queued 不得倒退终态');
      for (const [outcome,label] of [['interrupted','中断'],['failed','失败']]) {
        await page.evaluate(({outcome}) => { ackInput('dw-dlv-'+outcome); ackUpdate('dw-dlv-'+outcome,'cli_injected',{turn_id:'turn-'+outcome}); ackUpdate('dw-dlv-'+outcome,'turn_processed',{turn_id:'turn-'+outcome,turn_outcome:outcome}); }, {outcome});
        check((await page.locator('#timeline').innerText()).includes(`回合已结束（${label}）`), '三种终态 outcome 必须逐值显示：'+outcome);
      }
      await page.evaluate(() => { ackInput('dw-dlv-U'); ackUpdate('dw-dlv-U','helper_queued',{queued_at:new Date().toISOString(),reason:'model_admission_unknown',cancellable:false}); });
      await page.clock.fastForward(31000);
      text = await page.locator('#waitingList').innerText();
      check(text.includes('可能已进入会话／暂停自动重投／需核原回合'), 'admission unknown 必须明确隔离和核原回合');
      check(!text.includes('尚未注入会话'), 'admission unknown 不能被30秒提示误报为仍在排队');
      check(await page.locator('#waitingList .waiting-cancel-btn').count()===0, '不确定派发不可显示可取消动作');
      await page.evaluate(() => { ackInput('dw-dlv-L'); ackUpdate('dw-dlv-L','helper_received',{ack_protocol:1,processing_mode:'legacy',reason:'legacy_injection_unknown',cancellable:false}); });
      timeline = await page.locator('#timeline').innerText();
      check(timeline.includes('旧版 helper 已收／状态无法确认'), 'legacy 收件必须保留状态未知');
      await page.evaluate(() => ackUpdate('dw-dlv-L','cli_injected',{ack_protocol:1,processing_mode:'legacy',cancellable:false}));
      const legacyStage = await page.evaluate(() => c1InputCards.get(c1DeliveryCards.get(deliveryMergeKey('dw-dlv-L'))).stageEl.textContent);
      check(legacyStage==='旧版 helper 已收／状态无法确认', '迟到 v1 injected 不能升级历史投递为真实 admission');
      await page.evaluate(() => { ackInput('dw-dlv-D'); ackUpdate('dw-dlv-D','helper_received',{ack_protocol:1,processing_mode:'desktop',reason:'desktop_inbox_saved',cancellable:false}); });
      check((await page.locator('#timeline').innerText()).includes('已保存到桌面收件箱'), '桌面收件不能被当作模型回合');
      check(errors.length===0, '浏览器不得出现脚本异常：'+errors.join(';'));
      await page.close();
    }
    console.log(JSON.stringify({ assertions, failed:0 }));
  } finally { await browser.close(); }
})().catch(e => { console.error(e.stack); process.exitCode=1; });
