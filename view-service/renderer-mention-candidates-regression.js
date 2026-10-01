// V11 · view 页输入框键入＠弹出同组 Agent 候选的浏览器闸。
// 派工 R-NEWPLAT-V11-tosonnet50ts-20261001(组长 2026-10-01)· 依据 PLAN-SPEC-ITERATION-THIN-SH 9.5/9.6(业主裁定)
//
// 装置:自带 HTTP 夹具,把仓内真实 renderer-canary-sse.html 当作 /view/<full_name> 页喂给真 Chrome;
// 候选接口为【提案】GET /view/<full_name>/mention-candidates(Hub 侧待认领),这里用桩。
// 口径:真键盘(click + keyboard.type/press);pageerror 与 console error 两路监听,末尾断言为空(只放行桩有意回的 4xx/5xx 资源行)。
//
// MUTATION=<name> 删除对应守卫,对应那条断言必须红(判"首红是不是该条"):
//    enter-sends-while-open · ignore-insert-field · no-stale-guard · trigger-mid-word · no-ime-guard · enter-ignored-when-no-items
// 环境变量: PW_CHANNEL=chrome 用系统 Chrome;NODE_PATH 指向含 playwright 的 node_modules。
const assert = require('assert');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');

const RENDERER = path.join(__dirname, 'renderer-canary-sse.html');
let html = fs.readFileSync(RENDERER, 'utf8');
const mutation = process.env.MUTATION || '';
function mutate(from, to) {
  const hits = html.split(from).length - 1;
  assert.strictEqual(hits, 1, `mutation anchor must match exactly once: ${from} hits=${hits}`);
  html = html.replace(from, to);
}
const MUTATIONS = ['enter-sends-while-open', 'ignore-insert-field', 'no-stale-guard', 'trigger-mid-word', 'no-ime-guard', 'enter-ignored-when-no-items', 'refetch-while-loading', 'fixed-max-height'];
if (mutation && !MUTATIONS.includes(mutation)) throw new Error(`unknown mutation ${mutation}`);
if (mutation === 'enter-sends-while-open') mutate("e.preventDefault(); e.stopImmediatePropagation(); mentionSelect(mention.active); }\n    }, true);", "mentionSelect(mention.active); }\n    }, true);");
if (mutation === 'ignore-insert-field') mutate("const insert = it.insert || it.full_name || it.name || '';", "const insert = it.name || '';");
if (mutation === 'no-stale-guard') mutate("if (gen !== mention.gen) return;                       // 过期应答不得覆盖新应答", '');
if (mutation === 'trigger-mid-word') mutate("/(^|\\s)@([A-Za-z0-9.]*)$/.exec(", "/()@([A-Za-z0-9.]*)$/.exec(");
if (mutation === 'no-ime-guard') mutate("if (!mention.open || e.isComposing) return;", "if (!mention.open) return;");
if (mutation === 'enter-ignored-when-no-items') mutate("if (!mentionSelectable()) return;\n      const n = mentionMatches().length;", "const n = Math.max(1, mentionMatches().length);");
if (mutation === 'refetch-while-loading') mutate("      if (mention.status === 'loading' && !force) return;   // 沿用在途那次,慢接口下连敲不重复请求\n", '');
if (mutation === 'fixed-max-height') mutate("mentionPopup.style.maxHeight = `${Math.max(64, Math.min(232, Math.floor(composerInner.getBoundingClientRect().top) - 14))}px`;", "mentionPopup.style.maxHeight = '232px';");

