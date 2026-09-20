// 🔴 重连幂等的**端到端守闸**(板件 0.2.1.35-⑬ 裁定 ③:把它接进跑者)
//
// 钉的是这条不变量:**SSE 断开 → 页面自己重连 → hub 整批重放 ring** 之后,
// 同一回合的正文**不得增长**(既不翻倍,也不是被擦成 0),且**已打开的卡片数不变**。
//
// 为什么必须有这一件(0.2.1.35-⑬ 的教训):
//   合成用例(page.evaluate 直接调 handleIncomingEvent)**测不到这条路径** ——
//   它绕过了 `eventSource.onmessage` 这个"清空"唯一的消费点,于是
//   「幂等机制在自建重连这条路上是死代码」这种缺陷能在全绿套件里活很久。
//   本件用**真 EventSource + 真断链 + 真整批重放**,不给任何测试缝。
//
// 装置:`renderer-sse-fixture.py`(RECONNECT=1:发完即 shutdown+close 真断链)。
//   它按 `internal/m8/terminal.go:645` HandleInternalViewEvents「先回放 eventRing,
//   再挂实时流」的语义模拟 hub;全仓无 `Last-Event-ID` ⇒ 客户端无法逐条判重,
//   "清空 + 由重放重建"是**唯一**的幂等机制。
//
// 🔴 可被 tools/run-browser-regressions.py 直接加进 SUITES:
//   · 自启自收夹具、自挑空闲端口、路径一律走 __dirname(cwd=仓根也能跑)
//   · **调用方可控的 env 面只有已登记的两个**:`PW_CHANNEL` / `RENDERER_OVERRIDE`
//     (跑者会机械扫描 process.env.* 并要求登记 ⇒ 不开新面);其余参数一律显式传给子进程夹具
//   · rc 与发布约定一致:**0 全过 / 2 断言失败 / 3 环境不可用**
//
// 阳性对照(证明本件有牙,改后也要能红):
//   RENDERER_OVERRIDE=<改动前那版字节> node renderer-sse-reconnect.js   ⇒ 必须 rc=2
//   取法:git show 92f146de:view-service/renderer-canary-sse.html > /tmp/pre-fix.html
//        (92f146de = 本修复之前最后一个提交)
const assert = require('assert');
const fs = require('fs');
const net = require('net');
const os = require('os');
const path = require('path');
const { spawn } = require('child_process');
const { chromium } = require('playwright');

const HERE = __dirname;
const FIXTURE = path.join(HERE, 'renderer-sse-fixture.py');
const RENDERER = process.env.RENDERER_OVERRIDE || path.join(HERE, 'renderer-canary-sse.html');
const CONTROL = !!process.env.RENDERER_OVERRIDE;
const SAMPLE_MS = 1000;

// 两条腿:长正文(>4000 折叠阈值,业主从"打开全文/复制/下载"那一面看到)
//         短正文(<4000,业主**肉眼**在页面上看到)—— 缺陷形态不同,必须各钉一条
const LONG_UNIT = '契约正文压测段落';                 // 8 字
const LONG_TEXT = LONG_UNIT.repeat(960);               // 7680 字 ⇒ 折叠
const SHORT_TEXT = Array.from({ length: 50 }, (_, i) => `[${String(i).padStart(3, '0')}]契约正文压测`).join(''); // 550 字 ⇒ 不折叠
const CHUNK = 160;
const LEGS = [
  { name: 'L1 长正文(折叠)', text: LONG_TEXT, ring: 20, secs: 20,
    expectChars: LONG_TEXT.length, expectChunks: Math.ceil(LONG_TEXT.length / CHUNK), folded: true },
  { name: 'L2 短正文(肉眼)', text: SHORT_TEXT, ring: 2, secs: 16,
    expectChars: SHORT_TEXT.length, expectChunks: Math.ceil(SHORT_TEXT.length / CHUNK), folded: false },
];

let pass = 0;
let fail = 0;
const failures = [];
async function t(name, fn) {
  try { await fn(); console.log('  ✅', name); pass += 1; }
  catch (e) { console.log('  🔴', name, '->', e && e.message); fail += 1; failures.push(name); }
}
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

