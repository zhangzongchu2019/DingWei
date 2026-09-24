// ① groupSnapshot 必须在【编组工作区没打开时】也被刷新。
// 🔴 从 internal/m8/console_auth.go 的真实页面脚本抽 refreshGroups 求值,不复制实现。
// 为什么要这条格:左栏的「编组-Agent 树」在编组 tab 没打开时也要画,
// 它读的就是 groupSnapshot。旧写法把 groupSnapshot=data 关在 if(groupMode) 里 ⇒
// 不开编组 tab 就永远拿不到数据,而且这个错【不报错】,只是树空着。
const fs = require('fs'), assert = require('assert');
const SRC = '../internal/m8/console_auth.go';

function extract(goSource) {
  const i = goSource.indexOf('<script>'), j = goSource.indexOf('</script>', i);
  assert.ok(i > 0 && j > i, '抽取失败:未找到页面脚本段');
  let block = goSource.slice(i + '<script>'.length, j);
  assert.ok(block.includes('refreshGroups'), '抽取越界:片段里应含 refreshGroups');
  for (const re of [/^\s*refreshPlatform\(\);\s*$/m, /^\s*refreshAgents\(\);\s*$/m,
                    /^\s*refreshGroups\(\);\s*$/m, /^\s*setInterval\(refreshAgents,15000\);\s*$/m]) {
    const before = block;
    block = block.replace(re, '\n');
    assert.notStrictEqual(block, before, `桩失效:自举段未命中 ${re}`);
  }
  return block;
}

function makeEnv() {
  function el(tag) {
    // 🔴 dataset:renderAgentNav 会写 el.dataset.groupCode / .session。
    //    假元素没有它时抛 "Cannot set properties of undefined",而异常被产品 catch 吞掉。
    const n = { tag, children: [], style: {}, attrs: {}, className: '', value: '', hidden: false, dataset: {},
      classList: { _s: new Set(), toggle(c, on) { on ? this._s.add(c) : this._s.delete(c); },
                   contains(c) { return this._s.has(c); } },
      append(...xs) { this.children.push(...xs); },
      replaceChildren(...xs) { this.children = [...xs]; },
      setAttribute(k, v) { this.attrs[k] = String(v); },
      insertBefore(node, ref) { const at = ref ? this.children.indexOf(ref) : -1;
        if (at < 0) this.children.push(node); else this.children.splice(at, 0, node); return node; },
      querySelector() { return null; },
      remove() { n.removed = true; }, removed: false };
    return n;
  }
  // 🔴 2026-09-24 补桩:refreshGroups 现在会调 renderAgentNav(),它要 #a 与 #agentNavMode。
  //    缺了不会报错 —— 异常被产品自己的 catch 吞掉,refreshGroups 静默返回 null,
  //    于是这条格红在"应把 data 返回出来"上,看起来像产品坏了。(纪律五十二)
  const byId = { tabs: el('div'), panes: el('div'), hint: el('div'), a: el('nav'),
    retryPlatform: el('button'), retryAgents: el('button'), agentNavMode: el('button'),
    'agents-status': el('div'), 'platform-items': el('div'), 'platform-status': el('div') };
  return { document: { createElement: el, getElementById: id => byId[id] || null }, byId,
           alert() {}, encodeURIComponent };
}

// 载荷:两个组、一个自有 Agent。编组工作区【不打开】。
const PAYLOAD = { owned_agents: [{ session: 'zzc-a-1', online: true }],
                  groups: [{ code: 'g1', short_name: 'alpha' }, { code: 'g2', short_name: 'beta' }] };

function load(block, env) {
  const fn = new Function('document', 'alert', 'encodeURIComponent', 'fetch', 'setInterval', `
    ${block}
    return {refreshGroups, getSnapshot: function(){return groupSnapshot;}, isGroupMode: function(){return groupMode;}};
  `);
  return fn(env.document, env.alert, env.encodeURIComponent,
    () => Promise.resolve({ ok: true, json: () => Promise.resolve(PAYLOAD), text: () => Promise.resolve('') }),
    () => { throw new Error('本测试不应注册真实定时器'); });
}

let pass = 0, fail = 0;
const t = async (name, f) => {
  try { await f(); console.log('  ✅', name); pass++; }
  catch (e) { console.log('  🔴', name, '->', String(e && e.message || e).split('\n')[0]); fail++; }
};

// 🔴 变异锚点绑在【只有本次改动落地后才存在】的字节上:
//    "return null;groupSnapshot=data;if(groupMode){" —— 改动前这串不存在。
// 🔴 2026-09-24 改锚:原锚是 'return null;groupSnapshot=data;if(groupMode){' ——
//    它把【左右邻居】一起绑进了判据。产品后来在中间插了一句 renderAgentNav();,
//    锚点当场失配,这条格连着两个交付锚都是红的(纪律五十二:桩跟着产品漂)。
//    ⇒ 新锚只绑【赋值本身】这一小段,并用"把它包进 if(groupMode)"来复现那次回退。
//      邻居再怎么加,这个锚都还在;而它改的正是本条要防的那件事:赋值被 groupMode 挡住。
const MUTATION = {
  name: 'snapshot-back-inside-groupmode',
  from: 'groupSnapshot=data;',
  to:   'if(groupMode)groupSnapshot=data;',
};

(async () => {
  const goSource = fs.readFileSync(SRC, 'utf8');
  console.log('① groupSnapshot 刷新（源自 internal/m8/console_auth.go 实际文本）');

  await t('甲 编组工作区未打开时,refreshGroups 仍把响应写进 groupSnapshot', async () => {
    const env = makeEnv(), M = load(extract(goSource), env);
    assert.strictEqual(M.isGroupMode(), false, '前置:本例必须在 groupMode=false 下跑');
    const returned = await M.refreshGroups();
    assert.ok(returned, 'refreshGroups 应把 data 返回出来');
    const snap = M.getSnapshot();
    assert.ok(Array.isArray(snap.groups), '🔴 groupSnapshot.groups 必须是数组(未刷新时它是初值)');
    assert.strictEqual(snap.groups.length, 2, '🔴 未开编组 tab 也必须拿到 2 个组');
    assert.strictEqual(snap.groups[0].code, 'g1');
    assert.ok(Array.isArray(snap.owned_agents) && snap.owned_agents.length === 1,
      '🔴 owned_agents 也必须一起进快照(左栏树判"是不是自己的"靠它)');
  });

  await t(`乙 变异 ${MUTATION.name}:把赋值关回 if(groupMode) 里 ⇒ 甲必须红`, async () => {
    assert.ok(goSource.includes(MUTATION.from),
      `🔴 变异锚点未命中 —— 产品里找不到 ${MUTATION.from}`);
    const mutated = goSource.replace(MUTATION.from, MUTATION.to);
    assert.notStrictEqual(mutated, goSource, '🔴 变异未改动任何字节 —— 锚点失效');
    const env = makeEnv(), M = load(extract(mutated), env);
    await M.refreshGroups();
    const snap = M.getSnapshot();
    assert.ok(!(Array.isArray(snap.groups) && snap.groups.length === 2),
      '🔴 变异后甲仍然通过 ⇒ 甲这条格证明不了任何东西');
  });

  console.log(`\n通过 ${pass} / 失败 ${fail}`);
  process.exit(fail ? 1 : 0);
})();
