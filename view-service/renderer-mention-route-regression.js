// R-V2-0102-VIEW-MENTION-ROUTE · view 页提及投递的浏览器闸(发送侧与回执)。
// 要点板 runbooks/PLAN-R-V2-0102-VIEW-MENTION-ROUTE-opus50tc2k.md(第二、四、七、八节)。Hub 侧语义在 internal/m8/view_mention_route_test.go。
//
// 装置与口径同 renderer-mention-candidates-regression.js:自带 HTTP 夹具喂仓内真实 renderer-canary-sse.html,真键盘;
// /input 为桩:记下请求体,按需回 Hub 形状的逐目标回执。
//
// MUTATION=<name> 删对应守卫,对应断言必须红:
//    no-mentions-sent · no-text-check · no-receipt · receipt-as-html
// 退出码:1＝有用例失败(变异被杀)· 0＝全绿(变异模式下＝存活)· 3＝变异锚点失效 · 2＝脚本崩溃。
const assert = require('assert');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');

const RENDERER = path.join(__dirname, 'renderer-canary-sse.html');
let html = fs.readFileSync(RENDERER, 'utf8');
const mutation = process.env.MUTATION || '';
if (process.argv.some((a) => a.startsWith('--mutate'))) { console.error('MUTATION-SWITCH-WRONG-FORM: this script reads env MUTATION=<name>, not --mutate=<name>'); process.exit(3); }
function mutate(from, to) {
  const hits = html.split(from).length - 1;
  if (hits !== 1) { console.error(`MUTATION-ANCHOR-STALE: mutation ${mutation} anchor must match exactly once, hits=${hits}: ${from.slice(0, 160)}`); process.exit(3); }
  html = html.replace(from, to);
}
const MUTATIONS = ['mutation-anchor-selftest', 'no-mentions-sent', 'no-text-check', 'no-receipt', 'receipt-as-html', 'no-prune', 'prune-during-ime'];
if (mutation && !MUTATIONS.includes(mutation)) { console.error(`MUTATION-UNKNOWN: ${mutation}`); process.exit(3); }
if (mutation === 'mutation-anchor-selftest') mutate('THIS-TEXT-IS-NOT-IN-THE-PAGE', 'x');
if (mutation === 'no-mentions-sent') mutate('const withMentions = (o) => (mentions.length ? { ...o, mentions } : o);', 'const withMentions = (o) => o;');
if (mutation === 'no-text-check') mutate("if ((i === 0 || /\\s/.test(before)) && !/[A-Za-z0-9.]/.test(after)) return true;", 'if (true) return true;');
if (mutation === 'no-receipt') mutate('        renderMentionReceipts(result && result.mentions);\n', '');
if (mutation === 'no-prune') mutate("humanInput.addEventListener('input', (e) => { if (!e.isComposing) mentionPrunePicked(); });", '');
if (mutation === 'prune-during-ime') mutate("humanInput.addEventListener('input', (e) => { if (!e.isComposing) mentionPrunePicked(); });", "humanInput.addEventListener('input', () => mentionPrunePicked());");
if (mutation === 'receipt-as-html') mutate('row.dataset.status = String(m.status || \'\'); row.textContent = m.message;', 'row.dataset.status = String(m.status || \'\'); row.innerHTML = m.message;');

