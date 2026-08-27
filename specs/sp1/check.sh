#!/bin/sh
set -eu
ANCHOR=${ANCHOR:-acc531e55dce80e8c0e11db9abb4a5b6e86fb08d}
DOC=${DOC:-docs/dingwei-sp1-design.md}

check() {
python3 - "$ANCHOR" "$DOC" <<'PY'
import re,subprocess,sys
a,doc=sys.argv[1:]; text=open(doc,encoding='utf8').read()
contract=open('specs/sp1/spec.md',encoding='utf8').read()
spec=subprocess.check_output(['git','show',f'{a}:docs/dingwei-platform-specdoc.md'],text=True)
proto=subprocess.check_output(['git','show',f'{a}:docs/dingwei-prototype.html'],text=True)
m=re.search(r'<!-- COVERAGE-SP1-BEGIN -->(.*?)<!-- COVERAGE-SP1-END -->',text,re.S)
if not m: print('✗ SP1 结构化覆盖块缺失'); sys.exit(2)
cov=m.group(1)
def mapped(pattern):
    out=set()
    for l in cov.splitlines():
        if not l.startswith('|'): continue
        c=[x.strip() for x in l.strip('|').split('|')]
        token=c[0].lstrip('`').split('`',1)[0]
        if len(c)>=2 and 'D-SP1-' in c[1] and re.fullmatch(pattern,token): out.add(token)
    return out

authority=re.search(r'^\| \*\*权威章节\*\* \|(.+?)\|$',contract,re.M)
if not authority: print('✗ SP1 合同缺权威章节结构化行'); sys.exit(2)
expected=re.findall(r'§\d+(?:\.\d+(?:[-a-z0-9]*)?)*',authority.group(1))
for l in spec.splitlines():
    q=re.match(r'^#{2,4}\s+(?:§)?(21(?:\.\d+(?:[-a-z0-9]*)?)?|22\.(?:1|3|4)(?:[a-z0-9.-]*)?)\b',l)
    if q: expected.append('§'+q.group(1))
actual=mapped(r'§\d+(?:\.\d+(?:[-a-z0-9]*)?)*'); unknown=sorted(set(expected)-actual)

e=[]; sec=''
for l in spec.splitlines():
    q=re.match(r'^##\s+(E\.[134])\b',l)
    if q: sec=q.group(1)
    elif re.match(r'^##\s+E\.',l): sec=''
    if sec and l.startswith('|'):
        c=[re.sub(r'[*`]','',x.strip()) for x in l.strip('|').split('|')]
        if c and re.fullmatch(r'\d+[a-z]?',c[0]) and 'SP1' in c[-1].split('·'): e.append(f'{sec}#{c[0]}')
e_actual=mapped(r'E\.[134]#\d+[a-z]?'); e_unknown=sorted(set(e)-e_actual)

labels=('受控仓储 API 核验','身份与渠道绑定','Agent 列表','配置新 Agent','Agent 管理')
p=[]
for href,label in re.findall(r'<a href="#([^"]+)"[^>]*>(.*?)</a>',proto,re.S):
    if any(x in re.sub('<[^>]+>','',label) for x in labels): p.append('#'+href)
p_actual=mapped(r'#[a-z]\w*'); p_unknown=sorted(set(p)-p_actual)
ids=re.findall(r'^## (D-SP1-\d+)',text,re.M); id_unknown=[f'D-SP1-{i:02d}' for i in range(1,10) if ids.count(f'D-SP1-{i:02d}')!=1]
required=('agent → principal','AccessCtx','一企业一 PG 库','FORCE ROW LEVEL SECURITY','SET LOCAL','TenantCtx','SP7a 五列转交')
r_unknown=[x for x in required if x not in text]
print(f'[spec] 预期={len(set(expected))} 实检={len(actual & set(expected))} 未知={len(unknown)} {unknown}')
print(f'[E] 预期={len(set(e))} 实检={len(e_actual & set(e))} 未知={len(e_unknown)} {e_unknown}')
print(f'[prototype] 预期={len(set(p))} 实检={len(p_actual & set(p))} 未知={len(p_unknown)} {p_unknown}')
print(f'[delivery] 预期=9 实检={9-len(id_unknown)} 未知={len(id_unknown)} {id_unknown}')
print(f'[contract] 预期={len(required)} 实检={len(required)-len(r_unknown)} 未知={len(r_unknown)} {r_unknown}')
sys.exit(1 if unknown or e_unknown or p_unknown or id_unknown or r_unknown else 0)
PY
}

selftest() {
  T=$(mktemp -d); trap 'rm -rf "$T"' EXIT; fail=0; n=0
  run_bad(){ n=$((n+1)); if DOC="$2" check >/dev/null 2>&1; then echo "  ✗ 未拦：$1"; fail=1; else echo "  ✓ 拦住：$1"; fi; }
  awk '!/^\| `§21\.1a` \|/' "$DOC" > "$T/missing.md"
  run_bad '删除结构化真落点 §21.1a' "$T/missing.md"
  cp "$T/missing.md" "$T/prose.md"; printf '\n散文提及 §21.1a 已考虑。\n' >> "$T/prose.md"
  run_bad '块外散文提及不得补回覆盖' "$T/prose.md"
  n=$((n+1)); if check >/dev/null 2>&1; then echo '  ✓ 正常件放行'; else echo '  ✗ 正常件被拦'; fail=1; fi
  [ "$fail" -eq 0 ] && echo "--- SP1 覆盖反测 $n 项全过" || echo '--- SP1 覆盖反测失败'
  return "$fail"
}
case "${1:-}" in --selftest) selftest;; *) check;; esac
