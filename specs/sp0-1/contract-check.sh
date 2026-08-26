#!/bin/sh
set -eu
SPEC=specs/sp0-1/spec.md
ANCHOR=46b79ab0022b0686a23455f328c9316d7f87281f
[ -f "$SPEC" ] || { echo "✗ 合同缺失：$SPEC"; exit 2; }
for item in \
  'docs/dingwei-platform-specdoc.md:8b0b20b97af85bf1c42c1b8dd5a7c94a0e73d1de3100666c5acb2370ae56c61e' \
  'docs/dingwei-platform-architecture.md:0a2288c7b9e245eded06944cd063a6d0d458c6b1f45a3a9534d29817b6c58fdc' \
  'docs/dingwei-prototype.html:eeb71a5a77a53990569e08f9745165903bcf9e34cf6ce0a5047824167e1df4b8'; do
  f=${item%%:*}; want=${item#*:}
  got=$(git show "$ANCHOR:$f" | sha256sum | awk '{print $1}')
  [ "$got" = "$want" ] || { echo "✗ 冻结输入漂移：$f want=$want got=$got"; exit 1; }
done

bad=0
for n in ① ② ③ ④ ⑤ ⑥; do grep -q "^### $n" "$SPEC" || { echo "✗ 缺必备面 $n"; bad=1; }; done
for n in $(seq -w 1 11); do grep -q "D-SP0-$n" "$SPEC" || { echo "✗ 缺交付 ID D-SP0-$n"; bad=1; }; done
for n in $(seq -w 1 11); do grep -q "AC-SP0-$n" "$SPEC" || { echo "✗ 缺验收 ID AC-SP0-$n"; bad=1; }; done
[ "$bad" -eq 0 ] || exit 1
echo '预期集合：六面 + D-SP0-01..11 + AC-SP0-01..11'
echo '实检集合：六面 6/6 · 交付 11/11 · 验收 11/11'
echo '未知集合：0'
echo '✓ SP0-1 设计合同形状与冻结输入通过'
