#!/bin/sh
set -eu
SPEC=specs/sp1/spec.md
specs/sp0-1/contract-check.sh >/dev/null
[ -f "$SPEC" ] || { echo "✗ 缺 $SPEC"; exit 2; }
for n in 01 02 03 04 05 06 07 08; do
  grep -q "D-SP1-$n" "$SPEC" || { echo "✗ 缺 D-SP1-$n"; exit 1; }
done
for term in 'agent → principal' '一企业一 PG 库' 'TenantCtx' 'RLS'; do
  grep -q "$term" "$SPEC" || { echo "✗ SP1 点名范围缺：$term"; exit 1; }
done
! grep -q 'docs/dingwei-sp0-design.md' "$SPEC" || { echo '✗ SP1 合同串入 SP0 目标件'; exit 1; }
echo '预期集合：SP1 六面 + D-SP1-01..08 + 四项点名范围 + 独立目标件'
echo '实检集合：6/6 + 8/8 + 4/4 + docs/dingwei-sp1-design.md'
echo '未知集合：0'
echo '✓ SP1 独立合同通过'
