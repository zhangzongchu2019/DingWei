// T-146:左栏轮询不得重建右侧 tab/pane。直接抽 Go 中真实页面脚本求值。
const fs = require('fs'), assert = require('assert');
const src = fs.readFileSync('../internal/m8/console_auth.go', 'utf8');
const i = src.indexOf('<script>'), j = src.indexOf('</script>', i);
assert.ok(i > 0 && j > i, '抽取失败:未找到页面脚本段');
let block = src.slice(i + '<script>'.length, j);
assert.ok(block.includes('refreshAgents') && block.includes('applyAgentSnapshot'), '抽取越界或不是 T-146 页面');
// 不执行真实网络与定时器，只求值真实函数；所有行为仍来自 Go 源码，不复制实现。
// 桩:剥掉页面自举段(console_auth.go:513-519)。现在自举是
//   refreshPlatform(); refreshAgents(); refreshGroups(); setInterval(refreshAgents,15000);
// 逐条剥,剥不掉就报错 —— 不允许静默跳过(剥不掉 = 下面的定时器守卫会替它报)。
const BOOTSTRAP = [/^\s*refreshPlatform\(\);\s*$/m, /^\s*refreshAgents\(\);\s*$/m,
                   /^\s*refreshGroups\(\);\s*$/m, /^\s*setInterval\(refreshAgents,15000\);\s*$/m];
for (const re of BOOTSTRAP) {
  const before = block;
  block = block.replace(re, '\n');
  assert.notStrictEqual(block, before, `桩失效:自举段未命中 ${re} —— 页面自举改了形状,先修桩`);
}

function makeEnv() {
  const made = [];
  function el(tag) {
    const n = {
      tag, children: [], style: {}, attrs: {}, disabled: false, className: '',
      classList: {
        _s: new Set(),
        toggle(c, on) { on ? this._s.add(c) : this._s.delete(c); },
        contains(c) { return this._s.has(c); },
      },
      append(...xs) { this.children.push(...xs); },
      // 桩:applyAgentSnapshot 现在会在 tab 里插一个 .tab-state(console_auth.go:464)。
      querySelector(sel) {
        const cls = sel.replace(/^\./, '');
        for (const c of this.children) {
          if (c.className === cls || (c.classList && c.classList.contains(cls))) return c;
          const deep = c.querySelector && c.querySelector(sel);
          if (deep) return deep;
        }
        return null;
      },
      insertBefore(node, ref) {
        const at = ref ? this.children.indexOf(ref) : -1;
        if (at < 0) this.children.push(node); else this.children.splice(at, 0, node);
        return node;
      },
      replaceChildren(...xs) { this.children = [...xs]; },
      setAttribute(k, v) { this.attrs[k] = String(v); },
      remove() { n.removed = true; },
      removed: false,
    };
    made.push(n);
    return n;
  }
  // 桩:页面脚本顶层会给这两个按钮接线(console_auth.go:514-515),缺了就 null.onclick。
  const byId = { tabs: el('div'), panes: el('div'), hint: el('div'), a: el('nav'),
    retryPlatform: el('button'), retryAgents: el('button'),
    'agents-status': el('div'), 'platform-items': el('div'), 'platform-status': el('div') };
  const document = { createElement: el, getElementById: id => byId[id] || null };
  return { document, byId, made, alert() {}, encodeURIComponent };
}

function load(env, fetchImpl = () => { throw new Error('本测试不应发真实请求'); }) {
  const fn = new Function('document', 'alert', 'encodeURIComponent', 'fetch', 'setInterval', `
    ${block}
    return {openTab, tabs, renderAgentList, applyAgentSnapshot, refreshAgents};
  `);
  return fn(env.document, env.alert, env.encodeURIComponent, fetchImpl,
    () => { throw new Error('本测试不应注册真实定时器'); });
}

let pass = 0, fail = 0;
const t = (name, f) => { try { f(); console.log('  ✅', name); pass++; } catch (e) { console.log('  🔴', name, '->', e.message); fail++; } };
const online = session => ({session, online: true});
const offline = session => ({session, online: false});

console.log('T-146 Agent 列表刷新（源自 internal/m8/console_auth.go 实际文本）');