const NAME = 'dev.zzc';
let candidates = [];
let candidateMode = 'ok';     // ok | error | slow-first | slow-all
let candidateGets = 0;
const inputs = [];
const server = http.createServer((req, res) => {
  const u = req.url.split('?')[0];
  const json = (o, status = 200) => { res.writeHead(status, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' }); res.end(JSON.stringify(o)); };
  if (u === `/view/${NAME}`) { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' }); return res.end(html); }
  if (u === `/view/${NAME}/mention-candidates`) {
    candidateGets += 1; const nth = candidateGets;
    if (candidateMode === 'error') return json({ error: 'internal' }, 500);
    const body = { candidates };
    if (candidateMode === 'slow-first' && nth === 1) return setTimeout(() => json(body), 700);
    if (candidateMode === 'slow-all') return setTimeout(() => json(body), 400);
    return json(body);
  }
  if (u === `/view/${NAME}/status`) return json({ session: NAME, state: 'idle', readonly: false, can_write: true, auth_mode: 'legacy', owner: 'zzc', model: 'm', cost_total: 0, cost_last: 0, turns: 0 });
  if (u === `/view/${NAME}/input` && req.method === 'POST') {
    let b = ''; req.on('data', (c) => { b += c; }); req.on('end', () => { try { inputs.push(JSON.parse(b)); } catch { inputs.push({ raw: b }); } json({ ok: true }); }); return;
  }
  if (u.endsWith('/events')) { res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache', Connection: 'keep-alive' }); res.flushHeaders(); const t = setInterval(() => { try { res.write(': keepalive\n\n'); } catch { /* closed */ } }, 4000); res.on('close', () => clearInterval(t)); return; }
  if (u === '/tasks' || u.endsWith('/tasks')) return json({});
  if (u === '/alerts' || u.endsWith('/alerts')) return json([]);
  if (u === '/favicon.ico') { res.writeHead(204); return res.end(); }
  json({}, 404);
});

const CANDS = [
  { full_name: 'alpha.zzc', name: 'alpha', cli: 'claude', online: true, insert: 'alpha' },
  { full_name: 'beta.zzc', name: 'beta', cli: 'codex', online: true, insert: 'beta' },
  { full_name: 'gamma.zzc', name: 'gamma', cli: 'opencode', online: false, insert: 'gamma' },
  { full_name: 'twin.zzc', name: 'twin', cli: 'deepseek', online: true, insert: 'twin.zzc' },   // 同短名:Hub 判歧义 ⇒ 插全名
  { full_name: 'twin.lisa', name: 'twin', cli: 'weirdcli', online: true, insert: 'twin.lisa' },
];

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
  const popup = page.locator('#mentionPopup');
  const rowsText = () => page.locator('#mentionPopup .mention-row:not(.mention-note)').evaluateAll((els) => els.map((e) => e.textContent));
  const reset = async (list = CANDS, mode = 'ok') => {
    candidates = list; candidateMode = mode; candidateGets = 0; inputs.length = 0;
    await page.goto(`${base}/view/${NAME}`); await input.waitFor({ state: 'visible' });
    await page.waitForFunction(() => !document.getElementById('humanInput').disabled);
  };
  const typeAt = async (text) => { await input.click(); await page.keyboard.type(text); };
  try {
    await reset();

    await test('typing @ at the start opens a popup above the input, right next to it, with the first row highlighted', async () => {
      await typeAt('@');
      await popup.waitFor({ state: 'visible' });
      await page.waitForFunction(() => document.querySelectorAll('#mentionPopup .mention-row').length >= 5);
      const pb = await popup.boundingBox(); const ib = await input.boundingBox();
      assert.ok(pb.y + pb.height <= ib.y + 2, `popup must sit above the input (popup bottom ${pb.y + pb.height}, input top ${ib.y})`);
      assert.ok(ib.y - (pb.y + pb.height) < 40, 'popup must be close to the input');
      assert.ok(pb.x >= 0 && pb.y >= 0 && pb.x + pb.width <= 1000, 'popup stays inside the viewport');
      assert.strictEqual(await page.locator('#mentionPopup [aria-selected="true"]').count(), 1);
      assert.match(await page.locator('#mentionPopup [aria-selected="true"]').first().textContent(), /^Calpha（alpha\.zzc）/);
      const hit = await page.evaluate(() => { const r = document.querySelector('#mentionPopup .mention-row').getBoundingClientRect(); return document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2)?.closest('.mention-row') !== null; });
      assert.strictEqual(hit, true, 'the popup must be on top, not covered');
    });

    await test('rows show 短名（长名), a CLI icon and an online dot; two agents with the same short name are told apart by the long name', async () => {
      assert.deepStrictEqual(await rowsText(), ['C' + 'alpha（alpha.zzc）', 'Xbeta（beta.zzc）', 'Ogamma（gamma.zzc）', 'Dtwin（twin.zzc）', '?twin（twin.lisa）']);
      assert.deepStrictEqual(await page.locator('#mentionPopup .mention-dot').evaluateAll((els) => els.map((e) => e.dataset.online)), ['true', 'true', 'false', 'true', 'true']);
      assert.deepStrictEqual(await page.locator('#mentionPopup .mention-ico').evaluateAll((els) => els.map((e) => e.dataset.cli)), ['claude', 'codex', 'opencode', 'deepseek', 'weirdcli']);
    });

    await test('typing letters filters by the prefix of the short name or of the long name; the highlight returns to the first row', async () => {
      await page.keyboard.type('t');
      assert.deepStrictEqual((await rowsText()).map((t) => t.slice(1)), ['twin（twin.zzc）', 'twin（twin.lisa）']);
      await page.keyboard.press('Backspace'); await page.keyboard.type('b');
      assert.deepStrictEqual((await rowsText()).map((t) => t.slice(1)), ['beta（beta.zzc）']);
      await page.keyboard.press('Backspace'); await page.keyboard.type('TWIN.L');   // 大小写不敏感,长名前缀
      assert.deepStrictEqual((await rowsText()).map((t) => t.slice(1)), ['twin（twin.lisa）']);
    });

    await test('arrow keys move the highlight; Enter selects the row INSTEAD of sending and inserts "@name "', async () => {
      await page.keyboard.press('Escape'); await input.fill(''); await typeAt('hi @');
      await page.waitForFunction(() => document.querySelectorAll('#mentionPopup .mention-row').length >= 5);
      await page.keyboard.press('ArrowDown'); await page.keyboard.press('ArrowDown');
      assert.match(await page.locator('#mentionPopup [aria-selected="true"]').first().textContent(), /gamma/);
      await page.keyboard.press('ArrowUp');
      assert.match(await page.locator('#mentionPopup [aria-selected="true"]').first().textContent(), /beta/);
      await page.keyboard.press('Enter');
      assert.strictEqual(await input.inputValue(), 'hi @beta ');
      assert.strictEqual(inputs.length, 0, 'Enter with the popup open must not send the message');
      await popup.waitFor({ state: 'hidden' });
      assert.strictEqual(await input.evaluate((el) => el.selectionStart), 'hi @beta '.length, 'the caret lands after the inserted name');
    });

    await test('ambiguous short names insert what the Hub says (the full name); Tab also selects', async () => {
      await input.fill(''); await typeAt('@tw');
      await page.waitForFunction(() => document.querySelectorAll('#mentionPopup .mention-row').length === 2);
      await page.keyboard.press('ArrowDown'); await page.keyboard.press('Tab');
      assert.strictEqual(await input.inputValue(), '@twin.lisa ');
    });

    await test('after a mention is inserted, Enter sends the message with it (normal sending is untouched)', async () => {
      await page.keyboard.type('please look'); await page.keyboard.press('Enter');
      await page.waitForFunction(() => document.getElementById('humanInput').value === '');
      assert.strictEqual(inputs.length, 1);
      assert.match(JSON.stringify(inputs[0]), /@twin\.lisa please look/);
    });

    await test('clicking a row selects it and keeps the focus in the input', async () => {
      await input.fill(''); await typeAt('@');
      await page.waitForFunction(() => document.querySelectorAll('#mentionPopup .mention-row').length >= 5);
      await page.locator('#mentionPopup .mention-row').nth(2).click();
      assert.strictEqual(await input.inputValue(), '@gamma ');
      assert.strictEqual(await page.evaluate(() => document.activeElement?.id), 'humanInput');
    });

    await test('Escape closes the popup and leaves the text alone', async () => {
      await input.fill(''); await typeAt('@al');
      await popup.waitFor({ state: 'visible' });
      await page.keyboard.press('Escape');
      await popup.waitFor({ state: 'hidden' });
      assert.strictEqual(await input.inputValue(), '@al');
    });

    await test('the trigger is "@ at the start or after whitespace": an @ inside a word (an e-mail) opens nothing; a new line works', async () => {
      await input.fill(''); await typeAt('mail me at a@b');
      await page.waitForTimeout(150);
      assert.strictEqual(await popup.isHidden(), true);
      await input.fill(''); await typeAt('first line'); await page.keyboard.press('Shift+Enter'); await page.keyboard.type('@');
      await popup.waitFor({ state: 'visible' });
      await page.keyboard.press('Escape');
    });

    await test('while an input method is composing, keys are not intercepted (no selection, no navigation)', async () => {
      await input.fill(''); await typeAt('@');
      await page.waitForFunction(() => document.querySelectorAll('#mentionPopup .mention-row').length >= 5);
      const result = await page.evaluate(() => {
        const el = document.getElementById('humanInput');
        const ev = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true, isComposing: true });
        el.dispatchEvent(ev);
        return { defaultPrevented: ev.defaultPrevented, value: el.value };
      });
      assert.strictEqual(result.value, '@', 'composition Enter must not insert a mention');
      assert.strictEqual(await popup.isVisible(), true);
      await page.keyboard.press('Escape');
    });

    await test('no selectable row (here: no match) ⇒ Enter sends as usual instead of being swallowed', async () => {
      await input.fill(''); await typeAt('@zzzz');
      await page.waitForFunction(() => /没有匹配/.test(document.getElementById('mentionPopup').textContent));
      await page.keyboard.press('Enter');
      await page.waitForFunction(() => document.getElementById('humanInput').value === '');
      assert.strictEqual(inputs.length, 2);
      assert.match(JSON.stringify(inputs[1]), /@zzzz/);
    });

    await test('an empty group is said so plainly', async () => {
      await reset([]);
      await typeAt('@');
      await page.waitForFunction(() => /本组没有其他 Agent/.test(document.getElementById('mentionPopup').textContent));
    });

    await test('a failed load says so with a retry, never blocks typing or sending; retry works', async () => {
      await reset(CANDS, 'error');
      await typeAt('@');
      await page.waitForFunction(() => /候选加载失败/.test(document.getElementById('mentionPopup').textContent));
      candidateMode = 'ok';
      await page.locator('#mentionPopup [data-retry="1"]').dispatchEvent('mousedown');
      await page.waitForFunction(() => document.querySelectorAll('#mentionPopup .mention-row:not(.mention-note)').length === 5);
      await page.keyboard.press('Escape'); await input.fill('plain text'); await page.keyboard.press('Enter');
      await page.waitForFunction(() => document.getElementById('humanInput').value === '');
      assert.strictEqual(inputs.length, 1, 'sending works throughout');
    });

    await test('an older, slower answer never overwrites a newer one', async () => {
      await reset([{ full_name: 'old.zzc', name: 'old', cli: 'claude', online: true, insert: 'old' }], 'slow-first');
      await typeAt('@');                                         // 第 1 次请求:慢(700ms),带"旧"列表
      await page.waitForFunction(() => /加载候选/.test(document.getElementById('mentionPopup').textContent));
      candidates = [{ full_name: 'new.zzc', name: 'new', cli: 'codex', online: true, insert: 'new' }];
      await page.keyboard.press('Escape'); await input.fill('');
      await typeAt('@');                                         // 重新打开 ⇒ 第 2 次请求:快,带"新"列表
      await page.waitForFunction(() => /new/.test(document.getElementById('mentionPopup').textContent));
      await page.waitForTimeout(1000);                           // 让慢的那条迟到
      assert.deepStrictEqual((await rowsText()).map((t) => t.slice(1)), ['new（new.zzc）']);
    });

    await test('a slow interface gets ONE request however fast the user types (the in-flight one is reused)', async () => {
      await reset(CANDS, 'slow-all');
      await typeAt('@alp');
      await page.waitForFunction(() => document.querySelectorAll('#mentionPopup .mention-row:not(.mention-note)').length === 1);
      assert.strictEqual(candidateGets, 1, `typing while loading must not start new requests (got ${candidateGets})`);
    });

    await test('on a short viewport the popup is capped to the room above the input and is never clipped at the top', async () => {
      await reset();
      await page.setViewportSize({ width: 1000, height: 260 });
      await typeAt('@');
      await page.waitForFunction(() => document.querySelectorAll('#mentionPopup .mention-row').length >= 5);
      const pb = await popup.boundingBox();
      await page.setViewportSize({ width: 1000, height: 700 });
      assert.ok(pb.y >= 0, `popup top must stay inside the viewport (top ${pb.y})`);
    });

    await test('the popup follows the dark theme', async () => {
      await reset();
      await typeAt('@');
      await page.waitForFunction(() => document.querySelectorAll('#mentionPopup .mention-row').length >= 5);
      const light = await popup.evaluate((el) => getComputedStyle(el).backgroundColor);
      await page.keyboard.press('Escape');
      await page.evaluate(() => { document.documentElement.dataset.theme = 'dark'; });
      await input.fill(''); await typeAt('@');
      await page.waitForFunction(() => document.querySelectorAll('#mentionPopup .mention-row').length >= 5);
      const dark = await popup.evaluate((el) => getComputedStyle(el).backgroundColor);
      assert.notStrictEqual(light, dark, 'popup background must change with the theme');
    });

    await test('no pageerror and no unexpected console error across the whole run', async () => {
      assert.deepStrictEqual(pageErrors, []);
      assert.deepStrictEqual(consoleErrors.filter((t) => !/status of (404|500)/.test(t)), []);
    });
  } finally { await context.close(); await browser.close(); await new Promise((resolve) => server.close(resolve)); }
  console.log(`mention candidates browser: passed=${pass} failed=${fail} mutation=${mutation || 'none'}`);
  if (mutation) { if (fail < 1) process.exit(1); } else if (fail) process.exit(1);
})().catch((e) => { console.error(e); process.exit(2); });
