// T-138 回归:左栏短名取法。跑法: node renderer-t138-shortname-regression.js
// 🔴 从 internal/m8/console_auth.go 的【真实页面源码】抽出 dwShortName 求值,不复制粘贴 ——
//    复制一份来测,测的就是副本,原件改了测试还会绿。
const fs = require('fs'), assert = require('assert');
const src = fs.readFileSync('../internal/m8/console_auth.go', 'utf8');

const FN = 'function dwShortName(';
const f = src.indexOf(FN); assert.ok(f > 0, '抽取失败:dwShortName 未出现在页面源码里');
let depth = 0, end = -1;
for (let k = src.indexOf('{', f); k < src.length; k++) {
  if (src[k] === '{') depth++;
  else if (src[k] === '}') { depth--; if (depth === 0) { end = k + 1; break; } }
}
assert.ok(end > f, '抽取失败:未找到函数闭合括号');
const block = src.slice(f, end);
assert.ok(!/document|fetch|window/.test(block), '🔴 抽取越界:片段内不该出现 DOM/网络调用');
const dwShortName = new Function(`${block} return dwShortName;`)();

let pass = 0, fail = 0;
const t = (n, f_) => { try { f_(); console.log('  ✅', n); pass++; } catch (e) { console.log('  🔴', n, '->', e.message.split('\n')[0]); fail++; } };

console.log('T-138 短名取法（源自 internal/m8/console_auth.go 实际文本）');

t('① 三段式 ⇒ 取中间段', () => {
  assert.strictEqual(dwShortName('zzc-opus50ts-2642'), 'opus50ts');
  assert.strictEqual(dwShortName('zzc-prodops-2642'), 'prodops');
});
t('② 🔴 业主名下 20 条全部可缩且互不相同（真实名单）', () => {
  const names = ['zzc-coder56solts-2642','zzc-coder56solzzc-2642','zzc-coordinator-2642',
    'zzc-dev55-2642','zzc-dev5520-2642','zzc-kimi30kqtuxerh-2642','zzc-mac-2642',
    'zzc-openapi4ai-2642','zzc-openapi4aiterrazzc-2642','zzc-opus50tc2k-2642',
    'zzc-opus50ts-2642','zzc-prodops-2642','zzc-publicreviewerts-2642',
    'zzc-qwen38maxkhovvob4-2642','zzc-togpt56terrats-2642','zzc-togpt56terrazzc-2642',
    'zzc-tosonnet50tc2k-2642','zzc-tosonnet50ts-2642','zzc-ts3dev-2642','zzc-ts3pocops-2642'];
  const shorts = names.map(dwShortName);
  assert.strictEqual(new Set(shorts).size, 20, '20 条短名应互不相同');
  shorts.forEach((s, i) => assert.ok(s.length < names[i].length, names[i] + ' 未被缩短'));
});
t('③ 🔴 近似名不得被缩成同一个（最易出事的五对）', () => {
  [['zzc-coder56solts-2642','zzc-coder56solzzc-2642'],
   ['zzc-dev55-2642','zzc-dev5520-2642'],
   ['zzc-opus50tc2k-2642','zzc-opus50ts-2642'],
   ['zzc-togpt56terrats-2642','zzc-togpt56terrazzc-2642'],
   ['zzc-tosonnet50tc2k-2642','zzc-tosonnet50ts-2642']].forEach(([a, b]) =>
     assert.notStrictEqual(dwShortName(a), dwShortName(b), a + ' 与 ' + b + ' 被缩成同一个'));
});
t('④ 🔴 非三段式 ⇒ 原样返回（外来格式不猜）', () => {
  assert.strictEqual(dwShortName('systemtaskintl-secops71-7094'), 'secops71'); // 恰为三段,仍取中间
  assert.strictEqual(dwShortName('twoparts-only'), 'twoparts-only');
  assert.strictEqual(dwShortName('a-b-c-d'), 'a-b-c-d');
  assert.strictEqual(dwShortName('nodash'), 'nodash');
});
t('⑤ 空串与异常输入不抛', () => {
  assert.strictEqual(dwShortName(''), '');
  assert.doesNotThrow(() => dwShortName(null));
});

console.log(`\n通过 ${pass} / 失败 ${fail}`);
process.exit(fail ? 1 : 0);
