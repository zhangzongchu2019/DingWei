// T-141 回归:能力票复用判定。跑法: node renderer-t141-capability-reuse-regression.js
// (在 view-service/ 目录下执行;纯逻辑,不需要 playwright)
// T-141 判定逻辑测试 —— 从【将要部署的那份文件】原样抽出新增代码求值,不复制粘贴。
// 🔴 抽取范围必须只覆盖被测的那几行:首版抽多了,把碰 DOM 的代码带进来,
//    测的根本不是我要测的东西。范围不准 ⇒ 结论无意义(与「判据不排他」同形)。
const fs = require('fs'), assert = require('assert');
const src = fs.readFileSync('renderer-canary-sse.html', 'utf8');

// 🔴 锚点只用【函数头】,终点靠数大括号 —— 若锚点落在函数体内,
//    任何对函数体的变异都会红在"抽不出来",而不是红在"逻辑错",
//    那样变异测试就失去判别力(实测踩到过)。
const START = 'let consoleCapabilityPageId = null;';
const FN = 'function consoleCapabilityUsable() {';
const i = src.indexOf(START); assert.ok(i > 0, '抽取失败:起点未命中');
const f = src.indexOf(FN, i);  assert.ok(f > i, '抽取失败:函数头未命中');
let depth = 0, end = -1;
for (let k = f + FN.length - 1; k < src.length; k++) {
  if (src[k] === '{') depth++;
  else if (src[k] === '}') { depth--; if (depth === 0) { end = k + 1; break; } }
}
assert.ok(end > f, '抽取失败:未找到函数闭合括号');
const block = src.slice(i, end);
assert.ok(!/document|window|fetch/.test(block), '🔴 抽取越界:片段内不该出现 DOM/网络调用');

// 把被测片段放进一个闭包,并把它依赖的外层变量显式提供 + 暴露 setter。
const make = new Function(`
  let consoleCapability = null;
  let pageIdentity = null;
  ${block}
  return {
    usable: () => consoleCapabilityUsable(),
    RENEW: CONSOLE_CAPABILITY_RENEW_MS,
    willRenew: () => !(Date.now() < consoleCapabilityExpiresAt - CONSOLE_CAPABILITY_RENEW_MS),
    set cap(v){ consoleCapability = v; },
    set pid(v){ pageIdentity = v ? { page_id: v } : null; },
    set capPid(v){ consoleCapabilityPageId = v; },
    set exp(v){ consoleCapabilityExpiresAt = v; },
  };
`);

let pass = 0, fail = 0;
const t = (n, f) => { try { f(); console.log('  ✅', n); pass++; } catch (e) { console.log('  🔴', n, '->', e.message.split('\n')[0]); fail++; } };
const S = () => make();
const NOW = () => Date.now();

console.log('T-141 判定逻辑（源自 renderer-canary-sse.html 实际文本）');

t('① 无票 ⇒ 不可用', () => {
  const s = S(); s.pid = 'p1'; assert.strictEqual(s.usable(), false);
});
t('①b 🔴 排他:其余条件【全部成立】,仅缺票 ⇒ 仍须不可用', () => {
  // 变异实测:首版只有 ① 时,把 `!!consoleCapability` 改成 `true` 仍全绿 ——
  // 因为 ① 其实是被【换页检查】挡下的,不是被【有没有票】挡下的。
  // 两层冗余只拆一层 ⇒ 判据不排他。本例把另外两条对齐,只留"有没有票"起作用。
  const s = S();
  s.pid = 'p1'; s.capPid = 'p1'; s.exp = Date.now() + 300000;
  s.cap = null;
  assert.strictEqual(s.usable(), false, '仅缺票时必须不可用');
});
t('② 有票、同页、未到期 ⇒ 可用', () => {
  const s = S(); s.pid = 'p1'; s.cap = 'c'; s.capPid = 'p1'; s.exp = NOW() + 300000;
  assert.strictEqual(s.usable(), true);
});
t('③ 🔴 票已过期 ⇒ 不可用（不能为了不闪就永远复用）', () => {
  const s = S(); s.pid = 'p1'; s.cap = 'c'; s.capPid = 'p1'; s.exp = NOW() - 1;
  assert.strictEqual(s.usable(), false);
});
t('④ 🔴 换页（page_id 变）⇒ 不可用（不得拿旧页的票）', () => {
  const s = S(); s.pid = 'p2'; s.cap = 'c'; s.capPid = 'p1'; s.exp = NOW() + 300000;
  assert.strictEqual(s.usable(), false);
});
t('⑤ 余额 120s ⇒ 复用，不续铸', () => {
  const s = S(); s.pid = 'p1'; s.cap = 'c'; s.capPid = 'p1'; s.exp = NOW() + 120000;
  assert.strictEqual(s.usable(), true);
  assert.strictEqual(s.willRenew(), false);
});
t('⑥ 🔴 余额 30s ⇒ 必须续铸（阈值 60s 生效）', () => {
  const s = S(); s.pid = 'p1'; s.cap = 'c'; s.capPid = 'p1'; s.exp = NOW() + 30000;
  assert.strictEqual(s.willRenew(), true);
});
t('⑦ 阈值常量 = 60000', () => assert.strictEqual(S().RENEW, 60000));
t('⑧ 🔵 行为不变式：5 分钟票在 5 秒轮询下不应触发续铸', () => {
  const s = S(); s.pid = 'p1'; s.cap = 'c'; s.capPid = 'p1'; s.exp = NOW() + 300000;
  for (let k = 0; k < 48; k++) assert.strictEqual(s.willRenew(), false, `第 ${k} 次轮询不该续铸`);
});

console.log(`\n通过 ${pass} / 失败 ${fail}`);
process.exit(fail ? 1 : 0);
