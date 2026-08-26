#!/bin/sh
set -eu
ANCHOR=53d91aedd158c0a8618f0196f013cfee5cfd44aa
SPEC=specs/sp2/spec.md
for item in \
 'docs/dingwei-platform-specdoc.md:8b0b20b97af85bf1c42c1b8dd5a7c94a0e73d1de3100666c5acb2370ae56c61e' \
 'docs/dingwei-platform-architecture.md:0a2288c7b9e245eded06944cd063a6d0d458c6b1f45a3a9534d29817b6c58fdc' \
 'docs/dingwei-prototype.html:eeb71a5a77a53990569e08f9745165903bcf9e34cf6ce0a5047824167e1df4b8' \
 'docs/dingwei-sp0-design.md:f635b96e6c3574319d2e8df7e6a1c41e9693a66d7581504df60a6801531b8bec' \
 'docs/dingwei-sp1-design.md:8fafe6c5bac5595eaa771543dfc4199ba3670081786f1769f39833dc616e27bf'; do
 f=${item%%:*}; want=${item#*:}; got=$(git show "$ANCHOR:$f" | sha256sum | awk '{print $1}')
 [ "$got" = "$want" ] || { echo "✗ 冻结输入漂移：$f"; exit 1; }
done
[ -f "$SPEC" ] || { echo "✗ 缺 $SPEC"; exit 2; }
for face in ① ② ③ ④ ⑤ ⑥; do grep -q "^### $face" "$SPEC" || { echo "✗ 缺必备面 $face"; exit 1; }; done
for n in 01 02 03 04 05 06 07 08 09 10 11 12; do grep -q "D-SP2-$n" "$SPEC" || { echo "✗ 缺 D-SP2-$n"; exit 1; }; done
for n in 01 02 03 04 05 06 07 08 09 10 11 12; do grep -q "AC-SP2-$n" "$SPEC" || { echo "✗ 缺 AC-SP2-$n"; exit 1; }; done
python3 - "$SPEC" <<'PY'
import re,sys
rows={}
text=open(sys.argv[1],encoding='utf8').read()
m=re.search(r'<!-- ACCEPTANCE-SP2-BEGIN -->(.*?)<!-- ACCEPTANCE-SP2-END -->',text,re.S)
if not m: print('✗ ACCEPTANCE-SP2 结构化块缺失'); raise SystemExit(2)
for l in m.group(1).splitlines():
    if not l.startswith('|'): continue
    c=[x.strip() for x in l.strip().strip('|').split('|')]
    if c and re.fullmatch(r'`?AC-SP2-\d{2}`?',c[0]):
        aid=c[0].strip('`')
        if len(c)!=4 or any(not x or x in ('—','见全文') for x in c[1:]):
            print('✗ AC 四列不完整：'+aid); raise SystemExit(1)
        rows[aid]=1
if len(rows)!=12: print(f'✗ AC 四列表实检 {len(rows)}/12'); raise SystemExit(1)
PY
for id in CreateProject CreateTaskPlan DispatchNode RecordExecution RecordGateVerdict AppendRevision DeployRelease TaskSnapshot; do
 grep -q "WRK-.*-$id" "$SPEC" || { echo "✗ 缺 WRK 接口：$id"; exit 1; }
done
ACTIVE_REVIEW=$(sed -n '/^### 4\.4 证据与评审/,/^## 5\./p' "$SPEC")
printf '%s\n' "$ACTIVE_REVIEW" | grep -q '最多 4 轮' || { echo '✗ 生效评审段未落实 4 轮上限'; exit 1; }
! printf '%s\n' "$ACTIVE_REVIEW" | grep -q '6 轮' || { echo '✗ 生效评审段残留 6 轮规则'; exit 1; }
for reviewer in zzc-opus50tchangshark-2642 zzc-deepseekv4pro08130ca16c13-2642 zzc-opus50tczc2000-2642; do
 grep -q "$reviewer" "$SPEC" || { echo "✗ 缺评审票：$reviewer"; exit 1; }
done
grep -q '3/3' "$SPEC" && grep -q 'AC-E05' "$SPEC" || { echo '✗ 三票门未落 AC-E05'; exit 1; }
for dep in SP0 SP1 SP7a SP7b; do grep -q "| \*\*$dep" "$SPEC" || { echo "✗ 前置处置表缺 $dep"; exit 1; }; done
! grep -Eq 'SP[01].*已通过本轮评审|已通过设计输入|上述“设计已通过”' "$SPEC" || { echo '✗ SP0/SP1 draft 被宽写为已通过'; exit 1; }
grep -q 'draft，待双评审' "$SPEC" || { echo '✗ SP0/SP1 冻结状态未按实取写明'; exit 1; }
grep -q '架构 §13.*WRK' "$SPEC" || { echo '✗ 原型入口派生源未钉为架构 §13 WRK 行'; exit 1; }
for token in 'F.1.2' 'F.1.3' 'F.2 #4' 'delivery.verify' 'A§21'; do grep -q "$token" "$SPEC" || { echo "✗ 附录 F 承接/冲突登记缺 $token"; exit 1; }; done

# 冻结锚派生范围分母；只要求各集合非空并报告实算值，不拿手写分母作真值。
set -- $(git show "$ANCHOR:docs/dingwei-platform-specdoc.md" | python3 -c '
import re,sys
ls=sys.stdin.read().splitlines(); on=False; h=0; sec=""; e={"E.1":0,"E.3":0,"E.4":0}
for l in ls:
 if re.match(r"^## §5\b",l): on=True
 if re.match(r"^## §10\b",l): on=False
 if on and re.match(r"^#{2,4} ",l): h+=1
 m=re.match(r"^##\s+(E\.[134])\b",l)
 if m: sec=m.group(1)
 elif re.match(r"^##\s+E\.",l): sec=""
 if sec and l.startswith("|"):
  c=[re.sub(r"[*`]","",x.strip()) for x in l.strip().strip("|").split("|")]
  if c and re.fullmatch(r"\d+[a-z]?",c[0]) and "SP2" in c[-1].split("·"): e[sec]+=1
print(h,e["E.1"],e["E.3"],e["E.4"])
')
for n in "$1" "$2" "$3" "$4"; do [ "$n" -gt 0 ] || { echo '✗ 冻结预期集合为空'; exit 2; }; done
UI_N=$(git show "$ANCHOR:docs/dingwei-platform-architecture.md" | python3 -c '
import re,sys
on=False; ids=set()
for l in sys.stdin:
 if re.match(r"^## 13\.",l): on=True; continue
 if on and re.match(r"^## ",l): on=False
 if not on or not l.startswith("|"): continue
 c=[x.strip() for x in l.strip().strip("|").split("|")]
 if len(c)>=2 and "WRK" in c[1].split("/"):
  for group in re.findall(r"`([^`]+)`",c[0]): ids.update(group.split("/"))
print(len(ids))
')
[ "$UI_N" -gt 0 ] || { echo '✗ 架构 §13 WRK 原型入口预期集合为空'; exit 2; }
SELFTEST_OUT=$(specs/sp2/check.sh --selftest) || { echo '✗ SP2 coverage checker 反测未过'; exit 1; }
SELFTEST_N=$(printf '%s\n' "$SELFTEST_OUT" | sed -n 's/^--- SP2 覆盖反测 \([0-9][0-9]*\) 项全过$/\1/p')
[ -n "$SELFTEST_N" ] || { echo '✗ SP2 coverage checker 未报告实算反测数'; exit 2; }
if DOC=docs/definitely-missing-sp2-design.md specs/sp2/check.sh >/dev/null 2>&1; then
 echo '✗ 目标件缺失被 coverage checker 当通过'; exit 1
else
 rc=$?; [ "$rc" -eq 2 ] || { echo "✗ 目标件缺失应 rc=2，实际 $rc"; exit 1; }
fi
echo '预期集合：六面 + D/AC各12 + WRK接口8 + 四轮 + 三票 + 前置4 + 机器派生 heading/E + coverage反测/缺件'
echo "实检集合：6/6 + 12/12 + 12/12 + 8/8 + 1/1 + 3/3 + 4/4 + headings=$1 / E=$(( $2+$3+$4 ))（$2+$3+$4）/ 架构§13-WRK入口=$UI_N + selftest=$SELFTEST_N + rc2"
echo '未知集合：0'
echo '✓ SP2 设计合同与冻结输入通过'
