#!/bin/sh
set -eu
ANCHOR=46b79ab0022b0686a23455f328c9316d7f87281f
for item in \
 'docs/dingwei-platform-specdoc.md:8b0b20b97af85bf1c42c1b8dd5a7c94a0e73d1de3100666c5acb2370ae56c61e' \
 'docs/dingwei-platform-architecture.md:0a2288c7b9e245eded06944cd063a6d0d458c6b1f45a3a9534d29817b6c58fdc' \
 'docs/dingwei-prototype.html:eeb71a5a77a53990569e08f9745165903bcf9e34cf6ce0a5047824167e1df4b8'; do
 f=${item%%:*}; want=${item#*:}; got=$(git show "$ANCHOR:$f" | sha256sum | awk '{print $1}')
 [ "$got" = "$want" ] || { echo "✗ 冻结输入漂移：$f"; exit 1; }
done
[ -f specs/sp0/spec.md ] && [ -f specs/sp1/spec.md ] || { echo '✗ 两个独立合同缺件'; exit 2; }
grep -q 'docs/dingwei-sp0-design.md' specs/sp0/spec.md
grep -q 'docs/dingwei-sp1-design.md' specs/sp1/spec.md
! grep -q 'docs/dingwei-sp1-design.md' specs/sp0/spec.md
! grep -q 'docs/dingwei-sp0-design.md' specs/sp1/spec.md
for n in 01 02 03 04 05 06 07 08 09 10 11; do grep -q "D-SP0-$n" specs/sp0/spec.md || { echo "✗ 缺 D-SP0-$n"; exit 1; }; done
for n in 01 02 03 04 05 06 07 08; do grep -q "D-SP1-$n" specs/sp1/spec.md || { echo "✗ 缺 D-SP1-$n"; exit 1; }; done
echo '预期集合：SP0 合同/目标/11 交付 + SP1 合同/目标/8 交付'
echo '实检集合：SP0 11/11 · SP1 8/8 · 目标路径互异'
echo '未知集合：0'
echo '✓ SP0 / SP1 独立并行合同与冻结输入通过'
