#!/bin/sh
set -eu
ANCHOR=46b79ab0022b0686a23455f328c9316d7f87281f
DOC=docs/dingwei-sp1-design.md
python3 - "$ANCHOR" "$DOC" <<'PY'
import re,subprocess,sys
a,doc=sys.argv[1:]; text=open(doc,encoding='utf8').read()
spec=subprocess.check_output(['git','show',f'{a}:docs/dingwei-platform-specdoc.md'],text=True)
proto=subprocess.check_output(['git','show',f'{a}:docs/dingwei-prototype.html'],text=True)

# SP1 权威 headings：§7.1 及 §21、§22.1、§22.3、§22.4 子树
expected=['§7.1']
for l in spec.splitlines():
    m=re.match(r'^#{2,4}\s+(?:§)?(21(?:\.\d+(?:[-a-z0-9]*)?)?|22\.(?:1|3|4)(?:[a-z0-9.-]*)?)\b',l)
    if m: expected.append('§'+m.group(1))
actual=set(re.findall(r'§\d+(?:\.\d+(?:[-a-z0-9]*)?)*',text))
unknown=sorted(set(expected)-actual)

e=[]; sec=''
for l in spec.splitlines():
    m=re.match(r'^##\s+(E\.[134])\b',l)
    if m: sec=m.group(1)
    elif re.match(r'^##\s+E\.',l): sec=''
    if sec and l.startswith('|'):
        c=[re.sub(r'[*`]','',x.strip()) for x in l.strip('|').split('|')]
        if c and re.fullmatch(r'\d+[a-z]?',c[0]) and 'SP1' in c[-1].split('·'): e.append(f'{sec}#{c[0]}')
e_unknown=sorted(set(e)-set(re.findall(r'E\.[134]#\d+[a-z]?',text)))

labels=('受控仓储 API 核验','身份与渠道绑定','Agent 列表','配置新 Agent','Agent 管理')
p=[]
for href,label in re.findall(r'<a href="#([^"]+)"[^>]*>(.*?)</a>',proto,re.S):
    plain=re.sub('<[^>]+>','',label)
    if any(x in plain for x in labels): p.append('#'+href)
p_unknown=sorted(set(p)-set(re.findall(r'#[a-z]\w*',text)))

ids=re.findall(r'^## (D-SP1-\d+)',text,re.M)
id_unknown=[f'D-SP1-{i:02d}' for i in range(1,9) if ids.count(f'D-SP1-{i:02d}')!=1]
required=('agent → principal','AccessCtx','一企业一 PG 库','FORCE ROW LEVEL SECURITY','SET LOCAL','TenantCtx','SP7a 五列转交')
r_unknown=[x for x in required if x not in text]
print(f'[spec] 预期={len(set(expected))} 实检={len(set(expected))-len(unknown)} 未知={len(unknown)} {unknown}')
print(f'[E] 预期={len(set(e))} 实检={len(set(e))-len(e_unknown)} 未知={len(e_unknown)} {e_unknown}')
print(f'[prototype] 预期={len(set(p))} 实检={len(set(p))-len(p_unknown)} 未知={len(p_unknown)} {p_unknown}')
print(f'[delivery] 预期=8 实检={8-len(id_unknown)} 未知={len(id_unknown)} {id_unknown}')
print(f'[contract] 预期={len(required)} 实检={len(required)-len(r_unknown)} 未知={len(r_unknown)} {r_unknown}')
sys.exit(1 if unknown or e_unknown or p_unknown or id_unknown or r_unknown else 0)
PY
