// renderer 重连重放用例:真 EventSource 断线重连后,hub 重放 ring 的同一批事件,
// 验证同一回合的正文**不会**被二次累加(修复 E 的端到端证据)。
// 用法:
//   RENDERER=<件> RING_TURNS=20 RECONNECT=1 python3 renderer-sse-fixture.py 19460 &
//   PORT=19460 SECS=30 node renderer-sse-reconnect.js
// 2026-08-29 实测(7964 字正文,20 chunk,ring 20 回合,30 秒内约 10 次重连):
//   c7172424(无 E,冒烟当时线上那版) 6858→13833→34758→69633 字,assistant 事件 20→200,仍在涨
//   8a1b6017(有 E)                  全程 6858 字 / 20 条,纹丝不动
// ⚠️ fixture 的 RECONNECT 模式必须真的关掉底层连接(close_connection + shutdown),
//    只 return 在 HTTP/1.1 keep-alive 下浏览器不认为断线、不会重连,本用例会**静默空转**。
//    历史上就踩过一次:第一版跑出来全程 20 条,差点被误读成"没有该缺陷"。
const { chromium } = require('playwright');
const PORT = process.env.PORT || 19460;
const SECS = Number(process.env.SECS || 30);
(async () => {
  const b = await chromium.launch({ headless: true });
  const page = await b.newPage({ viewport: { width: 1280, height: 900 } });
  const errs = []; page.on('pageerror', e => errs.push(e.message));
  await page.goto(`http://127.0.0.1:${PORT}/renderer`, { waitUntil: 'domcontentloaded' });
  const snap = () => page.evaluate(() => {
    const g = document.querySelector('#timeline > [data-turn-key="t-1"]');
    const el = g && g.querySelector('.turn-assistant .event-content');
    let chars = 0;
    if (el) { const c = el.cloneNode(true); c.querySelectorAll('.event-meta').forEach(n=>n.remove()); chars = c.innerText.length; }
    const grp = turnGroups.get('t-1');
    return { chars, assistants: grp ? grp.events.filter(e=>e.kind==='assistant_text').length : -1,
      cards: document.getElementById('timeline').children.length };
  }).catch(e => ({ err: String(e).slice(0,60) }));
  for (let t = 5; t <= SECS; t += 5) {
    await new Promise(r => setTimeout(r, 5000));
    const s = await snap();
    console.log(`  t=${String(t).padStart(2)}s  ${JSON.stringify(s)}`);
    if (s.err) break;
  }
  if (errs.length) console.log('  异常:', errs.slice(0,2).join(' | '));
  await b.close();
})();
