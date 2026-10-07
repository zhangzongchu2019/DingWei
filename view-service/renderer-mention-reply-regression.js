// R-V2-0103-MENTION-REPLY-MIRROR · 被提及 Agent 那一轮的回复「只显示回来」——页面一侧的浏览器闸。
// 要点板 runbooks/PLAN-R-V2-0103-MENTION-REPLY-MIRROR-opus50tc2k.md（第二节第 4 条、第六节第 2、5 条）。
// Hub 侧（识别那一轮、一次性发、同 owner、不进 A 的投递）在 internal/m8/view_mention_reply_test.go。
//
// 断言面＝页面：mention_reply 画成独立卡片（头「来自 X 的回复」、与本页 Agent 回复区分）、正文安全渲染、按 message_id 去重、
// 失败只画状态行；旧页面（无该分支的变体）收到不报错、不显示。实现承诺＝renderer-canary-sse.html 的 renderMentionReply。
// 装置：自带 HTTP 夹具喂仓内真实 renderer-canary-sse.html；/events 为真 SSE，由夹具推事件（与生产同一入口 onmessage）。
//
// MUTATION=<name>：no-branch · no-dedupe · as-html · merge-into-turn
// 退出码：1＝有用例失败（变异被杀）· 0＝全绿（变异模式下＝存活）· 3＝变异锚点失效 · 2＝脚本崩溃。
const assert = require('assert');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');

const RENDERER = path.join(__dirname, 'renderer-canary-sse.html');
const pristine = fs.readFileSync(RENDERER, 'utf8');
let html = pristine;
const mutation = process.env.MUTATION || '';
if (process.argv.some((a) => a.startsWith('--mutate'))) { console.error('MUTATION-SWITCH-WRONG-FORM: this script reads env MUTATION=<name>'); process.exit(3); }
function mutate(from, to) {
  const hits = html.split(from).length - 1;
  if (hits !== 1) { console.error(`MUTATION-ANCHOR-STALE: ${mutation} hits=${hits}: ${from.slice(0, 120)}`); process.exit(3); }
  html = html.replace(from, to);
}
const BRANCH = "      if (ev.kind === 'mention_reply') {\n        if (renderMentionReply(ev)) rendered += 1;\n        if (shouldFollow) scheduleTimelineToBottom();\n        return;\n      }\n";
const MUTATIONS = ['mutation-anchor-selftest', 'no-branch', 'no-dedupe', 'as-html', 'merge-into-turn'];
if (mutation && !MUTATIONS.includes(mutation)) { console.error(`MUTATION-UNKNOWN: ${mutation}`); process.exit(3); }
if (mutation === 'mutation-anchor-selftest') mutate('THIS-TEXT-IS-NOT-IN-THE-PAGE', 'x');
if (mutation === 'no-branch') mutate(BRANCH, '');
if (mutation === 'no-dedupe') mutate("if (id && mentionReplySeen.has(id)) return false;", '');
if (mutation === 'as-html') mutate('else content.innerHTML = renderMarkdown(text);', 'else content.innerHTML = text;');
if (mutation === 'merge-into-turn') mutate(BRANCH, "      if (ev.kind === 'mention_reply') { ev.kind = 'assistant_text'; ev.turn_id = 'merged'; }\n");
// 「旧页面」变体：本版之前没有 mention_reply 分支的页面（去掉分支即可还原那种行为）。
const oldPage = pristine.split(BRANCH).join('');
if (oldPage === pristine) { console.error('MUTATION-ANCHOR-STALE: old-page variant could not remove the branch'); process.exit(3); }

