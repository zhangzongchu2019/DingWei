// T-074:同一笔投递的两个名字(dlv_X / dw-dlv_X)必须收敛到同一张卡;
// 而出站寻址必须仍用 helper 认的 dw- 形式。
// 🔴 两格缺一不可:只验「合成一张卡」会让「把前缀一刀切掉」通过,而那会让取消静默失效。
const { chromium } = require('playwright');
const assert = require('assert');
const base = process.env.RENDERER_URL || 'http://127.0.0.1:19312/renderer-canary-sse.html';

(async () => {
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route('**/status', r => r.fulfill({ status:200, contentType:'application/json', body: JSON.stringify({
      session:'fixture', state:'idle', readonly:true, can_write:false, owner:'f', model:'m', cost_total:0, cost_last:0, turns:0 })}));
    await page.route('**/events', r => r.fulfill({ status:200, contentType:'text/event-stream', body:'' }));
    await page.goto(`${base}?mock=1&unlocked=1`, { waitUntil:'domcontentloaded' });
    await page.evaluate(() => { clearInterval(statusTimer); resetConversation(); finishTimelineReplay(false); });

    // ① 归一函数本身:两种写法收敛到同一个合卡键
    const keys = await page.evaluate(() => ({
      bare: deliveryMergeKey('dlv_abc'), pref: deliveryMergeKey('dw-dlv_abc'),
      addrFromBare: deliveryAddrId('dlv_abc'), addrFromPref: deliveryAddrId('dw-dlv_abc'),
      addrEmpty: deliveryAddrId(''),
    }));
    assert.strictEqual(keys.bare, keys.pref, '① 两种写法必须收敛到同一个合卡键');
    // ② 出站寻址必须是 helper 认的 dw- 形式(与 helper _delivery_id 规则一致)
    assert.strictEqual(keys.addrFromBare, 'dw-dlv_abc', '② 裸号出站须补 dw- 前缀');
    assert.strictEqual(keys.addrFromPref, 'dw-dlv_abc', '② 已带前缀不得重复加');
    assert.strictEqual(keys.addrEmpty, '', '② 空值不得编造出 "dw-"');

    // 🔴 阳性对照:两个函数必须真的不同 —— 若有人把它们写成同一个,上面两格会同时"通过"
    assert.notStrictEqual(keys.bare, keys.addrFromBare, '🔴 合卡键与出站寻址不得是同一个值,否则判据是空的');

    // ③ 端到端:同一笔投递的两个名字只产生一张卡
    await page.evaluate(() => {
      handleIncomingEvent({ kind:'input_posted', text:'同一条输入', message_id:'m-x',
        delivery_id:'dlv_same', stage:'cli_injected', cursor:'1' });
      handleIncomingEvent({ kind:'user_input', text:'同一条输入', message_id:'m-x',
        delivery_id:'dw-dlv_same', data:{ lifecycle:'admitted' }, turn_id:'', cursor:'2' });
    });
    await page.waitForTimeout(200);
    const n = await page.evaluate(() => document.querySelectorAll('#timeline > *').length);
    assert.strictEqual(n, 1, `③ 同一笔投递两个名字须只产生一张卡,实为 ${n} 张`);

    console.log('T-074 delivery-id regression: ALL PASS');
  } finally { await browser.close(); }
})();
