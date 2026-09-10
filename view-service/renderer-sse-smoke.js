// renderer 端到端冒烟:真 EventSource + 真节奏,盯异常 / 长任务 / 内容完整性 / 主线程可响应。
// 配套 renderer-sse-fixture.py 使用:
//   RENDERER=<件> python3 renderer-sse-fixture.py 19400 &
//   PORT=19400 [PRELOAD=200] [BUDGET_MS=90000] node renderer-sse-smoke.js
// PRELOAD 预置历史卡:现网 View 页开场就带历史,空时间线不是真实起点。
const { chromium } = require('playwright');
const PORT = process.env.PORT || 19400;
const BUDGET_MS = Number(process.env.BUDGET_MS || 25000);
(async () => {
  const b = await chromium.launch({ headless: true });
  const page = await b.newPage({ viewport: { width: 1280, height: 900 } });
  const errs = [], warns = [];
  page.on('pageerror', e => errs.push('PAGEERROR: ' + e.message + '\n' + (e.stack||'').split('\n').slice(0,12).join('\n')));
  page.on('console', m => { if (m.type()==='error') errs.push('CONSOLE: '+m.text()); });
  await page.goto(`http://127.0.0.1:${PORT}/renderer`, { waitUntil: 'domcontentloaded' });
  await page.evaluate(() => {
    window.__long = [];
    try { new PerformanceObserver(l => { for (const e of l.getEntries()) window.__long.push(Math.round(e.duration)); })
      .observe({ entryTypes: ['longtask'] }); } catch (e) {}
  });
  // 预置历史卡片:现网 View 页开场就带着 N 张历史卡,空时间线不是真实起点
  const PRELOAD = Number(process.env.PRELOAD || 0);
  if (PRELOAD) {
    await page.evaluate((n) => {
      for (let i = 0; i < n; i += 1) {
        handleIncomingEvent({ kind: 'user_input', text: `历史问题 ${i}`, delivery_id: `h-${i}`,
          turn_id: `ht-${i}`, stage: 'cli_injected', cursor: `h${String(i).padStart(5,'0')}.1` });
        handleIncomingEvent({ kind: 'assistant_text',
          text: `历史回答 ${i}\n\n| 列A | 列B |\n|---|---|\n| ${i} | 值 |\n\n- 要点一\n- 要点二\n`,
          delivery_id: `h-${i}`, turn_id: `ht-${i}`, cursor: `h${String(i).padStart(5,'0')}.2` });
        handleIncomingEvent({ kind: 'state_change', data: { change: 'turn_completed' },
          delivery_id: `h-${i}`, turn_id: `ht-${i}`, cursor: `h${String(i).padStart(5,'0')}.3` });
      }
      if (window.__dwRender) window.__dwRender.flush();
    }, PRELOAD);
    console.log(`预置历史卡 ${PRELOAD} 组,时间线现有 ${await page.evaluate(()=>document.getElementById('timeline').children.length)} 张`);
  }
  const t0 = Date.now();
  let done = false;
  while (Date.now() - t0 < BUDGET_MS) {
    const st = await page.evaluate(() => {
      const g = document.querySelector('#timeline > [data-turn-key="t-1"]');
      const el = g && g.querySelector('.turn-assistant .event-content');
      let chars = 0;
      if (el) { const c = el.cloneNode(true); c.querySelectorAll('.event-meta').forEach(n=>n.remove()); chars = c.innerText.length; }
      return { chars, completed: !!(g && g.querySelector('.turn-completed')),
               targetPresent: !!g,
               cards: document.getElementById('timeline').children.length,
               long: window.__long.length, longMax: Math.max(0, ...window.__long) };
    }).catch(e => ({ err: String(e) }));
    if (st.err) { console.log('EVAL FAIL(主线程可能已卡死):', st.err); break; }
    if (st.completed && st.chars > 0) { done = true;
      console.log(`完成 @${Date.now()-t0}ms  正文 ${st.chars} 字  卡片 ${st.cards}  长任务 ${st.long} 次(最长 ${st.longMax}ms)`);
      break; }
    await new Promise(r => setTimeout(r, 500));
  }
  if (!done) {
    const st = await page.evaluate(() => {
      const g = document.querySelector('#timeline > [data-turn-key="t-1"]');
      const el = g && g.querySelector('.turn-assistant .event-content');
      let chars = 0;
      if (el) { const c = el.cloneNode(true); c.querySelectorAll('.event-meta').forEach(n=>n.remove()); chars = c.innerText.length; }
      return { chars, targetPresent: !!g, cards: document.getElementById('timeline').children.length,
        long: window.__long.length, longMax: Math.max(0, ...window.__long),
        pending: window.__dwRender ? window.__dwRender.pending() : 'n/a',
        esState: (typeof eventSource !== 'undefined' && eventSource) ? eventSource.readyState : 'n/a' };
    }).catch(e => ({ err: String(e) }));
    console.log('未完成:', JSON.stringify(st));
  }
  // 交互性:超时预算内主线程能否响应
  const ti = Date.now();
  await page.evaluate(() => 1).catch(()=>{});
  console.log('主线程往返', Date.now()-ti, 'ms');
  if (errs.length) { console.log('--- 页面异常 ---'); errs.slice(0,10).forEach(e=>console.log('  '+e)); }
  else console.log('无页面异常');
  await b.close();
})();
