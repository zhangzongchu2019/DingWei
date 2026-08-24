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
bad = own_bad or spec_bad or a_dup
print("--- " + ("引用核验通过" if not bad else "引用核验未过"))
sys.exit(1 if bad else 0)
PY