t('① 掉线快照重建左栏,但既有 tab/pane/iframe src 原样保留并标灰', () => {
  const env = makeEnv(), M = load(env);
  M.renderAgentList([online('a-x-1')]); M.applyAgentSnapshot([online('a-x-1')]);
  M.openTab('a-x-1');
  const entry = M.tabs.get('a-x-1'), tabCount = env.byId.tabs.children.length;
  const paneCount = env.byId.panes.children.length, src = entry.pane.src;
  M.renderAgentList([offline('a-x-1')]); M.applyAgentSnapshot([offline('a-x-1')]);
  assert.strictEqual(env.byId.a.children.length, 1, '左栏必须替换快照,不得叠加旧项');
  assert.strictEqual(M.tabs.size, 1, '掉线不得删除 tab');
  assert.strictEqual(env.byId.tabs.children.length, tabCount, '刷新不得新增/重建 tab');
  assert.strictEqual(env.byId.panes.children.length, paneCount, '刷新不得新增/重建 pane');
  assert.strictEqual(M.tabs.get('a-x-1'), entry, 'tab 记录对象必须保持同一对象');
  assert.strictEqual(entry.pane.src, src, 'iframe src 不得改变');
  assert.ok(entry.tab.classList.contains('offline'), '掉线 tab 必须标灰');
  assert.strictEqual(entry.tab.attrs['aria-disabled'], 'true', '掉线 tab 必须标出不可交互');
  assert.strictEqual(entry.pane.style.pointerEvents, 'none', '掉线 tab 不得继续交互');
});

t('② 重连快照恢复左栏与既有 tab 可交互性,仍不重载 iframe', () => {
  const env = makeEnv(), M = load(env);
  M.renderAgentList([offline('a-x-1')]); M.applyAgentSnapshot([offline('a-x-1')]);
  // 测试直接开已存在的历史 tab；真实页面中它来自掉线前。
  M.openTab('a-x-1'); M.applyAgentSnapshot([offline('a-x-1')]);
  const entry = M.tabs.get('a-x-1'), src = entry.pane.src;
  M.renderAgentList([online('a-x-1')]); M.applyAgentSnapshot([online('a-x-1')]);
  assert.strictEqual(M.tabs.size, 1);
  assert.strictEqual(M.tabs.get('a-x-1'), entry);
  assert.strictEqual(entry.pane.src, src);
  assert.ok(!entry.tab.classList.contains('offline'), '重连后须取消标灰');
  assert.strictEqual(entry.pane.style.pointerEvents, '', '重连后须恢复交互');
  assert.strictEqual(env.byId.a.children[0].disabled, false, '左栏按钮须恢复可点');
});

t('③ 列表增删只改变左栏数量,不改变已开右栏数量', () => {
  const env = makeEnv(), M = load(env);
  M.openTab('a-x-1'); M.openTab('a-y-1');
  const panes = [...env.byId.panes.children], srcs = panes.map(p => p.src);
  M.renderAgentList([online('a-z-1')]); M.applyAgentSnapshot([online('a-z-1')]);
  assert.strictEqual(env.byId.a.children.length, 1, '左栏应替换为最新快照');
  assert.strictEqual(M.tabs.size, 2, '历史 tab 数不得随左栏快照改变');
  assert.strictEqual(env.byId.panes.children.length, 2, 'pane 数不得随左栏快照改变');
  assert.deepStrictEqual(env.byId.panes.children, panes, 'pane 对象与顺序均不得改变');
  assert.deepStrictEqual(env.byId.panes.children.map(p => p.src), srcs, '所有 iframe src 均不得改变');
});

(async () => {
  try {
    const env = makeEnv();
    const M = load(env, () => Promise.reject(new Error('temporary network failure')));
    M.renderAgentList([online('a-x-1'), offline('a-y-1')]);
    M.applyAgentSnapshot([online('a-x-1'), offline('a-y-1')]);
    M.openTab('a-x-1');
    const left = [...env.byId.a.children];
    const entry = M.tabs.get('a-x-1'), pane = entry.pane, src = pane.src;
    await M.refreshAgents();
    assert.strictEqual(env.byId.a.children.length, left.length, '请求失败不得清空或累加左栏');
    assert.deepStrictEqual(env.byId.a.children, left, '请求失败须保留上一次左栏快照对象');
    assert.strictEqual(M.tabs.size, 1, '请求失败不得删除已开 tab');
    assert.strictEqual(M.tabs.get('a-x-1'), entry, '请求失败不得重建 tab');
    assert.strictEqual(entry.pane, pane, '请求失败不得重建 pane');
    assert.strictEqual(entry.pane.src, src, '请求失败不得重载 iframe');
    console.log('  ✅ ④ fetch 失败保留左栏快照与已开 tab/pane/src'); pass++;
  } catch (e) {
    console.log('  🔴 ④ fetch 失败保留左栏快照与已开 tab/pane/src ->', e.message); fail++;
  }
  console.log(`\n通过 ${pass} / 失败 ${fail}`);
  process.exit(fail ? 1 : 0);
})();
