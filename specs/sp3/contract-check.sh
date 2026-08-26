#!/bin/sh
set -eu
ANCHOR=53e4a2aed1569eef91e4048e7da2a1cd32db3ec2
SPEC=specs/sp3/spec.md
for item in \
 'docs/dingwei-platform-specdoc.md:8b0b20b97af85bf1c42c1b8dd5a7c94a0e73d1de3100666c5acb2370ae56c61e' \
 'docs/dingwei-platform-architecture.md:0a2288c7b9e245eded06944cd063a6d0d458c6b1f45a3a9534d29817b6c58fdc' \
 'docs/dingwei-prototype.html:eeb71a5a77a53990569e08f9745165903bcf9e34cf6ce0a5047824167e1df4b8' \
 'docs/dingwei-sp0-design.md:f635b96e6c3574319d2e8df7e6a1c41e9693a66d7581504df60a6801531b8bec' \
 'docs/dingwei-sp1-design.md:8fafe6c5bac5595eaa771543dfc4199ba3670081786f1769f39833dc616e27bf' \
 'docs/dingwei-sp2-design.md:37d2c17cc0761914aae91abd20273746231d894c50163cb89b8123cc03d76a86'; do
 f=${item%%:*}; want=${item#*:}; got=$(git show "$ANCHOR:$f" | sha256sum | awk '{print $1}')
 [ "$got" = "$want" ] || { echo "✗ 冻结输入漂移：$f"; exit 2; }
done
[ -f "$SPEC" ] || { echo "✗ 缺 $SPEC"; exit 2; }
for face in ① ② ③ ④ ⑤ ⑥; do grep -q "^### $face" "$SPEC" || { echo "✗ 缺必备面 $face"; exit 1; }; done
for n in 01 02 03 04 05 06 07 08 09 10 11 12; do grep -q "D-SP3-$n" "$SPEC" || { echo "✗ 缺 D-SP3-$n"; exit 1; }; done
for n in 01 02 03 04 05 06 07 08 09 10 11 12; do grep -q "AC-SP3-$n" "$SPEC" || { echo "✗ 缺 AC-SP3-$n"; exit 1; }; done
for id in IngestItem Search BuildContextSnapshot MarkLifecycle; do grep -q "KNO-.*-$id" "$SPEC" || { echo "✗ 缺 KNO 接口 $id"; exit 1; }; done
grep -q '<!-- EVENT-SET-SP3: NOT_APPLICABLE -->' "$SPEC" || { echo '✗ KNO event 空面未机器声明'; exit 2; }
EVENT_N=$(git show "$ANCHOR:docs/dingwei-platform-architecture.md" | grep -Ec '^\| `EV-KNO-[^`]+` \| KNO \|' || true)
[ "$EVENT_N" -eq 0 ] || { echo "✗ KNO event 空面与架构实数冲突：$EVENT_N"; exit 2; }
for dep in SP1 SP7a SP7b SP2; do grep -q "| \*\*$dep" "$SPEC" || { echo "✗ 前置处置缺 $dep"; exit 1; }; done
grep -q '§11.6 派发快照表为字段全集唯一权威源' "$SPEC" || { echo '✗ snapshot 字段权威源未钉为 §11.6'; exit 1; }
grep -q 'SpecDoc §11.6 与 SP2 §3.3 snapshot 字段不一致' "$SPEC" || { echo '✗ snapshot 字段冲突未登记'; exit 1; }
ACTIVE=$(sed -n '/^### 4\.1 评审协议/,/^## 5\./p' "$SPEC")
printf '%s\n' "$ACTIVE" | grep -q '最多 4 轮' || { echo '✗ 生效协议缺 4 轮上限'; exit 1; }
! printf '%s\n' "$ACTIVE" | grep -q '6 轮' || { echo '✗ 生效协议残留 6 轮'; exit 1; }
for reviewer in zzc-opus50tchangshark-2642 zzc-deepseekv4pro08130ca16c13-2642 zzc-opus50tczc2000-2642; do
 grep -q "$reviewer" "$SPEC" || { echo "✗ 缺评审票 $reviewer"; exit 1; }
done
SELFTEST=$(specs/sp3/check.sh --selftest) || { echo '✗ SP3 checker 反测失败'; exit 1; }
N=$(printf '%s\n' "$SELFTEST" | sed -n 's/^--- SP3 覆盖反测 \([0-9][0-9]*\) 项全过$/\1/p')
[ -n "$N" ] || { echo '✗ SP3 checker 未报告实算反测数'; exit 2; }
COUNTS=$(printf '%s\n' "$SELFTEST" | sed -n 's/^--- SP3 预期集合实算 //p')
[ -n "$COUNTS" ] || { echo '✗ SP3 checker 未报告机器派生集合数'; exit 2; }
if DOC=docs/definitely-missing-sp3-design.md specs/sp3/check.sh >/dev/null 2>&1; then
 echo '✗ 目标件缺失被当通过'; exit 1
else rc=$?; [ "$rc" -eq 2 ] || { echo "✗ 缺目标件应 rc=2，实际 $rc"; exit 1; }; fi
echo "预期集合：$COUNTS；selftest=$N；缺件=rc2"
echo '实检集合：六面6/D12/AC12/KNO接口4/三票3/前置4'
echo '未知集合：0'
echo '✓ SP3 设计合同与冻结输入通过'