function freePort() {
  return new Promise((resolve, reject) => {
    const s = net.createServer();
    s.once('error', reject);
    s.listen(0, '127.0.0.1', () => { const p = s.address().port; s.close(() => resolve(p)); });
  });
}
async function waitForPort(port, child, timeoutMs = 15000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (child.exitCode !== null) throw Object.assign(new Error(`夹具提前退出 rc=${child.exitCode}`), { env: true });
    try {
      await new Promise((resolve, reject) => {
        const s = net.createConnection({ port, host: '127.0.0.1' });
        s.once('connect', () => { s.destroy(); resolve(); });
        s.once('error', reject);
      });
      return;
    } catch { await sleep(150); }
  }
  throw Object.assign(new Error(`夹具 ${timeoutMs}ms 内没起来`), { env: true });
}

// 起一份夹具(每条腿一份配置),返回 { base, stop }
async function startFixture({ text, ring }) {
  if (!fs.existsSync(FIXTURE)) throw Object.assign(new Error(`缺夹具 ${FIXTURE}`), { env: true });
  const textFile = path.join(os.tmpdir(), `dw-sse-reconnect-${process.pid}-${ring}.txt`);
  fs.writeFileSync(textFile, text, 'utf8');
  const port = await freePort();
  const child = spawn('python3', [FIXTURE, String(port)], {
    cwd: HERE,
    env: { ...process.env, RENDERER, TEXT_FILE: textFile, RECONNECT: '1',
      RING_TURNS: String(ring), DELAY: '0.01', CHUNK: String(CHUNK), REPEAT: '1', TOOLS: '0' },
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  child.stdout.resume(); child.stderr.resume();
  let stopped = false;
  const stop = async () => {
    if (stopped) return; stopped = true;
    try { child.kill('SIGTERM'); } catch {}
    await sleep(200);
    try { if (child.exitCode === null) child.kill('SIGKILL'); } catch {}
    try { fs.unlinkSync(textFile); } catch {}
  };
  try { await waitForPort(port, child); } catch (e) { await stop(); throw e; }
  return { base: `http://127.0.0.1:${port}`, stop };
}

// 计数器装在页面脚本之前:EventSource 构造数用来证明"重连真的发生了"(守卫),
// markStreamReconnect/resetConversation 的调用数用来分辨"增长停了"是因为机制生效还是因为流根本没断。
const INIT = () => {
  window.__es = { ctor: 0, opens: 0, errors: 0, maxOpensOneObj: 0 };
  const Orig = window.EventSource;
  function Patched(url, opts) {
    window.__es.ctor += 1;
    const es = new Orig(url, opts);
    let mine = 0;
    es.addEventListener('open', () => { mine += 1; window.__es.opens += 1;
      if (mine > window.__es.maxOpensOneObj) window.__es.maxOpensOneObj = mine; });
    es.addEventListener('error', () => { window.__es.errors += 1; });
    return es;
  }
  Patched.prototype = Orig.prototype;
  Patched.CONNECTING = Orig.CONNECTING; Patched.OPEN = Orig.OPEN; Patched.CLOSED = Orig.CLOSED;
  window.EventSource = Patched;
  window.__c = { mark: 0, reset: 0 };
};

async function measureLeg(browser, leg) {
  const fx = await startFixture(leg);
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
  const samples = [];
  try {
    const page = await context.newPage();
    const pageErrors = [];
    page.on('pageerror', (e) => pageErrors.push(String(e.message).slice(0, 120)));
    await page.addInitScript(INIT);
    await page.goto(`${fx.base}/renderer`, { waitUntil: 'domcontentloaded' });
    await page.evaluate(() => {
      const om = window.markStreamReconnect, or = window.resetConversation;
      if (typeof om === 'function') window.markStreamReconnect = function () { window.__c.mark += 1; return om.apply(this, arguments); };
      if (typeof or === 'function') window.resetConversation = function () { window.__c.reset += 1; return or.apply(this, arguments); };
    });
    const probe = () => page.evaluate(() => {
      const grp = turnGroups.get('t-1');
      const events = grp ? grp.events.filter((e) => e.kind === 'assistant_text') : [];
      const g = document.querySelector('#timeline > [data-turn-key="t-1"]');
      const el = g && g.querySelector('.turn-assistant .event-content');
      let dom = 0;
      if (el) { const c = el.cloneNode(true); c.querySelectorAll('.event-meta').forEach((n) => n.remove()); dom = c.innerText.length; }
      const note = document.querySelector('#timeline > [data-turn-key="t-1"] .longtext-note');
      const m = note ? note.textContent.replace(/,/g, '').match(/全文\s*(\d+)\s*字/) : null;
      return {
        full: events.reduce((n, e) => n + (e.text || '').length, 0),
        assistants: events.length,
        dom,
        noteChars: m ? Number(m[1]) : null,
        folded: !!document.querySelector('#timeline > [data-turn-key="t-1"] .longtext-bar'),
        cards: document.getElementById('timeline').children.length,
        mark: window.__c.mark, reset: window.__c.reset,
        es: { ...window.__es },
      };
    }).catch((e) => ({ probeError: String(e).slice(0, 100) }));

    const rounds = Math.max(6, Math.floor((leg.secs * 1000) / SAMPLE_MS));
    for (let i = 0; i < rounds; i += 1) {
      await sleep(SAMPLE_MS);
      samples.push(await probe());
    }
    return { samples, pageErrors };
  } finally {
    await context.close();
    await fx.stop();
  }
}

(async () => {
  console.log('重连幂等(真 EventSource + 真断链 + hub 整批重放 ring)');
  console.log(`  · renderer ${RENDERER}`);
  console.log(`    sha256=${require('crypto').createHash('sha256').update(fs.readFileSync(RENDERER)).digest('hex').slice(0, 16)}…`
    + (CONTROL ? '  🔴 RENDERER_OVERRIDE 生效:这是【阳性对照】跑,期望 rc=2' : ''));
  if (!fs.existsSync(RENDERER)) { console.log('  ⚠️  环境缺失:被测 renderer 不存在'); process.exit(3); }

  const launchOptions = { headless: true };
  if (process.env.PW_CHANNEL) launchOptions.channel = process.env.PW_CHANNEL;
  let browser;
  try { browser = await chromium.launch(launchOptions); }
  catch (e) { console.log('  ⚠️  环境缺失:Playwright 起不来 ->', String(e.message).slice(0, 160)); process.exit(3); }

  try {
    for (const leg of LEGS) {
      let res;
      try { res = await measureLeg(browser, leg); }
      catch (e) {
        if (e && e.env) { console.log(`  ⚠️  环境缺失(${leg.name}):${e.message}`); await browser.close(); process.exit(3); }
        throw e;
      }
      const { samples, pageErrors } = res;
      const valid = samples.filter((s) => !s.probeError);
      const last = valid[valid.length - 1] || {};
      const maxOf = (k) => valid.reduce((m, s) => Math.max(m, s[k] || 0), 0);
      const scene = `样本数=${valid.length}/${samples.length} 末次=${JSON.stringify(last)} 观测最大值={full:${maxOf('full')},dom:${maxOf('dom')},assistants:${maxOf('assistants')},cards:${maxOf('cards')}}`;

      // 🔴 守卫:重连必须**真的发生过**,否则"没增长"毫无判别力(夹具没断链也能全绿)。
      //    这一条在两态(修前/修后)都必须成立,所以它能当守卫而不是断言。
      await t(`${leg.name} · 守卫:真断链重连确实发生了(≥3 次)`, async () => {
        assert.ok(valid.length >= 6, `样本太少(${valid.length}),量不出增长趋势`);
        assert.ok((last.es && last.es.ctor >= 3), `EventSource 只构造了 ${last.es && last.es.ctor} 次 ⇒ 重连没发生,本格是空转。${scene}`);
        assert.ok((last.es && last.es.errors >= 2), `只观测到 ${last.es && last.es.errors} 次 error ⇒ 断链没真发生。${scene}`);
        if (pageErrors.length) assert.strictEqual(pageErrors.length, 0, `页面抛错:${pageErrors.slice(0, 2).join(' | ')}`);
      });

      // 正文**可取回全文**(group.events):不得增长,也不得被擦成 0
      await t(`${leg.name} · 🔴 重连重放后正文全文长度恒为单份(${leg.expectChars} 字),既不翻倍也不归零`, async () => {
        assert.strictEqual(maxOf('full'), leg.expectChars,
          `全文长度出现过 ${maxOf('full')} 字(应为 ${leg.expectChars})⇒ 每次重连把整份 ring 又追加了一遍。${scene}`);
        assert.strictEqual(last.full, leg.expectChars, `末次全文 ${last.full} 字,应为 ${leg.expectChars}(归零=被擦了,也不是修好)。${scene}`);
      });

      // 事件条数:同一条正文不得被重放成 N 份 chunk
      await t(`${leg.name} · 🔴 assistant 事件数恒为单份(${leg.expectChunks} 条)`, async () => {
        assert.strictEqual(maxOf('assistants'), leg.expectChunks,
          `assistant 事件数涨到 ${maxOf('assistants')}(应为 ${leg.expectChunks})。${scene}`);
        assert.strictEqual(last.assistants, leg.expectChunks, `末次 ${last.assistants} 条,应为 ${leg.expectChunks}。${scene}`);
      });

      // 卡片数:修法是"清空+重建",不许顺手把别的回合也擦掉
      await t(`${leg.name} · 🔴 时间线卡片数恒为 ring+1(${leg.ring + 1}),重连不得增删卡片`, async () => {
        assert.strictEqual(maxOf('cards'), leg.ring + 1, `卡片数涨到 ${maxOf('cards')}(应为 ${leg.ring + 1})。${scene}`);
        assert.strictEqual(last.cards, leg.ring + 1, `末次卡片 ${last.cards},应为 ${leg.ring + 1}。${scene}`);
      });

      if (leg.folded) {
        // 折叠条文案里的字数 = 业主点"打开全文/复制全文/下载 .md"拿到的那份的字数
        await t(`${leg.name} · 🔴 折叠条自报字数恒为单份(业主从这一面看到翻倍)`, async () => {
          const notes = valid.map((s) => s.noteChars).filter((n) => n !== null);
          assert.ok(notes.length > 0, `没读到折叠条文案(它应当存在,正文 ${leg.expectChars} 字 > 4000 阈值)。${scene}`);
          const bad = notes.filter((n) => n !== leg.expectChars);
          assert.strictEqual(bad.length, 0, `折叠条自报字数出现过 ${[...new Set(bad)].join(',')}(应恒为 ${leg.expectChars})。${scene}`);
        });
      } else {
        // 短正文:业主**肉眼**在页面上看到的那一面
        await t(`${leg.name} · 🔴 DOM 可见字数恒为单份(${leg.expectChars} 字,业主肉眼那一面)`, async () => {
          assert.strictEqual(maxOf('dom'), leg.expectChars,
            `页面上可见字数涨到 ${maxOf('dom')}(应为 ${leg.expectChars})⇒ 业主会看见正文自己越来越长。${scene}`);
          assert.strictEqual(last.dom, leg.expectChars, `末次可见 ${last.dom} 字,应为 ${leg.expectChars}。${scene}`);
          assert.strictEqual(last.folded, false, `短正文不该被折叠(阈值 4000),这条腿的形态前提坏了。${scene}`);
        });
      }

      // 🔴 机制钉(不是不变量):增长停下来必须是**因为幂等机制被 arm 了**,
      //    不是因为流根本没重连、也不是因为整页被擦掉。
      //    🔵 若将来改成 per-event 逐条判重(板件登记 0.2.1.35-⑰),**这条要随之改写** ——
      //       它钉的是当前修法("清空 + 由重放重建"),不是永恒不变量。
      await t(`${leg.name} · 🔴 幂等机制确实被 arm(markStreamReconnect ≥1 且 resetConversation ≥1)`, async () => {
        assert.ok(last.mark >= 1, `markStreamReconnect 一次都没被调用(${last.mark})⇒ 幂等机制在这条路径上是死代码。${scene}`);
        assert.ok(last.reset >= 1, `resetConversation 一次都没跑(${last.reset})⇒ 清空从未发生。${scene}`);
      });
    }
  } finally {
    await browser.close();
  }

  console.log(`\n  合计 ${pass + fail} 条:✅ ${pass} · 🔴 ${fail}`);
  if (CONTROL && fail === 0) {
    console.log('  🔴 阳性对照跑居然全绿 ⇒ 本件**没有判别力**,不成立');
    process.exit(2);
  }
  if (fail) { console.log(`  红的条:${failures.join(' / ')}`); process.exit(2); }
  process.exit(0);
})().catch((e) => {
  console.log('  🔴 装置异常:', e && e.stack ? e.stack.split('\n').slice(0, 4).join('\n') : e);
  process.exit(2);
});
