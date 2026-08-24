#!/bin/sh
# 架构设计的 § 引用实算 —— 计数由脚本产出，不写进正文（同 E.3 #36）。
# 立此脚本的直接原因：正文曾手写「53 个 § 引用」，内容改过之后该数就不对了，
# 被甲域实算打脸（实为 94 次 / 50 唯一目标）。⇒ 会变的数不进正文。
set -eu
A=${1:-docs/dingwei-architecture-design-v0.1.md}
S=${2:-docs/dingwei-platform-specdoc-v2.0.md}
python3 - "$A" "$S" <<'PY'
import re,sys
a=open(sys.argv[1],encoding='utf-8').read(); sp=open(sys.argv[2],encoding='utf-8').read()
secs={m.group(1) for m in re.finditer(r'^#{2,4}\s+(?:[🔴⚠️✅⛔]\s*)*(?:§\s*)?(\d+(?:\.\d+[a-z]?)*)\s',sp,re.M)}
own={m.group(1) for m in re.finditer(r'^#{2,4}\s+§?(\d+(?:\.\d+[a-z]?)*)\s',a,re.M)}
hits=re.findall(r'§(\d+(?:\.\d+[a-z]?)*)',a)
uniq=sorted(set(hits))
lines={i for i,l in enumerate(a.split('\n'),1) if '§' in l}
dang=[r for r in uniq if r not in secs and r not in own]
multi=[r for r in uniq if list(secs).count(r)>1]
print(f"引用次数 {len(hits)} · 唯一目标 {len(uniq)} · 含引用的行 {len(lines)}")
print(f"悬空 {len(dang)}{'：'+','.join(dang) if dang else ''} · 歧义 {len(multi)}")
sys.exit(1 if dang or multi else 0)
PY
