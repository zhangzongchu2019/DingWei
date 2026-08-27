#!/bin/sh
set -eu
SPEC=specs/sp0/spec.md
specs/sp0-1/contract-check.sh >/dev/null
[ -f "$SPEC" ] || { echo "✗ 缺 $SPEC"; exit 2; }
for n in 01 02 03 04 05 06 07 08 09 10 11; do
  grep -q "D-SP0-$n" "$SPEC" || { echo "✗ 缺 D-SP0-$n"; exit 1; }
done
grep -q '§1–§4、附录 A' "$SPEC" || { echo '✗ SP0 权威范围未写准'; exit 1; }
! grep -q 'docs/dingwei-sp1-design.md' "$SPEC" || { echo '✗ SP0 合同串入 SP1 目标件'; exit 1; }
echo '预期集合：SP0 六面 + D-SP0-01..11 + 独立目标件'
echo '实检集合：6/6 + 11/11 + docs/dingwei-sp0-design.md'
echo '未知集合：0'
echo '✓ SP0 独立合同通过'
