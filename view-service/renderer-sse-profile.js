// renderer 回放路径耗时归因:把「刷新后多久才出内容」分摊到
// rebuildTurn / renderMarkdown / scrollTimelineToBottom(强制同步重排) / enforceTimelineCap。
// 配套 renderer-sse-fixture.py 的 RING_TURNS 使用(模拟刷新时 hub 重放 ring):
//   RENDERER=<件> RING_TURNS=250 python3 renderer-sse-fixture.py 19450 &
//   PORT=19450 node renderer-sse-profile.js
// 2026-08-29 实测(250 回合 ring + 8K 长回复):
//   修复前 d4fd8edc  137233ms(其中 renderMarkdown 136126ms / 1051 次,时间线 252 张=上限失效)
//   修复后 8a1b6017     745ms(renderMarkdown 29ms / 251 次,时间线 200 张)
//   ring 放大到 800 回合,修复后仍 1364ms。
const { chromium } = require('playwright');
const PORT = process.env.PORT || 19450;
(async () => {
  const b = await chromium.launch({ headless: true });
  const page = await b.newPage({ viewport: { width: 1280, height: 900 } });
  const errs = []; page.on('pageerror', e => errs.push(e.message));
  await page.addInitScript(() => {
    window.__P = { rebuildTurn:0, renderMarkdown:0, scrollBottom:0, enforceCap:0 };
    window.__N = { rebuildTurn:0, renderMarkdown:0, scrollBottom:0, enforceCap:0 };
    const wrap = (name, key) => {
      const it = setInterval(() => {
        if (typeof window[name] !== 'function') return;
        clearInterval(it);
        const orig = window[name];
        window[name] = function (...a) {
          const s = performance.now();
          try { return orig.apply(this, a); }
          finally { window.__P[key] += performance.now() - s; window.__N[key] += 1; }
        };
      }, 1);
    };
    wrap('rebuildTurn','rebuildTurn'); wrap('renderMarkdown','renderMarkdown');
    wrap('scrollTimelineToBottom','scrollBottom'); wrap('enforceTimelineCap','enforceCap');
  });
  const t0 = Date.now();
  await page.goto(`http://127.0.0.1:${PORT}/renderer`, { waitUntil: 'domcontentloaded' });
  // 等目标回合完成
  let doneAt = null;
  for (let i = 0; i < 120; i++) {
    const r = await page.evaluate(() => {
      const g = document.querySelector('#timeline > [data-turn-key="t-1"]');
      return { ok: !!(g && g.querySelector('.turn-completed')),
               cards: document.getElementById('timeline').children.length };
    }).catch(()=>({ok:false,cards:-1}));
    if (r.ok) { doneAt = Date.now() - t0; break; }
    await new Promise(r2 => setTimeout(r2, 500));
  }
  const p = await page.evaluate(() => ({ ...window.__P, n: window.__N,
    cards: document.getElementById('timeline').children.length }));
  console.log(`目标回合渲染完成 @${doneAt === null ? '>60s(未完成)' : doneAt + 'ms'}  时间线 ${p.cards} 张`);
  const row = (label, ms, n, note='') => console.log(`  ${label.padEnd(24)} ${String(Math.round(ms)).padStart(7)}ms  ${String(n).padStart(6)} 次  ${note}`);
  row('rebuildTurn', p.rebuildTurn, p.n.rebuildTurn);
  row('renderMarkdown', p.renderMarkdown, p.n.renderMarkdown, '(含在 rebuildTurn 内)');
  row('scrollTimelineToBottom', p.scrollBottom, p.n.scrollBottom, '← 每次强制同步重排');
  row('enforceTimelineCap', p.enforceCap, p.n.enforceCap);
  if (errs.length) console.log('  异常:', errs.slice(0,3).join(' | '));
  await b.close();
})();
