#!/bin/sh
# 架构设计的引用核验 —— 按【来源类型】解析，不再混用裸 §N。
#
# 立此版本的直接原因（甲域 sol 必改 2，2026-08-24）：上一版有两个洞，且它报「歧义 0」，
#   洞 1：`list(secs).count(r)>1` —— secs 是 set，count 恒 ≤1，**该判据永远为真，是空结论**。
#   洞 2：本件 §0–§13 与 SpecDoc §0–§13 **共用裸 §N 命名空间**，22 个目标双命中；
#         脚本把任一命中当成功，**根本判不出该引用指哪一份文档**。
# ⇒ 命名空间分开：本件自身章节写 `A§N`，SpecDoc 引用写 `§N`；校验器按来源唯一解析。
# ⚠️ 这是「闸报了通过但它判不了」的第四次 —— 前三次见 lint-rule-must-have-teeth。
set -eu
A=${1:-docs/dingwei-architecture-design-v0.1.md}
S=${2:-docs/dingwei-platform-specdoc-v2.0.md}
if [ "${1:-}" = "--selftest" ]; then
  A=docs/dingwei-architecture-design-v0.1.md; S=docs/dingwei-platform-specdoc-v2.0.md
  # selftest 定义在文件末尾，先读完再调用
  eval "$(sed -n '/^selftest() {/,/^}/p' "$0")"
  selftest "$A" "$S"; exit $?
fi
python3 - "$A" "$S" <<'PY'
import re,sys
a=open(sys.argv[1],encoding='utf-8').read(); sp=open(sys.argv[2],encoding='utf-8').read()
# heading 保留【列表】而非 set —— 只有列表才测得出真实重复
sp_list=[m.group(1) for m in re.finditer(r'^#{2,4}\s+(?:[🔴⚠️✅⛔]\s*)*(?:§\s*)?(\d+(?:\.\d+[a-z]?)*)\s',sp,re.M)]
a_list=[m.group(1) for m in re.finditer(r'^#{2,4}\s+A§(\d+(?:\.\d+[a-z]?)*)\s',a,re.M)]
def dup(lst): 
    seen=set(); d=[]
    for x in lst:
        if x in seen: d.append(x)
        seen.add(x)
    return sorted(set(d))
sp_dup, a_dup = dup(sp_list), dup(a_list)
sp_set, a_set = set(sp_list), set(a_list)

body=[l for l in a.split('\n') if not re.match(r'^#{2,4}\s',l)]
txt='\n'.join(body)
own_refs=re.findall(r'A§(\d+(?:\.\d+[a-z]?)*)', txt)
spec_refs=re.findall(r'(?<!A)§(\d+(?:\.\d+[a-z]?)*)', txt)
own_bad=sorted({r for r in own_refs if r not in a_set})
spec_bad=sorted({r for r in spec_refs if r not in sp_set})

print(f"本件内引用 A§ : {len(own_refs)} 次 / {len(set(own_refs))} 目标 · 悬空 {len(own_bad)}{'：'+','.join(own_bad) if own_bad else ''}")
print(f"SpecDoc 引用 § : {len(spec_refs)} 次 / {len(set(spec_refs))} 目标 · 悬空 {len(spec_bad)}{'：'+','.join(spec_bad) if spec_bad else ''}")
print(f"heading 真实重复：本件 {len(a_dup)}{'：'+','.join(a_dup) if a_dup else ''} · SpecDoc {len(sp_dup)}{'：'+','.join(sp_dup) if sp_dup else ''}")
# ⚠️ sp_dup 必须在内(甲域最终复核):上一版算了它、打印了它,却没接进失败条件 ——
# 输出显示「SpecDoc 1:1」而同一行下面写「引用核验通过」、退出码 0。
# 这是「检测到了但没接进失败条件」,与「定义了没装上」同族,差在【打印了结果却放行】,更难察觉。
bad = own_bad or spec_bad or a_dup or sp_dup
print("--- " + ("引用核验通过" if not bad else "引用核验未过"))
sys.exit(1 if bad else 0)
PY

# ---- 反测（--selftest）：四个变异必须全部被拦，且退出码为 1 ----
# 第 4 个（SpecDoc heading 重复）是甲域最终复核抓出的漏项：
# 上一版算了、打印了，却没接进失败条件 —— 而本席原来的三个反测【没有一个测 SpecDoc 侧】，
# 漏洞正好落在反测的盲区里。⇒ 反测不覆盖的地方，就是闸的实际边界。
selftest() {
  A=$1; S=$2; T=$(mktemp -d); fail=0
  run() { # name, arch, spec
    scripts/arch-refcheck.sh "$2" "$3" >/dev/null 2>&1 && { echo "  ✗ 反测未被拦：$1"; fail=1; } || echo "  ✓ 拦住：$1"; }
  cp "$A" "$T/a.md"; printf '\n见 §99.9\n' >> "$T/a.md"; run "SpecDoc 悬空 §99.9" "$T/a.md" "$S"
  cp "$A" "$T/b.md"; printf '\n见 A§77.7\n' >> "$T/b.md"; run "本件悬空 A§77.7" "$T/b.md" "$S"
  cp "$A" "$T/c.md"; printf '\n## A§6 重复章节号\n\n正文\n' >> "$T/c.md"; run "本件 heading 重复" "$T/c.md" "$S"
  cp "$S" "$T/s.md"; printf '\n## §1 总纲（重复）\n\n正文\n' >> "$T/s.md"; run "SpecDoc heading 重复" "$A" "$T/s.md"
  rm -rf "$T"
  [ $fail -eq 0 ] && echo "--- 反测四项全过（闸有牙）" || echo "--- 反测未过"
  return $fail
}
