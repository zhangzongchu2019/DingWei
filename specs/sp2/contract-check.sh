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
echo '预期集合：六面 + D-SP2-01..12 + AC-SP2-01..12 + WRK 接口 8 + 四轮上限'
echo '实检集合：6/6 + 12/12 + 12/12 + 8/8 + 1/1'
echo '未知集合：0'
echo '✓ SP2 设计合同与冻结输入通过'
