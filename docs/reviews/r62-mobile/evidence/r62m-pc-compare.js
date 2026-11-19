// S-R62-MOBILE 一次性证据：PC 1440×900 像素级不变对照（基锚 bef01c57c 外壳 vs 移动化外壳）。
const assert = require('assert');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { execSync } = require('child_process');
const { chromium } = require('playwright');
const ROOT = '/tmp/wt-r62-mobile';
const BASE = 'bef01c57c465cd0fca9eabd6e94b16ae919040e1';
function tpl(source, start, end) {
  const a = source.indexOf(start); assert.notStrictEqual(a, -1, start);
  const b = source.indexOf(end, a + start.length); assert.notStrictEqual(b, -1, end);
  return source.slice(a + start.length, b);
}
function sub(s) { return s.split('{{CONSOLE_VERSION}}').join('7.7.7.7-harness').split('{{CONSOLE_COMMIT}}').join('c0ffee1234567890').split('{{CONSOLE_VERSION_TEXT}}').join('7.7.7.7-harness'); }
const baseGo = execSync(`git -C ${ROOT} show ${BASE}:internal/m8/console_auth.go`, { encoding: 'utf8', maxBuffer: 64 * 1024 * 1024 });
const newGo = fs.readFileSync(path.join(ROOT, 'internal/m8/console_auth.go'), 'utf8');
const baseShell = sub(tpl(baseGo, 'const consolePageHTML = `', '`\n\nfunc (h *Hub) HandleConsoleAgents'));
const newShell = sub(tpl(newGo, 'const consolePageHTML = `', '`\n\nfunc (h *Hub) HandleConsoleAgents'));
const baseLoginGo = execSync(`git -C ${ROOT} show ${BASE}:internal/m8/console_login.go`, { encoding: 'utf8' });
const newLoginGo = fs.readFileSync(path.join(ROOT, 'internal/m8/console_login.go'), 'utf8');
const baseLogin = tpl(baseLoginGo, 'fmt.Fprintf(w, `', '`, html.EscapeString').replace('%s', 'abcd1234').replace('%q', '"abcd1234"');
const newLogin = tpl(newLoginGo, 'fmt.Fprintf(w, `', '`, html.EscapeString').replace('%s', 'abcd1234').replace('%q', '"abcd1234"');
const renderer = fs.readFileSync(path.join(ROOT, 'view-service/renderer-canary-sse.html'), 'utf8');
const json = (res, v) => { res.writeHead(200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(v)); };
const html = (res, b) => { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' }); res.end(b); };
const historyPage = () => ({ generation: 'g', high_water: 150, events: Array.from({ length: 150 }, (_, i) => ({ cursor: { generation: 'g', seq: i + 1 }, event_id: `h-${i + 1}`, received_at: new Date(1700000000000 + i).toISOString(), event: JSON.stringify({ kind: 'assistant_text', text: `PC_COMPARE_LINE_${i + 1}`, delivery_id: `d-${i + 1}`, turn_id: `t-${i + 1}`, stage: 'cli_injected', ts: 1700000000000 + i }) })), gaps: [], has_older: false, before_seq: null, gap_before: false });
const server = http.createServer((req, res) => {
  const url = new URL(req.url, 'http://fixture');
  if (url.pathname === '/base/console') return html(res, baseShell);
  if (url.pathname === '/new/console') return html(res, newShell);
  if (url.pathname === '/base/login') return html(res, baseLogin);
  if (url.pathname === '/new/login') return html(res, newLogin);
  const view = url.pathname.match(/^\/view\/([^/]+)(\/.*)?$/);
  if (view) {
    const s = view[2] || '';
    if (s === '') return html(res, renderer);
    if (s === '/status') return json(res, { session: view[1], state: 'running', readonly: true, can_write: false, auth_mode: 'legacy', owner: 'owner', model: 'm', cost_total: 0, cost_last: 0, turns: 1 });
    if (s === '/tasks' || s === '/page') return json(res, {});
    if (s === '/history') return json(res, historyPage());
    if (s === '/events') { res.writeHead(200, { 'Content-Type': 'text/event-stream' }); res.write(`data: ${JSON.stringify({ kind: 'assistant_text', text: 'PC_COMPARE_LIVE', delivery_id: 'live', turn_id: 'live' })}\n\n`); req.on('close', () => { try { res.end(); } catch (_) {} }); return; }
  }
  if (url.pathname === '/api/console/platform') return json(res, { role: 'owner', actions: ['read'], items: [{ id: 'agent-groups', kind: 'agent-groups', title: 'Agent 管理', href: '#agent-groups', status: 'approved' }] });
  if (url.pathname === '/api/console/agents') return json(res, { agents: [{ agent_id: 'id-1', name: 'mobile', full_name: 'owner-mobile-2642', origin: 'selfhost', group_code: '', online: true }] });
  if (url.pathname === '/api/console/agent-groups') return json(res, { groups: [], owned_agents: [] });
  if (url.pathname === '/favicon.ico') { res.writeHead(204); return res.end(); }
  if (req.method === 'POST') { req.resume(); return json(res, { ok: true }); }
  res.writeHead(404); res.end();
});
(async () => {
  await new Promise((r) => server.listen(0, '127.0.0.1', r));
  const base = `http://127.0.0.1:${server.address().port}`;
  const browser = await chromium.launch({ headless: true });
  const out = '/tmp/r62-evidence-mobile';
  fs.mkdirSync(out, { recursive: true });
  let mismatches = 0;
  try {
    for (const variant of ['base', 'new']) {
      const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
      const page = await ctx.newPage();
      await page.clock.install({ time: new Date('2026-11-18T08:00:00Z') });
      await page.goto(`${base}/${variant}/console`);
      await page.locator('#a button').first().waitFor();
      await page.screenshot({ path: `${out}/pc-workbench-${variant}.png`, animations: 'disabled' });
      await page.locator('#a button').first().click();
      const frame = page.frames().find((f) => f.url().includes('/view/owner-mobile-2642'));
      await frame.waitForFunction(() => document.getElementById('timeline') && document.getElementById('timeline').children.length >= 100, null, { timeout: 15000 });
      // 滚动进度条是瞬态动画：滚到底使其满格稳定，再做像素对照。
      await frame.evaluate(() => {
        let el = document.getElementById('timeline');
        while (el && el !== document.body) {
          const style = getComputedStyle(el);
          if (el.scrollHeight > el.clientHeight + 10 && /(auto|scroll)/.test(style.overflowY)) break;
          el = el.parentElement;
        }
        if (el && el !== document.body) el.scrollTop = el.scrollHeight;
      });
      // 冻结时钟下加载条的隐藏定时器不会自己 firing：推进 mock 时钟触发隐藏，
      // 两变体推进量相同 ⇒ 状态条时间文本仍一致；再留真实 300ms 让 CSS 过渡收尾。
      await page.clock.runFor(3000);
      await page.waitForTimeout(300);
      await page.screenshot({ path: `${out}/pc-session-${variant}.png`, animations: 'disabled' });
      await page.goto(`${base}/${variant}/login`);
      await page.screenshot({ path: `${out}/pc-login-${variant}.png`, animations: 'disabled' });
      await ctx.close();
    }
    for (const name of ['pc-workbench', 'pc-session', 'pc-login']) {
      const a = fs.readFileSync(`${out}/${name}-base.png`);
      const b = fs.readFileSync(`${out}/${name}-new.png`);
      const same = a.equals(b);
      console.log(`${name}: base=${a.length}B new=${b.length}B identical=${same}`);
      if (!same) mismatches += 1;
    }
    console.log(mismatches ? 'PC_PIXEL_MISMATCH' : 'PC_PIXEL_IDENTICAL');
    process.exitCode = mismatches ? 1 : 0;
  } finally { await browser.close(); server.close(); }
})().catch((e) => { console.error(e); server.close(); process.exitCode = 1; });
