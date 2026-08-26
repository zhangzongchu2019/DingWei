#!/bin/sh
set -eu
ANCHOR=46b79ab0022b0686a23455f328c9316d7f87281f
DOC=docs/dingwei-sp0-design.md
python3 - "$ANCHOR" "$DOC" <<'PY'
import re,subprocess,sys
a,doc=sys.argv[1:]; text=open(doc,encoding='utf8').read()
spec=subprocess.check_output(['git','show',f'{a}:docs/dingwei-platform-specdoc.md'],text=True)
proto=subprocess.check_output(['git','show',f'{a}:docs/dingwei-prototype.html'],text=True)

# §1–§4 所有 heading ＋ 附录 A
expected=[]; on=False
for l in spec.splitlines():
    if re.match(r'^## §1\b',l): on=True
    if re.match(r'^## §5\b',l): on=False
    if on:
        m=re.match(r'^#{2,4}\s+(?:§)?(\d+(?:\.\d+[a-z]?)*)\b',l)
        if m: expected.append('§'+m.group(1))
expected.append('附录 A')
actual=set(re.findall(r'§\d+(?:\.\d+[a-z]?)*|附录 A',text))
unknown=sorted(set(expected)-actual)

# E.1/E.3/E.4 owner_sp 精确为 SP0 的逐行 token
e=[]; sec=''
for l in spec.splitlines():
    m=re.match(r'^##\s+(E\.[134])\b',l)
    if m: sec=m.group(1)
    elif re.match(r'^##\s+E\.',l): sec=''
    if sec and l.startswith('|'):
        c=[re.sub(r'[*`]','',x.strip()) for x in l.strip('|').split('|')]
        if c and re.fullmatch(r'\d+[a-z]?',c[0]) and 'SP0' in c[-1].split('·'): e.append(f'{sec}#{c[0]}')
e_unknown=sorted(set(e)-set(re.findall(r'E\.[134]#\d+[a-z]?',text)))

# 原型导航中 SP0 直接入口由 label 派生
labels=('待决队列','Spec 与设计文档','spec 准入检查','交付物链路','分级对齐','架构总览')
p=[]
for href,label in re.findall(r'<a href="#([^"]+)"[^>]*>(.*?)</a>',proto,re.S):
    plain=re.sub('<[^>]+>','',label)
    if any(x in plain for x in labels): p.append('#'+href)
p_unknown=sorted(set(p)-set(re.findall(r'#[a-z]\w*',text)))

ids=re.findall(r'^## (D-SP0-\d+)',text,re.M)
id_unknown=[f'D-SP0-{i:02d}' for i in range(1,12) if ids.count(f'D-SP0-{i:02d}')!=1]
print(f'[spec] 预期={len(set(expected))} 实检={len(set(expected))-len(unknown)} 未知={len(unknown)} {unknown}')
print(f'[E] 预期={len(set(e))} 实检={len(set(e))-len(e_unknown)} 未知={len(e_unknown)} {e_unknown}')
print(f'[prototype] 预期={len(set(p))} 实检={len(set(p))-len(p_unknown)} 未知={len(p_unknown)} {p_unknown}')
print(f'[delivery] 预期=11 实检={11-len(id_unknown)} 未知={len(id_unknown)} {id_unknown}')
bad=unknown or e_unknown or p_unknown or id_unknown or '新增外部依赖：0' not in text or '目标架构接口 ID 新增：0' not in text
sys.exit(1 if bad else 0)
PY
