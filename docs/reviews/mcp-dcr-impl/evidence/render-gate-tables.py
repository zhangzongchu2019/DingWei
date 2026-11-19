from pathlib import Path
import json,collections
A=Path(__file__).resolve().parent
phases=['go-packages-count3','z3-baseline4a-view20',*[f'z3-fresh-{i}' for i in range(1,6)],'migration-full','cmd-focused','focused-race']
rows=['# 逐门逐包与SKIP清单','','计数为运行实例，含count重复；top不含子项，all包含各级子项。FAIL列表含父项，不能直接当独立缺陷数。','', '| 命令日志 | 包 | top PASS/SKIP | all PASS/SKIP | FAIL项 | interrupted行 |','| --- | --- | --- | --- | --- | --- |']
skips=['# SKIP逐名条件','','同名重复运行合并列次数；条件取原始Go输出，完整原文以RAW-MANIFEST绑定日志为准。','']
for phase in phases:
 s=json.loads((A/(phase+'.summary.json')).read_text())
 for pkg,p in s['packages'].items():
  failures=', '.join(x['test'] for x in p['failures']) or '0'
  ints=sum(x['package']==pkg for x in s['interrupted_lines'])
  rows.append(f"| {phase} | {pkg} | {p['top_pass']}/{p['top_skip']} | {p['all_pass']}/{p['all_skip']} | {failures} | {ints} |")
  group=collections.defaultdict(list)
  for x in p['skips']:group[x['test']].append(x['reasons'])
  if group:
   skips.extend(['## '+phase+' / '+pkg,''])
   for name,values in group.items():
    reasons=[]
    for v in values:
     for line in v:
      if line not in reasons:reasons.append(line)
    skips.append('- `'+name+'` ×'+str(len(values))+': '+('；'.join(reasons) or '见原日志'))
   skips.append('')
(A/'GATES-draft.md').write_text('\n'.join(rows)+'\n')
(A/'SKIPS-draft.md').write_text('\n'.join(skips)+'\n')
print('gate tables rendered')
