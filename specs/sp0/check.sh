#!/bin/sh
set -eu
ANCHOR=${ANCHOR:-46b79ab0022b0686a23455f328c9316d7f87281f}
DOC=${DOC:-docs/dingwei-sp0-design.md}

check() {
python3 - "$ANCHOR" "$DOC" <<'PY'
import re,subprocess,sys
a,doc=sys.argv[1:]; text=open(doc,encoding='utf8').read()
spec=subprocess.check_output(['git','show',f'{a}:docs/dingwei-platform-specdoc.md'],text=True)
proto=subprocess.check_output(['git','show',f'{a}:docs/dingwei-prototype.html'],text=True)
m=re.search(r'<!-- COVERAGE-SP0-BEGIN -->(.*?)<!-- COVERAGE-SP0-END -->',text,re.S)
if not m: print('✗ SP0 结构化覆盖块缺失'); sys.exit(2)
cov=m.group(1)
def mapped(pattern):
    out=set()
    for l in cov.splitlines():
        if not l.startswith('|'): continue
        c=[x.strip() for x in l.strip('|').split('|')]
        token=c[0].lstrip('`').split('`',1)[0]
        if len(c)>=2 and 'D-SP0-' in c[1] and re.fullmatch(pattern,token): out.add(token)
    return out

expected=[]; on=False
for l in spec.splitlines():
    if re.match(r'^## §1\b',l): on=True
    if re.match(r'^## §5\b',l): on=False
    if on:
        q=re.match(r'^#{2,4}\s+(?:§)?(\d+(?:\.\d+[a-z]?)*)\b',l)
        if q: expected.append('§'+q.group(1))
expected.append('附录 A')
actual=mapped(r'§\d+(?:\.\d+[a-z]?)*|附录 A'); unknown=sorted(set(expected)-actual)

e=[]; sec=''
for l in spec.splitlines():
    q=re.match(r'^##\s+(E\.[134])\b',l)
    if q: sec=q.group(1)
    elif re.match(r'^##\s+E\.',l): sec=''
    if sec and l.startswith('|'):
        c=[re.sub(r'[*`]','',x.strip()) for x in l.strip('|').split('|')]
        if c and re.fullmatch(r'\d+[a-z]?',c[0]) and 'SP0' in c[-1].split('·'): e.append(f'{sec}#{c[0]}')
e_actual=mapped(r'E\.[134]#\d+[a-z]?'); e_unknown=sorted(set(e)-e_actual)

labels=('待决队列','Spec 与设计文档','spec 准入检查','交付物链路','分级对齐','架构总览')
p=[]
for href,label in re.findall(r'<a href="#([^"]+)"[^>]*>(.*?)</a>',proto,re.S):
    if any(x in re.sub('<[^>]+>','',label) for x in labels): p.append('#'+href)
p_actual=mapped(r'#[a-z]\w*'); p_unknown=sorted(set(p)-p_actual)
ids=re.findall(r'^## (D-SP0-\d+)',text,re.M); id_unknown=[f'D-SP0-{i:02d}' for i in range(1,12) if ids.count(f'D-SP0-{i:02d}')!=1]
print(f'[spec] 预期={len(set(expected))} 实检={len(actual & set(expected))} 未知={len(unknown)} {unknown}')
print(f'[E] 预期={len(set(e))} 实检={len(e_actual & set(e))} 未知={len(e_unknown)} {e_unknown}')
print(f'[prototype] 预期={len(set(p))} 实检={len(p_actual & set(p))} 未知={len(p_unknown)} {p_unknown}')
print(f'[delivery] 预期=11 实检={11-len(id_unknown)} 未知={len(id_unknown)} {id_unknown}')
bad=unknown or e_unknown or p_unknown or id_unknown or '新增外部依赖：0' not in text or '目标架构接口 ID 新增：0' not in text
sys.exit(1 if bad else 0)
PY
}

selftest() {
  T=$(mktemp -d); trap 'rm -rf "$T"' EXIT; fail=0; n=0
  run_bad(){ n=$((n+1)); if DOC="$2" check >/dev/null 2>&1; then echo "  ✗ 未拦：$1"; fail=1; else echo "  ✓ 拦住：$1"; fi; }
  cp "$DOC" "$T/base.md"
  awk '!/^\| `§2\.1` \|/' "$DOC" > "$T/missing.md"
  run_bad '删除结构化真落点 §2.1' "$T/missing.md"
  cp "$T/missing.md" "$T/prose.md"; printf '\n散文提及 §2.1 已考虑。\n' >> "$T/prose.md"
  run_bad '块外散文提及不得补回覆盖' "$T/prose.md"
  n=$((n+1)); if check >/dev/null 2>&1; then echo '  ✓ 正常件放行'; else echo '  ✗ 正常件被拦'; fail=1; fi
  [ "$fail" -eq 0 ] && echo "--- SP0 覆盖反测 $n 项全过" || echo '--- SP0 覆盖反测失败'
  return "$fail"
}
case "${1:-}" in --selftest) selftest;; *) check;; esac