const NAME = 'dev.zzc';
const CANDS = [
  { full_name: 'beta.zzc', name: 'beta', cli: 'codex', online: true, insert: 'beta' },
  { full_name: 'quill.zzc', name: 'quill', cli: 'claude', online: false, insert: 'quill' },
  { full_name: 'twin.lisa', name: 'twin', cli: 'codex', online: true, insert: 'twin.lisa' },
];
const inputs = [];
let reply = { ok: true };
const server = http.createServer((req, res) => {
  const u = req.url.split('?')[0];
  const json = (o, status = 200) => { res.writeHead(status, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' }); res.end(JSON.stringify(o)); };
  if (u === `/view/${NAME}`) { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' }); return res.end(html); }
  if (u === `/view/${NAME}/mention-candidates`) return json({ candidates: CANDS });
  if (u === `/view/${NAME}/status`) return json({ session: NAME, state: 'idle', readonly: false, can_write: true, auth_mode: 'legacy', owner: 'zzc', model: 'm', cost_total: 0, cost_last: 0, turns: 0 });
  if (u === `/view/${NAME}/input` && req.method === 'POST') {
    let b = ''; req.on('data', (c) => { b += c; }); req.on('end', () => { try { inputs.push(JSON.parse(b)); } catch { inputs.push({ raw: b }); } json(reply); }); return;
  }
  if (u.endsWith('/events')) { res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache', Connection: 'keep-alive' }); res.flushHeaders(); const t = setInterval(() => { try { res.write(': keepalive\n\n'); } catch { /* closed */ } }, 4000); res.on('close', () => clearInterval(t)); return; }
  if (u === '/tasks' || u.endsWith('/tasks')) return json({});
  if (u === '/alerts' || u.endsWith('/alerts')) return json([]);
  if (u === '/favicon.ico') { res.writeHead(204); return res.end(); }
  json({}, 404);
});

let pass = 0; let fail = 0;
async function test(name, fn) { try { await fn(); pass++; console.log(`PASS ${name}`); } catch (e) { fail++; console.log(`FAIL ${name}: ${e.message}`); } }
(async () => {
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const base = `http://127.0.0.1:${server.address().port}`;
  const browser = await chromium.launch(process.env.PW_CHANNEL ? { headless: true, channel: process.env.PW_CHANNEL } : { headless: true });
  const context = await browser.newContext({ viewport: { width: 1000, height: 700 } });
  const page = await context.newPage(); page.setDefaultTimeout(2500);
  const pageErrors = []; const consoleErrors = [];
  page.on('pageerror', (e) => pageErrors.push(e.message));
  page.on('console', (m) => { if (m.type() === 'error') consoleErrors.push(m.text()); });
  const input = page.locator('#humanInput');
  const receipt = page.locator('#mentionReceipt');
  const reset = async () => {
    inputs.length = 0; reply = { ok: true };
    await page.goto(`${base}/view/${NAME}`); await input.waitFor({ state: 'visible' });
    await page.waitForFunction(() => !document.getElementById('humanInput').disabled);
  };
  // 从浮层选一个候选:键入「@前缀」,等浮层第一行是它,回车选中。
  const pick = async (prefix, expectFirst) => {
    await page.keyboard.type('@' + prefix);
    await page.waitForFunction((t) => (document.querySelector('#mentionPopup [aria-selected="true"]')?.textContent || '').includes(t), expectFirst);
    await page.keyboard.press('Enter');
    await page.waitForFunction(() => document.getElementById('mentionPopup').hidden);
  };
  const send = async () => { const n = inputs.length; await page.keyboard.press('Enter'); await page.waitForFunction(() => document.getElementById('humanInput').value === ''); assert.strictEqual(inputs.length, n + 1, 'one POST /input'); return inputs[inputs.length - 1]; };
  try {
    await test('picking a candidate sends its full_name as a structured mention alongside the unchanged text', async () => {
      await reset(); await input.click();
      await pick('be', 'beta（beta.zzc）'); await page.keyboard.type('你好 ');
      await pick('tw', 'twin（twin.lisa）'); await page.keyboard.type('也看看');
      const body = await send();
      assert.strictEqual(body.text, '@beta 你好 @twin.lisa 也看看');
      assert.deepStrictEqual(body.mentions, ['beta.zzc', 'twin.lisa']);
    });

    await test('deleting the inserted @text drops that target; hand-typed @text never adds one', async () => {
      await reset(); await input.click();
      await pick('be', 'beta（beta.zzc）'); await page.keyboard.type('x ');
      await pick('qu', 'quill（quill.zzc）');
      assert.strictEqual(await input.inputValue(), '@beta x @quill ');
      await input.fill('@beta x 改主意了');                                   // 删掉 @quill
      let body = await send();
      assert.deepStrictEqual(body.mentions, ['beta.zzc'], `mentions=${JSON.stringify(body.mentions)}`);
      await input.click(); await pick('be', 'beta（beta.zzc）');
      await input.fill('@betax 粘连不算独立一段');                              // 「@beta」后紧跟名字字符 ⇒ 不是那段插入
      body = await send();
      assert.ok(!('mentions' in body), `glued text must not carry mentions: ${JSON.stringify(body)}`);
      await input.click(); await page.keyboard.type('@beta 纯手打 没有选');
      await page.keyboard.press('Escape');
      body = await send();
      assert.ok(!('mentions' in body), `hand-typed only must not carry mentions: ${JSON.stringify(body)}`);
    });

    // 0.2.1.103 C（R-V2-0103-SMALLFIXES）：选后删掉、再手打同名 ⇒ 不转发；选后接着输入 ⇒ 仍转发；输入法组字中途不误剔除。
    // 断言面＝发送体 mentions；实现承诺＝mentionPrunePicked（与 mentionTargetsFor 同一判据）。
    await test('picked then deleted, then hand-typed again ⇒ not forwarded', async () => {
      await reset(); await input.click();
      await pick('be', 'beta（beta.zzc）');                       // 「@beta 」
      for (let i = 0; i < 6; i++) await page.keyboard.press('Backspace');   // 删掉整段 → 已选集合剔除 beta
      assert.strictEqual(await input.inputValue(), '');
      await page.keyboard.type('@beta 你好'); await page.keyboard.press('Escape');   // 手打同名，不从浮层选
      const body = await send();
      assert.ok(!('mentions' in body), `retyped by hand after deleting must not forward: ${JSON.stringify(body)}`);
    });

    await test('picked and kept while typing on ⇒ still forwarded', async () => {
      await reset(); await input.click();
      await pick('be', 'beta（beta.zzc）'); await page.keyboard.type('接着写一段 ');
      const body = await send();
      assert.deepStrictEqual(body.mentions, ['beta.zzc']);
    });

    await test('an input event during IME composition does not drop a pick', async () => {
      await reset(); await input.click();
      await pick('be', 'beta（beta.zzc）');
      await page.evaluate(() => {   // 组字中途正文暂时不含「@beta」（如输入法替换选区），随后恢复
        const el = document.getElementById('humanInput'); const keep = el.value;
        el.value = '拼'; el.dispatchEvent(new InputEvent('input', { bubbles: true, isComposing: true }));
        el.value = keep + '你好'; el.dispatchEvent(new InputEvent('input', { bubbles: true, isComposing: true }));
      });
      const body = await send();
      assert.deepStrictEqual(body.mentions, ['beta.zzc'], 'composition input must not prune');
    });

    await test('no mention picked ⇒ the request body has exactly the old shape', async () => {
      await reset(); await input.click(); await page.keyboard.type('普通一句');
      const body = await send();
      assert.deepStrictEqual(body, { text: '普通一句' });
    });

    await test('per-target receipts from the Hub show under the input, then clear on the next plain send', async () => {
      await reset(); await input.click();
      await pick('be', 'beta（beta.zzc）');
      reply = { ok: true, mentions: [
        { full_name: 'beta.zzc', status: 'delivered', message: '已转给 beta，回复请到 beta 的页面查看' },
        { full_name: 'quill.zzc', status: 'queued', message: 'quill 不在线，已排队；回复请到 quill 的页面查看' },
        { full_name: 'twin.lisa', status: 'rejected', reason: 'cross_owner', message: '未转给 twin.lisa：暂不支持转给其他用户的 Agent<b>x</b>' },
      ] };
      await send();
      await receipt.waitFor({ state: 'visible' });
      const rows = await page.locator('#mentionReceipt .mention-receipt-row').evaluateAll((els) => els.map((e) => `${e.dataset.status}|${e.textContent}`));
      assert.deepStrictEqual(rows, [
        'delivered|已转给 beta，回复请到 beta 的页面查看',
        'queued|quill 不在线，已排队；回复请到 quill 的页面查看',
        'rejected|未转给 twin.lisa：暂不支持转给其他用户的 Agent<b>x</b>',
      ]);
      assert.strictEqual(await page.locator('#mentionReceipt b').count(), 0, 'Hub text must not be parsed as HTML');
      reply = { ok: true };
      await input.click(); await page.keyboard.type('再来一句'); await send();
      assert.strictEqual(await receipt.isHidden(), true, 'receipts clear on the next send without mentions');
    });

    await test('no pageerror and no unexpected console error across the whole run', async () => {
      assert.deepStrictEqual(pageErrors, []);
      assert.deepStrictEqual(consoleErrors.filter((t) => !/status of (404|500)/.test(t)), []);
    });
  } finally { await context.close(); await browser.close(); await new Promise((resolve) => server.close(resolve)); }
  console.log(`mention route browser: passed=${pass} failed=${fail} mutation=${mutation || 'none'}`);
  if (mutation) console.log(`MUTATION-VERDICT: ${mutation} ${fail >= 1 ? 'KILLED' : 'SURVIVED'}`);
  if (fail) process.exit(1);
})().catch((e) => { console.error(e); process.exit(2); });
