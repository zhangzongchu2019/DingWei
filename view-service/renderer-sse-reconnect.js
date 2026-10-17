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

// 四条腿:长正文(>4000 折叠阈值,业主从"打开全文/复制/下载"那一面看到)
//         短正文(<4000,业主**肉眼**在页面上看到)—— 缺陷形态不同,必须各钉一条
//         🔴 慢重放·窗内 / 慢重放·出窗(0.2.1.35-⑲,组长 ts-No.12 ③ 判为本版必做)
//
// ── 为什么必须有慢重放那两条腿 ──────────────────────────────────────────────
// 修法依赖一条**没被任何闸验过的前提**:「首条重放事件必须在 open 后
// `REPLAY_HARD_LIMIT_MS`(=1000ms)内到达」。原夹具是"连上即刻重放" ⇒ 前提恒成立
// ⇒ 它零判别力。真链路里 onopen 早于首字节:`view_service.py:605` 先把 200 头发给
// 浏览器(此刻窗口开始计时),**之后**才去连 hub(:611)、等 `h.mu` 全局锁
// (`internal/m8/terminal.go:627`)、整份拷 ring(上限 1000 条,:633/:34)、再逐行透传。
// 🔴 实测(本仓夹具 FIRST_DELAY 扫描):首条重放 ≤950ms ⇒ 不增长;≥1100ms ⇒ 每次重连
//    多一整份,`consumeReconnectClear` 一次都没返回 true,且**页面上无任何告警**。
// ⇒ 夹具加 `FIRST_DELAY`(首条事件之前的静默秒数;与 `DELAY` 是两件事,DELAY 只 sleep 在
//    chunk 之间,推不动"首条"),窗内 0.6s 与出窗 1.3s 各钉一条。
//
// ── (c) 落地后 L4 的反转(组长 ts-No.17 裁定:`0.2.1.35`-⑲ 只做 (c) 告警,不放宽窗口)──
//    产品侧:窗口过期 ⇒ 复用现成 stream alert 出一条**可见**告警,告诉业主怎么恢复(刷新)。
//    闸侧:🔴 **L4 由"已知缺陷显影"反转成硬断言** —— 出窗 ⇒ 必须出现告警;`knownDefect` 标记已删。
//    并且**窗内三条腿各加一条反向断言**:健康路径上不得出现这个告警 —— 否则误报会淹掉真告警,
//    而且没有这条,(c) 可以写成"每次 arm 都告警"而 L4 照样绿 ⇒ 那又是半条闸。
//    🔴 判别力来自**时间区间**而不是"告警出现过":`showStreamAlert` 还有一个**早就存在**的来源
//       —— 断线宽限期 `STREAM_CONNECT_GRACE_MS = 7500ms`。只数次数会让它冒充重放超时告警。
//    🔵 L4 **不再**断言"出窗不得增长":重复要等 ⑰(hub 侧 per-event 序号)才治得了,
//       (c) 只把静默变成可见。增长数字仍然打印(测量,不是断言)。
const LONG_UNIT = '契约正文压测段落';                 // 8 字
const LONG_TEXT = LONG_UNIT.repeat(960);               // 7680 字 ⇒ 折叠
const SHORT_TEXT = Array.from({ length: 50 }, (_, i) => `[${String(i).padStart(3, '0')}]契约正文压测`).join(''); // 550 字 ⇒ 不折叠
const CHUNK = 160;
// 慢重放两条腿的 firstDelay 是**按 1000ms 这个窗口值选的**。守卫会实测页面里的真值:
// 🔴 常量一旦被改,这两条腿立刻自己失败,而不是继续量一个已经过时的边界(守卫 C 的写法)。
const WINDOW_MS_ASSUMED = 1000;
const LEGS = [
  { name: 'L1 长正文(折叠)', text: LONG_TEXT, ring: 20, secs: 20,
    expectChars: LONG_TEXT.length, expectChunks: Math.ceil(LONG_TEXT.length / CHUNK), folded: true },
  { name: 'L2 短正文(肉眼)', text: SHORT_TEXT, ring: 2, secs: 16,
    expectChars: SHORT_TEXT.length, expectChunks: Math.ceil(SHORT_TEXT.length / CHUNK), folded: false },
  { name: 'L3 慢重放·窗内(首条 0.6s 后到)', text: SHORT_TEXT, ring: 2, secs: 14, firstDelay: 0.6,
    expectChars: SHORT_TEXT.length, expectChunks: Math.ceil(SHORT_TEXT.length / CHUNK), folded: false },
  // 🔵 L4 的身世(留着,因为它就是"钉要有有效期"的样例):
  //    它**曾经**是"已知缺陷显影腿"(knownDefect,不计 rc),那时缺陷还活着、修法待裁,
  //    做成硬红会让发布闸长期红着。文件里当时写明:**修法落地后本条要反转成硬断言**。
  //    组长 ts-No.17 裁定 (c)(窗口过期 ⇒ 可见告警)本版做 ⇒ **有效期到了,现已反转**:
  //    🔴 出窗 ⇒ **必须出现告警**,硬断言、计入 rc。knownDefect 标记已删。
  //    🔵 注意它**不再**断言"出窗不得增长" —— 重复本身要等 ⑰(hub 侧 per-event 序号)才治得了,
  //       (c) 只把静默变成可见。增长数字仍然**打印**出来(测量,不是断言)。
  { name: 'L4 慢重放·出窗(首条 1.3s 后到)', text: SHORT_TEXT, ring: 2, secs: 14, firstDelay: 1.3,
    expectChars: SHORT_TEXT.length, expectChunks: Math.ceil(SHORT_TEXT.length / CHUNK), folded: false,
    outOfWindow: true },
];

