// 业主 2026-09-11 报的两个现象,逐格钉死:
//   甲「飞书头像不见了」
//   乙「Agent 投递过来的消息,并没有显示为 Agent 头像」
//
// 🔴 根因不在图标本身 —— PROV_SVG 里四个来源 SVG 一直都在。
// 缺的是 window.PROVENANCE_ICONS_V1:provenanceIconHtml 第一道门就是
//   if (!provFlagOn() || !norm.prov || !norm.prov.trusted) return '';
// 而 VS 从未注入该变量 ⇒ 恒假 ⇒ 四个图标全是死代码,只剩按事件类型给的表情兜底
//   (user_input→👤、assistant_text→🤖),于是 Agent 投递显示成 👤、飞书什么都没有。
//
// 因此本测试**必须显式关掉 flag 跑一遍**(⑤⑥ 两格),否则「开着 flag 图标出现」
// 这件事无法区分是本次修好的、还是本来就好的 —— 那就成了不可能失败的检查。
const assert = require('assert');
const { finishRegression, launchChromium, startRendererServer } = require('./renderer-regression-selfhost');

const provEvent = (kind, displayName, id) => ({
  schema_version: 'delivery.v2', kind: 'input_posted',
  message_id: 'msg_' + id, delivery_id: 'dw-dlv_' + id, stage: 'hub_received',
  payload: '来源探针-' + kind,
  source: { kind: kind === 'feishu' ? 'feishu' : 'agent', display_name: displayName, trust_decision: 'trusted' },
  delivery: { role: 'primary' },
  provenance: { kind, display_name: displayName, attested_by: 'hub', trust_decision: 'trusted' },
  cancellable: false, updated_at: new Date(0).toISOString(),
});

async function boot(browser, baseUrl, flagOn) {
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  await page.route('**/status', r => r.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({
    session: 'fixture-dev55-2642', state: 'idle', readonly: true, can_write: false,
    owner: 'fixture', model: 'fixture-model', cost_total: 0, cost_last: 0, turns: 0 }) }));
  await page.route('**/events', r => r.fulfill({ status: 200, contentType: 'text/event-stream', body: '' }));
  await page.addInitScript(on => { window.PROVENANCE_ICONS_V1 = on; }, flagOn);
  await page.goto(`${baseUrl}?mock=1&unlocked=1`, { waitUntil: 'domcontentloaded' });
  await page.evaluate(() => { clearInterval(statusTimer); resetConversation(); finishTimelineReplay(false); });
  return page;
}

(async () => {
  const fixture = await startRendererServer();
  const baseUrl = fixture.url;
  let browser;
  try {
    // 服务出的内容 == 磁盘上的文件(这条自检写进过部署单,一晚栽过两次陈旧静态服务)
    const disk = require('crypto').createHash('sha256')
      .update(require('fs').readFileSync(__dirname + '/renderer-canary-sse.html')).digest('hex');
    const served = require('crypto').createHash('sha256')
      .update(Buffer.from(await (await fetch(baseUrl)).arrayBuffer())).digest('hex');
    assert.strictEqual(served, disk, `served != disk —— 测的不是本目录的文件\n  served=${served}\n  disk  =${disk}`);

    browser = await launchChromium();
    // ── flag 开:业主要的两个图标都必须出现 ───────────────────────────
    const on = await boot(browser, baseUrl, true);
    await on.evaluate(ev => handleIncomingEvent(ev), provEvent('feishu', '飞书', 'fs1'));
    await on.evaluate(ev => handleIncomingEvent(ev), provEvent('agent', 'zzc-prodops-2642', 'ag1'));

    // ① 甲:飞书来源必须有来源图标
    assert.strictEqual(await on.locator('.prov-ic[aria-label*="飞书"]').count(), 1,
      '① 飞书来源必须渲染飞书头像(业主:「飞书不见了」)');
    // ② 乙:Agent 投递必须是 Agent 头像,不是 👤
    assert.strictEqual(await on.locator('.prov-ic[aria-label*="Agent"], .prov-ic[title*="zzc-prodops-2642"]').count(), 1,
      '② Agent 投递必须渲染 Agent 头像(业主:「并没有显示为 Agent 头像」)');
    // ③ 两张卡各自只有一个来源图标,不得叠加表情
    assert.strictEqual(await on.locator('.prov-ic').count(), 2, '③ 两条来源各出且仅出一个来源图标');
    // ④ flag 开时 input 卡不得再挂 👤 类型标(否则「人/👤 双标」回归)
    assert.strictEqual(await on.locator('.event-type').filter({ hasText: '👤' }).count(), 0,
      '④ flag 开时输入卡不得再出 👤 类型标');
    await on.close();

    // ── flag 关:阳性对照,证明上面四格确实是 flag 带来的 ───────────────
    const off = await boot(browser, baseUrl, false);
    await off.evaluate(ev => handleIncomingEvent(ev), provEvent('feishu', '飞书', 'fs2'));
    await off.evaluate(ev => handleIncomingEvent(ev), provEvent('agent', 'zzc-prodops-2642', 'ag2'));
    // ⑤ flag 关必须一个来源图标都没有 —— 这正是业主看到的线上状态
    assert.strictEqual(await off.locator('.prov-ic').count(), 0,
      '⑤ 阳性对照失效:flag 关时竟仍有来源图标,说明①②不是 flag 起的作用');
    await off.close();

    console.log('renderer-provenance-icon-regression: 5/5 通过(含 flag 关的阳性对照)');
  } finally {
    if (browser) await browser.close();
    await fixture.close();
  }
})().catch(finishRegression);