const NAME = 'dev.zzc';
const streams = new Set();
const server = http.createServer((req, res) => {
  const u = req.url.split('?')[0];
  const json = (o, status = 200) => { res.writeHead(status, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' }); res.end(JSON.stringify(o)); };
  if (u === `/view/${NAME}`) { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' }); return res.end(html); }
  if (u === `/old/view/${NAME}`) { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' }); return res.end(oldPage); }
  if (u.endsWith('/mention-candidates')) return json({ candidates: [] });
  if (u.endsWith('/status')) return json({ session: NAME, state: 'idle', readonly: false, can_write: true, auth_mode: 'legacy', owner: 'zzc', model: 'm', cost_total: 0, cost_last: 0, turns: 0 });
  if (u.endsWith('/events')) {
    res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache', Connection: 'keep-alive' }); res.flushHeaders(); res.write(': open\n\n');
    streams.add(res); const t = setInterval(() => { try { res.write(': keepalive\n\n'); } catch { /* closed */ } }, 4000);
    res.on('close', () => { clearInterval(t); streams.delete(res); }); return;
  }
  if (u.endsWith('/tasks')) return json({});
  if (u.endsWith('/alerts')) return json([]);
  if (u === '/favicon.ico') { res.writeHead(204); return res.end(); }
  json({}, 404);
});
const push = (ev) => { for (const s of streams) s.write(`data: ${JSON.stringify(ev)}\n\n`); };
const reply = (id, text, extra = {}) => ({ schema_version: 'delivery.v2', kind: 'mention_reply', message_id: id, text,
  data: { from_full_name: 'Beta.zzc', from_label: 'Beta', status: 'ok', source_id: 'dw-dlv_x', ...extra }, updated_at: new Date().toISOString() });

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
  const open = async (url) => {
    await page.goto(url); await page.locator('#humanInput').waitFor({ state: 'visible' });
    await page.waitForFunction(() => typeof EventSource !== 'undefined');
    for (let i = 0; i < 100 && streams.size === 0; i++) await page.waitForTimeout(20);
    assert.ok(streams.size > 0, 'page opened the event stream');
  };
  const cards = () => page.locator('#timeline .mention-reply').evaluateAll((els) => els.map((e) => ({
    head: e.querySelector('.mention-reply-head')?.textContent, text: e.querySelector('.event-content')?.textContent,
    status: e.dataset.status, id: e.dataset.messageId, topLevel: e.parentElement?.id === 'timeline', assistant: e.classList.contains('assistant'),
    strong: !!e.querySelector('.event-content strong'), img: !!e.querySelector('.event-content img') })));
  try {
    await open(`${base}/view/${NAME}`);

    await test('a mention reply arriving on the event stream becomes its own card headed 「来自 Beta 的回复」, rendered as markdown', async () => {
      push(reply('mreply-1', '你好，**粗体** 一句'));
      await page.waitForFunction(() => document.querySelectorAll('#timeline .mention-reply').length === 1);
      const [c] = await cards();
      assert.strictEqual(c.head, '来自 Beta 的回复');
      assert.match(c.text, /你好，粗体 一句/);
      assert.strictEqual(c.strong, true, 'markdown rendered');
      assert.strictEqual(c.status, 'ok');
      assert.strictEqual(c.topLevel, true, 'a standalone card in the timeline');
      assert.strictEqual(c.assistant, false, 'not styled as this page\'s own agent');
    });

    await test('the card is not merged into a turn of this page\'s own agent', async () => {
      assert.strictEqual(await page.locator('#timeline .message-card.assistant').count(), 0, 'no assistant card was created for it');
      const ownTurn = await page.locator('#timeline .mention-reply').evaluate((e) => !!e.closest('.turn-group'));
      assert.strictEqual(ownTurn, false);
    });

    await test('the same message_id twice is drawn once', async () => {
      push(reply('mreply-1', '你好，**粗体** 一句'));
      push(reply('mreply-2', '第二条'));
      await page.waitForFunction(() => document.querySelectorAll('#timeline .mention-reply').length >= 2);
      await page.waitForTimeout(150);
      assert.deepStrictEqual((await cards()).map((c) => c.id), ['mreply-1', 'mreply-2']);
    });

    await test('HTML in the reply is shown as text, never executed', async () => {
      push(reply('mreply-3', '<img src=x onerror="window.__pwned=1">尾巴'));
      await page.waitForFunction(() => document.querySelectorAll('#timeline .mention-reply').length >= 3);
      await page.waitForTimeout(150);
      const c = (await cards())[2];
      assert.strictEqual(c.img, false, 'no <img> element created');
      assert.match(c.text, /尾巴/);
      assert.strictEqual(await page.evaluate(() => window.__pwned), undefined);
    });

    await test('a failed turn is one status line with the failed style', async () => {
      push(reply('mreply-4', 'Beta 这一轮未成功：未知原因', { status: 'failed' }));
      await page.waitForFunction(() => document.querySelectorAll('#timeline .mention-reply').length >= 4);
      const c = (await cards())[3];
      assert.strictEqual(c.status, 'failed');
      assert.strictEqual(c.text, 'Beta 这一轮未成功：未知原因');
      assert.strictEqual(await page.locator('#timeline .mention-reply-failed').count(), 1);
    });

    await test('an old page without the mention_reply branch ignores the event without errors', async () => {
      const errsBefore = pageErrors.length;
      streams.clear();
      await open(`${base}/old/view/${NAME}`);
      push(reply('mreply-old', '旧页面不该显示'));
      await page.waitForTimeout(400);
      assert.strictEqual(await page.locator('#timeline').evaluate((t) => t.textContent.includes('旧页面不该显示')), false, 'not displayed');
      assert.deepStrictEqual(pageErrors.slice(errsBefore), [], 'no page error');
    });

    await test('no pageerror and no unexpected console error across the whole run', async () => {
      assert.deepStrictEqual(pageErrors, []);
      assert.deepStrictEqual(consoleErrors.filter((t) => !/status of (404|500)/.test(t)), []);
    });
  } finally { await context.close(); await browser.close(); await new Promise((resolve) => server.close(resolve)); }
  console.log(`mention reply browser: passed=${pass} failed=${fail} mutation=${mutation || 'none'}`);
  if (mutation) console.log(`MUTATION-VERDICT: ${mutation} ${fail >= 1 ? 'KILLED' : 'SURVIVED'}`);
  if (fail) process.exit(1);
})().catch((e) => { console.error(e); process.exit(2); });