// 🔴 告警的**时间判据**(这条是本腿有没有牙的关键):
//    `showStreamAlert` 有两个来源 —— 重放窗口过期(本次要钉的),以及断线宽限期
//    `STREAM_CONNECT_GRACE_MS = 7500ms`(**早就存在**的那条)。只断言"告警出现过"
//    ⇒ 断线那条会**冒充**重放超时,本腿零判别力。
//    故判据是:告警必须出现在**某次 arm 之后 [WINDOW-150, WINDOW+900]ms** 内。
//    🔵 上界 900ms 远小于 7500ms ⇒ 断线告警落不进这个区间(它至少在 error 之后 7.5s,
//       而 error 又发生在 arm 之后 ~1.3s)。**区间是算出来的,不是拍出来的。**
const ALERT_EARLY_MS = 150;
const ALERT_LATE_MS = 900;

// 在 arms / alerts 两组时间戳里挑出"落在窗口过期那一刻附近"的告警
function alertHitsNearExpiry(arms, alerts, limit) {
  const hits = [];
  for (const a of (alerts || [])) {
    for (const t of (arms || [])) {
      const d = a.t - t;
      if (d >= limit - ALERT_EARLY_MS && d <= limit + ALERT_LATE_MS) hits.push({ arm: t, alert: a.t, delta: d, arg: a.arg });
    }
  }
  return hits;
}

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
// 🔴 firstDelay 只能**显式**传给子进程:跑者会机械扫描本件的 process.env.* 并要求登记,
//    所以这里不开新的 env 面(只有已登记的 PW_CHANNEL / RENDERER_OVERRIDE)。
async function startFixture({ text, ring, firstDelay }) {
  if (!fs.existsSync(FIXTURE)) throw Object.assign(new Error(`缺夹具 ${FIXTURE}`), { env: true });
  const textFile = path.join(os.tmpdir(),
    `dw-sse-reconnect-${process.pid}-${ring}-${String(firstDelay || 0).replace('.', '_')}.txt`);
  fs.writeFileSync(textFile, text, 'utf8');
  const port = await freePort();
  const child = spawn('python3', [FIXTURE, String(port)], {
    cwd: HERE,
    env: { ...process.env, RENDERER, TEXT_FILE: textFile, RECONNECT: '1',
      RING_TURNS: String(ring), DELAY: '0.01', CHUNK: String(CHUNK), REPEAT: '1', TOOLS: '0',
      FIRST_DELAY: String(firstDelay || 0) },
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
// 🔴 gaps:逐个连接实测「onopen → 首条 message」的间隔(ms)。慢重放两条腿靠它自证
//    "首条真的被推到窗内/出窗了",否则夹具没生效时它们会**静默退化成 L2** 还照样绿。
const INIT = () => {
  window.__es = { ctor: 0, opens: 0, errors: 0, maxOpensOneObj: 0, gaps: [] };
  const Orig = window.EventSource;
  function Patched(url, opts) {
    window.__es.ctor += 1;
    const es = new Orig(url, opts);
    let mine = 0;
    let openAt = 0;
    let firstMsg = true;
    es.addEventListener('open', () => { mine += 1; window.__es.opens += 1;
      if (!openAt) openAt = performance.now();
      if (mine > window.__es.maxOpensOneObj) window.__es.maxOpensOneObj = mine; });
    es.addEventListener('message', ev => {
      if (firstMsg) {
        firstMsg = false;
        if (openAt) window.__es.gaps.push(Math.round(performance.now() - openAt));
      }
      // Finish sampling at a real fixture replay boundary, never at an arbitrary
      // partial chunk. No expected body length is used to select that boundary.
      try {
        if (window.__finishReplay && JSON.parse(ev.data).data?.change === '__done__') {
          queueMicrotask(() => { es.close(); window.__replayFinished = true; });
        }
      } catch (_) {}
    });
    es.addEventListener('error', () => { window.__es.errors += 1; });
    return es;
  }
  Patched.prototype = Orig.prototype;
  Patched.CONNECTING = Orig.CONNECTING; Patched.OPEN = Orig.OPEN; Patched.CLOSED = Orig.CLOSED;
  window.EventSource = Patched;
  // consume/consumeTrue:包住 consumeReconnectClear() —— 它是"延迟清空有没有真被消费"的
  // 唯一直接证据。mark/reset 只能说明"有调用",说明不了"窗口内还是窗口外"。
  // arms / alerts:两组**时间戳**,用来判"告警是不是在窗口过期那一刻出现的"。
  // 🔴 只数次数不够 —— 断线告警(7500ms 宽限期)也会出现,数次数会把两者混成一个。
  window.__c = { mark: 0, reset: 0, consume: 0, consumeTrue: 0, arms: [], alerts: [] };
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
      // 🔴 变体基线(判"弹的是哪一件事",**不判措辞**):此刻页面脚本已跑完、任何告警都还没出现,
      //    标记里那份"连接不上"的文案与按钮标签就是 conn 变体的基线。后面的判据是
      //    "出窗时显示的**不是**这份基线" ⇒ 业主改措辞不会让闸红,而"弹错变体 / 弹空壳"一定会红。
      //    🔵 这一层是 M-C3 逼出来的:把 replay 文案改成空串后,产品里的 `|| conn` 兜底会把它
      //       悄悄换成"连接不上"那条,而整块 textContent 仍有 55 字(含按钮标签)⇒ 端到端 26/0 全绿,
      //       业主看到的却是**指错方向**的告警 —— 比没有告警更坏。
      const bt = document.getElementById('streamAlertText');
      const bb = document.getElementById('streamRetryBtn');
      window.__c.connBaseline = {
        msgPresent: !!bt,
        msg: bt ? bt.textContent.trim() : null,
        btn: bb ? bb.textContent.trim() : null,
      };
      const om = window.markStreamReconnect, or = window.resetConversation, oc = window.consumeReconnectClear;
      const oa = window.showStreamAlert;
      if (typeof om === 'function') window.markStreamReconnect = function () {
        window.__c.mark += 1;
        window.__c.arms.push(Math.round(performance.now()));
        return om.apply(this, arguments);
      };
      if (typeof or === 'function') window.resetConversation = function () { window.__c.reset += 1; return or.apply(this, arguments); };
      // 🔴 必须**透传返回值**:调用点是 `if (consumeReconnectClear()) resetConversation();`,
      //    吞掉返回值就等于把清空调没了(那会造出一个假的"修复生效")。
      if (typeof oc === 'function') window.consumeReconnectClear = function () {
        const r = oc.apply(this, arguments);
        window.__c.consume += 1;
        if (r === true) window.__c.consumeTrue += 1;
        return r;
      };
      // 🔴 记**调用时刻**与实参,但**不断言实参内容**:告警文案与外观是产品可见的,
      //    业主可能改(组长 ts-No.17 ②)⇒ 钉住措辞就是替产品立契约。
      if (typeof oa === 'function') window.showStreamAlert = function () {
        window.__c.alerts.push({ t: Math.round(performance.now()), arg: arguments.length ? String(arguments[0]) : null });
        return oa.apply(this, arguments);
      };
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
        consume: window.__c.consume, consumeTrue: window.__c.consumeTrue,
        arms: window.__c.arms.slice(), alerts: window.__c.alerts.slice(),
        // 告警的**可见性**用三件事一起判:hidden 标志 · 非空文本 · 非零盒子。
        // 🔴 只看 hidden===false 不够:一个 display:none 或 0x0 的空壳也能让 hidden 为 false,
        //    那就是"看起来告警了、业主什么也看不见" —— 与本组治的病同型。
        alertBox: (function () {
          const el = document.getElementById('streamAlert');
          if (!el) return { present: false };
          const r = el.getBoundingClientRect();
          // 🔴 整块 textLen **不足以**判"告警正文有没有":它把按钮标签也算进去。
          //    故分别量消息节点与按钮,并且只回**长度与布尔**,不把措辞带出页面
          //    (措辞是产品可见项,闸不钉它;带进日志只会诱使下一个人拿它当断言)。
          const base = window.__c.connBaseline || {};
          const msg = document.getElementById('streamAlertText');
          const btn = document.getElementById('streamRetryBtn');
          const msgText = msg ? msg.textContent.trim() : null;
          const btnText = btn ? btn.textContent.trim() : null;
          return { present: true, hidden: el.hidden, textLen: (el.textContent || '').replace(/\s+/g, ' ').trim().length,
                   w: +r.width.toFixed(1), h: +r.height.toFixed(1),
                   msgPresent: !!msg, msgLen: msgText === null ? null : msgText.length,
                   msgIsConn: msgText !== null && typeof base.msg === 'string' && msgText === base.msg,
                   btnIsConn: btnText !== null && typeof base.btn === 'string' && btnText === base.btn };
        })(),
        // 守卫:基线没取到 ⇒ 变体判据是空的,必须让腿自己红,不能假装验过
        baseOk: !!(window.__c.connBaseline && window.__c.connBaseline.msgPresent
          && window.__c.connBaseline.msg && window.__c.connBaseline.btn),
        // 页面内的窗口常量真值(读不到就 null,守卫会因此判"腿失去意义"而不是假装量过)
        limit: (function () { try { return REPLAY_HARD_LIMIT_MS; } catch (e) { return null; } })(),
        es: { ...window.__es },
      };
    }).catch((e) => ({ probeError: String(e).slice(0, 100) }));

    const rounds = Math.max(6, Math.floor((leg.secs * 1000) / SAMPLE_MS));
    for (let i = 0; i < rounds; i += 1) {
      await sleep(SAMPLE_MS);
      samples.push(await probe());
    }
    if (!leg.outOfWindow) {
      // All earlier max/visibility/reconnect observations remain. The last sample
      // must describe a completed replay, not a 39/48-chunk transient refill.
      await page.evaluate(() => { window.__finishReplay = true; });
      await page.waitForFunction(() => window.__replayFinished === true, null, {timeout:10000});
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

      // 🔴 慢重放腿的**自证守卫**:必须证明"首条重放确实落在窗内/出窗",否则
      //    FIRST_DELAY 没生效时这两条腿会退化成 L2 并**照样绿**(那是最坏的一种绿)。
      if (leg.firstDelay) {
        await t(`${leg.name} · 守卫:首条重放距 open 的实测间隔确实${leg.outOfWindow ? '越过了' : '落在'}窗口 ${WINDOW_MS_ASSUMED}ms ${leg.outOfWindow ? '之外' : '之内'}`, async () => {
          assert.strictEqual(last.limit, WINDOW_MS_ASSUMED,
            `页面内 REPLAY_HARD_LIMIT_MS 实测 ${last.limit},不是选这两条腿时假设的 ${WINDOW_MS_ASSUMED} ⇒ firstDelay 必须重算,本条已失去意义(不许静默沿用)。${scene}`);
          const gaps = (last.es && last.es.gaps) || [];
          assert.ok(gaps.length >= 2, `只量到 ${gaps.length} 条 open→首条 间隔,判不出与窗口的关系。${scene}`);
          const gMin = Math.min(...gaps);
          const gMax = Math.max(...gaps);
          if (leg.outOfWindow) {
            assert.ok(gMin > WINDOW_MS_ASSUMED,
              `最短间隔 ${gMin}ms 没越过窗口 ${WINDOW_MS_ASSUMED}ms ⇒ 本条根本没走到"窗口过期"那条路径,量的是别的东西。gaps=[${gaps.join(',')}]`);
          } else {
            assert.ok(gMax < WINDOW_MS_ASSUMED,
              `最长间隔 ${gMax}ms 已越过窗口 ${WINDOW_MS_ASSUMED}ms ⇒ 本条不再是"窗内"腿,它的绿不代表修复成立。gaps=[${gaps.join(',')}]`);
            assert.ok(gMin >= leg.firstDelay * 1000 - 200,
              `最短间隔 ${gMin}ms 远小于请求的 ${Math.round(leg.firstDelay * 1000)}ms ⇒ 夹具 FIRST_DELAY 没生效,本条退化成 L2。gaps=[${gaps.join(',')}]`);
          }
        });
      }

      // ── 出窗腿(L4):(c) 落地后**反转成硬闸** —— 出窗 ⇒ 必须出现可见告警 ─────────────
      // 🔵 它**不断言**"出窗不得增长":重复本身要等 ⑰(hub 侧 per-event 序号)才治得了,
      //    (c) 只把**静默**变成**可见**。增长数字仍然打印出来(测量,不是断言),
      //    因为发布单需要它 —— 别让人把"告警出现了"读成"重复修好了"。
      if (leg.outOfWindow) {
        await t(`${leg.name} · 前置事实:出窗时延迟清空一次都没被消费(consumeTrue == 0)`, async () => {
          assert.strictEqual(last.consumeTrue, 0,
            `consumeReconnectClear 返回过 ${last.consumeTrue} 次 true ⇒ 清空在出窗时仍被消费了,本条量的不是"窗口过期"那条路。${scene}`);
          assert.ok((last.arms || []).length >= 1, `一次 arm 都没记到 ⇒ 本条没有落点,判不了告警。${scene}`);
        });

        await t(`${leg.name} · 🔴 窗口过期 ⇒ 必须出现**可见**告警(时间判据:arm 后 ${WINDOW_MS_ASSUMED - ALERT_EARLY_MS}~${WINDOW_MS_ASSUMED + ALERT_LATE_MS}ms 内)`, async () => {
          const hits = alertHitsNearExpiry(last.arms, last.alerts, WINDOW_MS_ASSUMED);
          assert.ok(hits.length >= 1,
            `窗口过期了 ${(last.arms || []).length} 次,却没有一次在 arm 后 [${WINDOW_MS_ASSUMED - ALERT_EARLY_MS}, ${WINDOW_MS_ASSUMED + ALERT_LATE_MS}]ms 内触发告警`
            + ` ⇒ 缺陷仍然是**静默**的,(c) 没落地。实测告警调用=${JSON.stringify(last.alerts)} · arms=${JSON.stringify(last.arms)}。${scene}`);
          assert.strictEqual(last.baseOk, true,
            `conn 变体基线没取到(#streamAlertText / #streamRetryBtn 在页面脚本跑完后读不到)`
            + ` ⇒ "弹的是哪一件事"这条判据是空的,本腿不成立(不是"通过")。${scene}`);
          // 🔴 告警必须**看得见、而且说的就是这件事**,不是"调用发生过":
          //    hidden=false · 盒子非零 · 消息节点存在 · 消息非空 · 消息≠conn 基线 · 按钮≠conn 基线。
          //    🔵 为什么不能只用整块 textContent 非空:那块里**含按钮标签**。实测 M-C3
          //       (replay 文案改成空串)整块仍有 55 字 ⇒ 旧写法 26/0 全绿,而业主看到的是
          //       "连接不上…点【重试】" ⇒ **指错方向**,比没有告警更坏。
          //    🔵 判据全是长度与布尔,**不含任何一句措辞** ⇒ 业主改文案不会让本条红。
          //    用"任一次采样满足"而不是"末次采样满足" —— 告警会在下一次 onopen 被 hideStreamAlert()
          //    藏掉,只看末次会随采样相位忽红忽绿(那是 flaky,不是判据)。
          const shown = valid.filter((s) => s.alertBox && s.alertBox.present && s.alertBox.hidden === false
            && s.alertBox.w > 0 && s.alertBox.h > 0
            && s.alertBox.msgPresent === true && s.alertBox.msgLen > 0
            && s.alertBox.msgIsConn === false && s.alertBox.btnIsConn === false);
          assert.ok(shown.length >= 1,
            `告警函数被调过 ${hits.length} 次,但 ${valid.length} 次采样里**没有一次**同时满足`
            + `「hidden=false · 盒子非零 · 消息节点存在 · 消息非空 · 消息≠conn 基线 · 按钮≠conn 基线」`
            + ` ⇒ 要么业主什么也看不见,要么看见的是**另一件事**的告警。`
            + `末次采样 alertBox=${JSON.stringify((valid[valid.length - 1] || {}).alertBox)}。${scene}`);
          const ab = shown[shown.length - 1].alertBox;
          console.log(`      ↳ 告警命中 ${hits.length} 次;首次 arm→告警 delta=${hits[0].delta}ms(判据区间 ${WINDOW_MS_ASSUMED - ALERT_EARLY_MS}~${WINDOW_MS_ASSUMED + ALERT_LATE_MS}ms)`
            + ` · 实参=${JSON.stringify(hits[0].arg)} · 盒子 ${ab.w}x${ab.h} · 消息 ${ab.msgLen} 字(≠conn 基线)`
            + ` · 按钮变体已换=${!ab.btnIsConn} · 整块 textLen ${ab.textLen} · 可见采样 ${shown.length}/${valid.length}`);
          console.log('      ↳ 🔵 断线告警(7500ms 宽限期)落不进这个区间 ⇒ 它冒充不了本条;区间是算出来的,不是拍的。');
        });

        console.log(`      ↳ 测量(不断言):单份 ${leg.expectChars} 字 → 实测最大 ${maxOf('full')} 字`
          + `(≈${(maxOf('full') / leg.expectChars).toFixed(1)} 份)· consume ${last.consume} 次其中 true ${last.consumeTrue} 次`
          + ` · open→首条 gaps=[${((last.es && last.es.gaps) || []).join(',')}]ms · 页面窗口 ${last.limit}ms`);
        console.log('      ↳ 🔴 重复**仍在**((c) 不治重复,只治静默);治重复要等 ⑰(hub 侧 per-event 序号)。');
        continue;
      }

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
      await t(`${leg.name} · 🔴 幂等机制确实被 arm(mark ≥1 · reset ≥1 · consume 返回 true ≥1)`, async () => {
        assert.ok(last.mark >= 1, `markStreamReconnect 一次都没被调用(${last.mark})⇒ 幂等机制在这条路径上是死代码。${scene}`);
        assert.ok(last.reset >= 1, `resetConversation 一次都没跑(${last.reset})⇒ 清空从未发生。${scene}`);
        // 🔴 为什么还要 consumeTrue:`startApp()` 开机时也调一次 resetConversation(),
        //    所以"reset ≥1"在原理上可能被那次开机清空满足(取决于包裹器何时挂上)。
        //    consumeReconnectClear() 只在 `eventSource.onmessage` 里被调用 ⇒
        //    它返回过 true,才**证明**清空是"由重放的第一条事件穿过窗口触发的",不是别处顺手擦的。
        assert.ok(last.consumeTrue >= 1,
          `consumeReconnectClear 调用 ${last.consume} 次却没有一次返回 true ⇒ 清空不是从延迟窗口这条路走掉的。${scene}`);
      });

      // 🔴 反向断言(健康路径不许被吵):窗内腿**不得**出现"窗口过期时刻的告警"。
      //    没有这一条,(c) 可以写成"每次 arm 都告警"而 L4 照样绿 ⇒ 那又是半条闸。
      await t(`${leg.name} · 🔴 窗内**不得**触发重放超时告警(误报会把真告警淹掉)`, async () => {
        const hits = alertHitsNearExpiry(last.arms, last.alerts, WINDOW_MS_ASSUMED);
        assert.strictEqual(hits.length, 0,
          `窗内腿出现了 ${hits.length} 次"窗口过期时刻的告警" ⇒ 告警在健康路径上误报:${JSON.stringify(hits.slice(0, 3))}。${scene}`);
      });
    }
  } finally {
    await browser.close();
  }

  console.log(`\n  合计 ${pass + fail} 条:✅ ${pass} · 🔴 ${fail}`);
  console.log('  🔵 读法提醒:L4 钉的是「出窗 ⇒ 必须出现可见告警」,**不是**「出窗不重复」。');
  console.log('     重复本身仍在(见 L4 那两行测量输出),治它要 ⑰(hub 侧 per-event 序号)⇒ 别把 rc=0 读成"慢重放修好了"。');
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
