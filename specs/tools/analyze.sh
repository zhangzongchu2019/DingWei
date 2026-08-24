#!/bin/sh
# /speckit.analyze —— 跨制品一致性检查（SpecDoc §4.1：环节丢失的输入端检测器）
#
# 「跨制品」＝ SpecDoc（规则）· 架构设计（系统构成）· 原型（界面契约）· 代码（现状）四者之间。
# 单件自洽由各自的闸负责（speclint / arch-refcheck）；本脚本只查【件与件之间】。
#
# 设计取舍：只报【机器可判】的不一致。语义层的「引对号引错义」判不了，
# 那归人审（SpecDoc E.4 #60 lint 覆盖不到项之①）—— 本脚本不宣称能覆盖它。
set -u
SPEC=docs/dingwei-platform-specdoc.md
ARCH=docs/dingwei-architecture-design.md
PROTO=docs/dingwei-prototype.html
FAIL=0

echo "① 各件单件闸"
[ -x scripts/speclint-gate.sh ] && { python3 docs/tools/speclint.py "$SPEC" >/dev/null 2>&1 \
  && echo "  ✓ SpecDoc speclint" || { echo "  ✗ SpecDoc speclint"; FAIL=1; }; }
[ -x scripts/arch-refcheck.sh ] && { scripts/arch-refcheck.sh >/dev/null 2>&1 \
  && echo "  ✓ 架构引用核验" || { echo "  ✗ 架构引用核验"; FAIL=1; }; }

echo "② 架构设计引用的 SpecDoc 节号是否都存在（跨制品悬空）"
scripts/arch-refcheck.sh >/dev/null 2>&1 && echo "  ✓ 无悬空" || { echo "  ✗ 有悬空，详见 arch-refcheck"; FAIL=1; }

echo "③ 原型引用的 SpecDoc 节号是否都存在"
python3 - "$PROTO" "$SPEC" <<'PY'
import re,sys
proto=open(sys.argv[1],encoding='utf-8').read(); spec=open(sys.argv[2],encoding='utf-8').read()
secs={m.group(1) for m in re.finditer(r'^#{2,4}\s+(?:[🔴⚠️✅⛔]\s*)*(?:§\s*)?(\d+(?:\.\d+[a-z]?)*)\s',spec,re.M)}
refs=sorted(set(re.findall(r'§(\d+(?:\.\d+[a-z]?)*)',proto)))
dang=[r for r in refs if r not in secs]
print(f"  原型引用 {len(refs)} 个节号；悬空 {len(dang)}" + ("：" + ",".join(dang) if dang else " ✓"))
sys.exit(1 if dang else 0)
PY
[ $? -ne 0 ] && FAIL=1

echo "④ 三件的版本自述是否与 DELIVERY-STATUS 一致"
python3 - <<'PY'
import re,sys
st=open('docs/DELIVERY-STATUS.md',encoding='utf-8').read()
bad=0
for name,path,pat in [
  ('SpecDoc','dingwei-platform-specdoc.md',r'\*\*版本\*\*: (v[\d.]+)'),
  ('架构设计','dingwei-architecture-design.md',r'\*\*版本\*\*: (v[\d.]+)'),
  ('原型','dingwei-prototype.html',r'原型 (v[\d.]+)')]:
    body=open('docs/'+path,encoding='utf-8').read()
    m=re.search(pat,body); v=m.group(1) if m else None
    row=re.search(r'\|\s*`'+re.escape(path)+r'`\s*\|\s*\*\*(v[\d.]+)\*\*',st)
    sv=row.group(1) if row else None
    ok = v and sv and v==sv
    print(f"  {name}: 件内 {v} · 状态文件 {sv} {'✓' if ok else '✗'}")
    if not ok: bad=1
sys.exit(bad)
PY
[ $? -ne 0 ] && FAIL=1

echo "⑤ 生效前置状态（§0.4）"
if [ -f specs/platform/checklist.PENDING.md ]; then
  echo "  ⛔ checklist 未产出（reviewer-owned，执笔方不得代写）⇒ SpecDoc 仍为草案，不得被子 spec 引用"
else
  echo "  ✓ checklist 已就位"
fi

echo "---"
[ $FAIL -eq 0 ] && echo "跨制品一致性检查通过（不含语义层，见脚本头注）" || echo "跨制品一致性检查未过"
exit $FAIL
