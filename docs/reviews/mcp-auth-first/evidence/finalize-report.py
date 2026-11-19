from pathlib import Path
import json,re
A=Path(__file__).resolve().parent;f=A/'go-count3-anchor.log';lines=f.read_text(errors='replace').splitlines();z=json.loads((A/'z3-anchor-summary.json').read_text());assert z['state']=='PASS_POLICY'
s=(A/'REPORT-DRAFT.md').read_text();s=s.replace('最终读数将在门结束后填写；未完成前不记通过。','最终门按既定Z3例外完成，原始三遍并非全绿，见下列独立分项；不把author门当独立复核结论。')
s=s.replace('候选按既定Z3规则另起全新串行五轮整包，五轮每轮原日志单独保存。','候选按既定Z3规则另起全新串行五轮整包，五轮每轮原日志单独保存，终值见表。')
parts=[];start=0
for i,l in enumerate(lines):
 if l.startswith(('ok  ','FAIL\t')):
  t=lines[start:i+1];parts.append((l,sum(x.startswith('--- PASS: ') for x in t),sum(bool(re.match(r'^\s*--- PASS:',x)) for x in t),sum(bool(re.match(r'^\s*--- FAIL:',x)) for x in t),sum(bool(re.match(r'^\s*--- SKIP:',x)) for x in t)));start=i+1
section='''## 最终作者门终值（全部绑定ce9）

canonical：两个helper profile各1270，inventory97，`BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`。OpenSpec strict42/0（另有最终文档strict）。Go1.22.2、Python3.12.3、普通uid1001、PG18.3真实子进程证据在anchor-pg-runtime.json；最终两个白名单的PYTHONPATH均指本分支，不沿用早期GROUPS路径。

下表三遍PASS/FAIL/SKIP按各级`---`记录统计；顶层PASS另列，子项不重复冒称独立顶层测试。

| 包（三遍） | 顶层PASS | 含子项PASS | 含子项FAIL | 含子项SKIP | 原始包结果 |
|---|---:|---:|---:|---:|---|
'''
for l,p,a,ff,k in parts:section+=f'| {l.split()[1].split("/")[-1]} | {p} | {a} | {ff} | {k} | `{l}` |\n'
section+='''
35个唯一SKIP名称／105次出现逐名及条件见SKIPS.md与SKIPS.json；没有将FAIL改SKIP。没有强启浏览器/真实CLI/perf等其它opt-in，未宣称真实Antigravity验收。

按既定Z3允许名单另跑**全新串行五轮**m8，未择优、未沿用上述三遍：

| 新轮次 | rc | 顶层PASS | 含子项PASS | 各级SKIP | interrupted(9)原始行 | FAIL |
|---|---:|---:|---:|---:|---:|---|
'''
for x in z['rounds']:section+=f"| {x['round']} | {x['rc']} | {x['pass_top']} | {x['pass_all']} | {x['skip_occurrences']} | {x['interrupted_lines']} | {', '.join(t['name'] for t in x['failures']) or '0'} |\n"
section+=f"\n五轮全绿{z['fully_green']}/5；其它FAIL仅可为已批准名单的同形interrupted，否则脚本当场停报。每轮command/result/log与UTC在evidence/z3-anchor-round-*，原三遍rc1及main206对照也完整保留。无store改动或名单扩张，不以单跑绿替代整包。此例外只处置继承N4，不说明其已修复。\n\n"
s=s.replace('## 变异与负对照',section+'## 变异与负对照')
s=s.replace('待：完整门终值、push/ls-remote及独立复核。下一步按全门结果归档；R6新锚到即优先复验，不将其错误复验范围混入本片。','作者完整门、十二刀与原始失败已归档，产品未改；初交审623fcf4c已push且ls-remote核同，qwen按组长令与五轮并行复核。下一步只追加文档、核最终远端、给qwen与组长补证，等待独立复核／终审；不自并AUTH、不部署。R6已交PASS并已在r61第四段合入，不再重复复验。')
(A/'REPORT-DRAFT.md').write_text(s)
print('report final tables prepared')
