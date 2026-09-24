// t145/t146 的变异夹具 —— 🔴 这不是套件,是【预加载】,自己跑没有断言。
// 跑法:MUT=<变异名> node -r ./renderer-t145-t146-mutations.js renderer-t145-tabs-regression.js
//
// 为什么要它:t145/t146 原本【没有任何变异能力】—— 这正是它们能悄悄烂掉三次的原因之一。
// 它拦截 fs.readFileSync,把产品源码 console_auth.go 在【读进内存那一刻】改掉,
// 🔴 不落盘、不改工作树 —— 跑完 git status 仍然干净。
// 锚点未命中就 exit 4,不静默空跑(命中 0 必须失败)。
//
// 组长 2026-09-24 裁定「换量法仍须绿/红两极」,这四个变异就是红那一极的来源:
//   no-pane-append      openTab 不再把 pane 挂进 #panes        ⇒ t145 ①② 红
//   always-rebuild      openTab 去掉"已开过就切过去"           ⇒ t145 ② 红
//   snapshot-adds-pane  applyAgentSnapshot 顺手多塞一个 iframe ⇒ t146 ①③ 红
//   keep-list-on-fail   refreshAgents 失败时不清空左栏         ⇒ t146 ④ 红

const fs = require('fs'), path = require('path');
const RULES = {
  'no-pane-append': ["document.getElementById('panes').append(pane,offlineReason)",
                     "document.getElementById('panes').append(offlineReason)"],
  'always-rebuild': ["if(tabs.has(session)){ activate(session); return; }", ""],
  'snapshot-adds-pane': ["e.pane.style.pointerEvents=online?'':'none';",
                         "e.pane.style.pointerEvents=online?'':'none';document.getElementById('panes').append(document.createElement('iframe'));"],
  'keep-list-on-fail': ["catch(err){renderAgentList([]);", "catch(err){"],
};
const name = process.env.MUT;
const rule = RULES[name];
if (!rule) { console.error('unknown MUT=' + name); process.exit(3); }
const orig = fs.readFileSync;
fs.readFileSync = function (p, ...rest) {
  const out = orig.call(fs, p, ...rest);
  if (typeof p === 'string' && p.endsWith('console_auth.go') && typeof out === 'string') {
    if (!out.includes(rule[0])) { console.error(`🔴 变异 ${name} 锚点未命中`); process.exit(4); }
    return out.split(rule[0]).join(rule[1]);
  }
  return out;
};
