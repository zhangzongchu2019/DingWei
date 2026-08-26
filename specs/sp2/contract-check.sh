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
for id in CreateProject CreateTaskPlan DispatchNode RecordExecution RecordGateVerdict AppendRevision DeployRelease TaskSnapshot; do
 grep -q "WRK-.*-$id" "$SPEC" || { echo "✗ 缺 WRK 接口：$id"; exit 1; }
done
grep -q '最多 4 轮' "$SPEC" || { echo '✗ 未落实业主 4 轮上限'; exit 1; }
! grep -q '最多 6 轮' "$SPEC" || { echo '✗ 残留旧 6 轮口径'; exit 1; }
for reviewer in zzc-opus50tchangshark-2642 zzc-deepseekv4pro08130ca16c13-2642 zzc-opus50tczc2000-2642; do
 grep -q "$reviewer" "$SPEC" || { echo "✗ 缺评审票：$reviewer"; exit 1; }
done
grep -q '3/3' "$SPEC" && grep -q 'AC-E05' "$SPEC" || { echo '✗ 三票门未落 AC-E05'; exit 1; }
for dep in SP0 SP1 SP7a SP7b; do grep -q "| \*\*$dep" "$SPEC" || { echo "✗ 前置处置表缺 $dep"; exit 1; }; done

# 冻结锚派生范围分母；与合同参考数不符即输入漂移，不得继续按旧分母评审。
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
  c=[re.sub(r"[*`]","",x.strip()) for x in l.strip("|").split("|")]
  if c and re.fullmatch(r"\d+[a-z]?",c[0]) and "SP2" in c[-1].split("·"): e[sec]+=1
print(h,e["E.1"],e["E.3"],e["E.4"])
')
[ "$1:$2:$3:$4" = '29:16:3:11' ] || { echo "✗ 冻结范围漂移：headings/E.1/E.3/E.4=$1/$2/$3/$4"; exit 1; }
echo '预期集合：六面 + D-SP2-01..12 + AC-SP2-01..12 + WRK接口8 + 四轮 + 三票 + 前置4 + heading/E分母'
echo '实检集合：6/6 + 12/12 + 12/12 + 8/8 + 1/1 + 3/3 + 4/4 + 29/(16+3+11)'
echo '未知集合：0'
echo '✓ SP2 设计合同与冻结输入通过'
