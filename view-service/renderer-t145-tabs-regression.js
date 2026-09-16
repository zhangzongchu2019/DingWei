// T-145 回归:控制台多 tab 行为。跑法: node renderer-t145-tabs-regression.js
// 🔴 从 internal/m8/console_auth.go 的【真实页面源码】抽出 tab 逻辑求值,不复制粘贴 ——
//    复制一份来测,测的就是副本;原件改了测试还会绿。
const fs = require('fs'), assert = require('assert');
const src = fs.readFileSync('../internal/m8/console_auth.go', 'utf8');

// 抽出 <script> ... </script> 之间那段(页面模板是 Go 反引号字符串)
const i = src.indexOf('<script>'), j = src.indexOf('</script>', i);
assert.ok(i > 0 && j > i, '抽取失败:未找到页面脚本段');
const block = src.slice(i + '<script>'.length, j);
assert.ok(block.includes('MAX_TABS'), '🔴 抽取越界或抽错段:片段里应含 MAX_TABS');

// 用极简 DOM 替身求值 —— 只实现被测逻辑真正用到的那几个能力
function makeEnv() {
  const made = [];
  function el(tag) {
    const n = {
      tag, children: [], style: {}, classList: {
        _s: new Set(),
        toggle(c, on) { on ? this._s.add(c) : this._s.delete(c); },
        contains(c) { return this._s.has(c); },
      },
      append(...xs) { this.children.push(...xs); },
      remove() { n.removed = true; },
      removed: false,
    };
    made.push(n);
    return n;
  }
  const byId = { tabs: el('div'), panes: el('div'), hint: el('div'), a: el('nav') };
  const document = { createElement: el, getElementById: (id) => byId[id] || null };
  const alerts = [];
  return { document, alert: (m) => alerts.push(m), alerts, byId, made, encodeURIComponent };
}

function load(env) {
  const fn = new Function('document', 'alert', 'encodeURIComponent', 'fetch', `
    ${block.replace(/fetch\([\s\S]*$/, '')}   // 掐掉末尾的 fetch 调用,只留纯逻辑
    return { openTab, closeTab, activate, tabs, dwShortName, MAX_TABS };
  `);
  return fn(env.document, env.alert, env.encodeURIComponent, () => { throw new Error('不该调 fetch'); });
}

let pass = 0, fail = 0;
const t = (n, f) => { try { f(); console.log('  ✅', n); pass++; } catch (e) { console.log('  🔴', n, '->', e.message.split('\n')[0]); fail++; } };

console.log('T-145 多 tab 行为（源自 internal/m8/console_auth.go 实际文本）');

t('① 点一个 Agent ⇒ 增加一个 tab 与一个 pane', () => {
  const env = makeEnv(), M = load(env);
  M.openTab('zzc-opus50ts-2642');
  assert.strictEqual(M.tabs.size, 1);
  assert.strictEqual(env.byId.tabs.children.length, 1);
  assert.strictEqual(env.byId.panes.children.length, 1);
});

t('② 🔴 再点【同一个】⇒ 不得重建(重建即丢失那一页内容)', () => {
  const env = makeEnv(), M = load(env);
  M.openTab('zzc-opus50ts-2642');
  const pane = env.byId.panes.children[0];
  M.openTab('zzc-opus50ts-2642');
  // 🔴 排他:首版只断言 tabs.size 与 children[0] —— 变异实测【没抓到】。
  //    因为重建时 map 被覆盖(size 仍 1),而新 pane 追加在 children[1],children[0] 仍是原件。
  //    ⇒ 我查的是「第一个还在不在」,不是「有没有多出来一个」。必须查【数量】。
  assert.strictEqual(env.byId.panes.children.length, 1, '🔴 不得新建第二个 pane(新建=那一页被重载)');
  assert.strictEqual(env.byId.tabs.children.length, 1, '不得新建第二个 tab');
  assert.strictEqual(M.tabs.size, 1, '同一 Agent 不得产生第二个 tab');
  assert.strictEqual(env.byId.panes.children[0], pane, 'pane 必须仍是同一个对象');
});

t('③ 点不同 Agent ⇒ tab 数随之增加', () => {
  const env = makeEnv(), M = load(env);
  ['a-x-1', 'a-y-1', 'a-z-1'].forEach(M.openTab);
  assert.strictEqual(M.tabs.size, 3);
});

t('④ 🔴 切换用显示/隐藏,不动 src', () => {
  const env = makeEnv(), M = load(env);
  M.openTab('a-x-1'); M.openTab('a-y-1');
  const px = M.tabs.get('a-x-1').pane, py = M.tabs.get('a-y-1').pane;
  const srcX = px.src;
  M.activate('a-x-1');
  assert.strictEqual(px.style.display, 'block');
  assert.strictEqual(py.style.display, 'none');
  assert.strictEqual(px.src, srcX, '🔴 切换不得改 src');
});

t('⑤ 🔴 上限 8:第 9 个必须被拒且给出提示(不能静默不开)', () => {
  const env = makeEnv(), M = load(env);
  for (let k = 0; k < 8; k++) M.openTab('a-s' + k + '-1');
  assert.strictEqual(M.tabs.size, 8);
  M.openTab('a-s8-1');
  assert.strictEqual(M.tabs.size, 8, '超限不得新建');
  assert.strictEqual(env.alerts.length, 1, '🔴 必须提示 —— 静默不开是「失败没有声音」');
  assert.ok(/8/.test(env.alerts[0]), '提示里应说明上限是多少');
});

t('⑥ 关闭 ⇒ tab 与 pane 都被移除(才会断开那条长连接)', () => {
  const env = makeEnv(), M = load(env);
  M.openTab('a-x-1'); M.openTab('a-y-1');
  const e = M.tabs.get('a-x-1');
  M.closeTab('a-x-1');
  assert.strictEqual(M.tabs.size, 1);
  assert.ok(e.pane.removed, '🔴 pane 必须真被移除,否则连接不会断');
  assert.ok(e.tab.removed);
});

t('⑦ 关掉一个后仍可再开(上限是并发数,不是累计数)', () => {
  const env = makeEnv(), M = load(env);
  for (let k = 0; k < 8; k++) M.openTab('a-s' + k + '-1');
  M.closeTab('a-s0-1');
  M.openTab('a-new-1');
  assert.strictEqual(M.tabs.size, 8);
  assert.ok(M.tabs.has('a-new-1'));
});

t('⑧ tab 标题用短名,但 title 保留全名', () => {
  const env = makeEnv(), M = load(env);
  M.openTab('zzc-opus50ts-2642');
  const tab = env.byId.tabs.children[0];
  assert.strictEqual(tab.title, 'zzc-opus50ts-2642');
  assert.strictEqual(tab.children[0].textContent, 'opus50ts');
});

console.log(`\n通过 ${pass} / 失败 ${fail}`);
process.exit(fail ? 1 : 0);
