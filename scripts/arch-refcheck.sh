#!/bin/sh
# 目标架构引用核验：新件自身用普通数字 heading；正文中的 §N 一律指 SpecDoc。
# 旧 A§ 命名空间只属于 docs/v0.1/dingwei-architecture-design-v0.34.md，现行件不得使用。
set -eu
A=docs/dingwei-platform-architecture.md
S=docs/dingwei-platform-specdoc.md

check() {
python3 - "$1" "$2" <<'PY'
import re,sys
a=open(sys.argv[1],encoding='utf-8').read(); sp=open(sys.argv[2],encoding='utf-8').read()
sp_list=[m.group(1) for m in re.finditer(r'^#{2,4}\s+(?:[🔴⚠️✅⛔]\s*)*(?:§\s*)?(\d+(?:\.\d+[a-z]?)*)\b',sp,re.M)]
a_list=[m.group(1) for m in re.finditer(r'^#{2,4}\s+(\d+(?:\.\d+)*)\b',a,re.M)]
def dup(xs):
    seen=set(); out=set()
    for x in xs:
        if x in seen: out.add(x)
        seen.add(x)
    return sorted(out)
sp_dup,a_dup=dup(sp_list),dup(a_list)
sp_set=set(sp_list)
body='\n'.join(l for l in a.splitlines() if not re.match(r'^#{2,4}\s',l))
refs=re.findall(r'(?<!A)§(\d+(?:\.\d+[a-z]?)*)',body)
bad_refs=sorted({x for x in refs if x not in sp_set})
legacy=sorted(set(re.findall(r'A§(\d+(?:\.\d+[a-z]?)*)',a)))
print(f'目标架构 heading：{len(a_list)} 个 · 重复 {len(a_dup)}'+(('：'+','.join(a_dup)) if a_dup else ''))
print(f'SpecDoc 引用 §：{len(refs)} 次 / {len(set(refs))} 目标 · 悬空 {len(bad_refs)}'+(('：'+','.join(bad_refs)) if bad_refs else ''))
print(f'旧 A§ 引用：{len(legacy)}'+(('：'+','.join(legacy)) if legacy else ''))
bad=a_dup or bad_refs or legacy or not a_list or not sp_list
print('--- '+('引用核验通过' if not bad else '引用核验未过'))
sys.exit(1 if bad else 0)
PY
}

selftest() {
  T=$(mktemp -d); trap 'rm -rf "$T"' EXIT; fail=0; n=0
  run_bad() { n=$((n+1)); if check "$2" "$3" >/dev/null 2>&1; then echo "  ✗ 未拦：$1"; fail=1; else echo "  ✓ 拦住：$1"; fi; }
  cp "$A" "$T/a.md"; printf '\n见 §99.9\n' >> "$T/a.md"; run_bad 'SpecDoc 悬空引用' "$T/a.md" "$S"
  cp "$A" "$T/b.md"; printf '\n见 A§7\n' >> "$T/b.md"; run_bad '旧 A§ 命名空间回流' "$T/b.md" "$S"
  cp "$A" "$T/c.md"; printf '\n## 6 重复章节\n' >> "$T/c.md"; run_bad '目标架构 heading 重复' "$T/c.md" "$S"
  n=$((n+1)); if check "$A" "$S" >/dev/null 2>&1; then echo '  ✓ 正常件放行'; else echo '  ✗ 正常件被拦'; fail=1; fi
  [ "$fail" -eq 0 ] && echo "--- 反测 $n 项全过（闸有牙）" || echo '--- 反测未过'
  return "$fail"
}

case "${1:-}" in
  --selftest) selftest ;;
  *) check "${1:-$A}" "${2:-$S}" ;;
esac
