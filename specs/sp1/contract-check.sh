#!/bin/sh
set -eu
SPEC=specs/sp1/spec.md
specs/sp0-1/contract-check.sh >/dev/null
[ -f "$SPEC" ] || { echo "✗ 缺 $SPEC"; exit 2; }
for n in 01 02 03 04 05 06 07 08 09; do
  grep -q "D-SP1-$n" "$SPEC" || { echo "✗ 缺 D-SP1-$n"; exit 1; }
done
for term in 'agent → principal' '一企业一 PG 库' 'TenantCtx' 'RLS'; do
  grep -q "$term" "$SPEC" || { echo "✗ SP1 点名范围缺：$term"; exit 1; }
done
! grep -q 'docs/dingwei-sp0-design.md' "$SPEC" || { echo '✗ SP1 合同串入 SP0 目标件'; exit 1; }
DOC=docs/dingwei-sp1-design.md
for term in 'dw_owner' 'dw_app' 'dw_migrate' 'database_key' '0 或多结果即拒' '连接池按 database key 隔离' 'D-SP1-09'; do
  grep -q "$term" "$DOC" || { echo "✗ SP1 可施工合同缺：$term"; exit 1; }
done
for term in 'type AccessCtx struct' 'org       string' 'principal string' 'func NewAccessCtx(orgID, principalID string)'; do
  grep -q "$term" internal/repo/tenant.go || { echo "✗ AccessCtx 双轴代码合同缺：$term"; exit 1; }
done
_legacy=$(go run ./specs/tools/legacy-name internal cmd 2>/dev/null) || { echo '✗ 旧名计数器不可运行'; exit 2; }
printf '%s\n' "$_legacy" | grep -qx 'prod=0' || { echo '✗ 生产旧名未归零'; exit 1; }
printf '%s\n' "$_legacy" | grep -qx 'test=0' || { echo '✗ 测试旧名未归零'; exit 1; }
printf '%s\n' "$_legacy" | grep -qx 'comment=0' || { echo '✗ 注释旧名未归零'; exit 1; }
echo '预期集合：SP1 六面 + D-SP1-01..09 + 四项点名范围 + 三阻断施工合同 + 独立目标件'
echo '实检集合：6/6 + 9/9 + 4/4 + 三role/registry/路由 7/7 + AccessCtx 双轴 4/4 + 旧名 0/0/0'
echo '未知集合：0'
echo '✓ SP1 独立合同通过'
