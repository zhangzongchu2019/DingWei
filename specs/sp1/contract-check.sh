#!/bin/sh
set -eu
SPEC=${SPEC:-specs/sp1/spec.md}
DOC=${DOC:-docs/dingwei-sp1-design.md}

check() {
specs/sp0-1/contract-check.sh >/dev/null
[ -f "$SPEC" ] || { echo "✗ 缺 $SPEC"; return 2; }
[ -f "$DOC" ] || { echo "✗ 缺 $DOC"; return 2; }
for n in 01 02 03 04 05 06 07 08 09; do
  grep -q "D-SP1-$n" "$SPEC" || { echo "✗ 缺 D-SP1-$n"; exit 1; }
done
for term in 'agent → principal' '一企业一 PG 库' 'TenantCtx' 'RLS'; do
  grep -q "$term" "$SPEC" || { echo "✗ SP1 点名范围缺：$term"; exit 1; }
done
! grep -q 'docs/dingwei-sp0-design.md' "$SPEC" || { echo '✗ SP1 合同串入 SP0 目标件'; exit 1; }
for term in 'dw_owner' 'dw_app' 'dw_migrate' '由本企业 ops 持有' '本企业 ops 与受控 migration role' 'dw_app` 永久不可切' '平台 ops 只建集群/库/三个 role' '企业 ops 无写权的审计落点' 'database_key' '0 或多结果即拒' '连接池按 database key 隔离' 'D-SP1-09'; do
  grep -q "$term" "$DOC" || { echo "✗ SP1 可施工合同缺：$term"; exit 1; }
done
! grep -q '允许在一个池内切换 DSN 冒充多企业' "$DOC" || { echo '✗ 企业专池规则被反转为允许切 DSN'; return 1; }
! grep -q '取第一条继续' "$DOC" || { echo '✗ 0/多映射拒绝规则被反转为取首条'; return 1; }
for term in 'type AccessCtx struct' 'org       string' 'principal string' 'func NewAccessCtx(orgID, principalID string)'; do
  grep -q "$term" internal/repo/tenant.go || { echo "✗ AccessCtx 双轴代码合同缺：$term"; exit 1; }
done
grep -q "pg_has_role(CURRENT_USER, 'dw_owner', 'USAGE')" specs/fixtures/rls/setup.sql || { echo '✗ 缺企业 ops 持 owner 正测'; exit 1; }
grep -q 'SET SESSION AUTHORIZATION dw_app' specs/fixtures/rls/t4-ddl-rights.sql || { echo '✗ 缺应用面不可切 owner/migrate 负测'; exit 1; }
_legacy=$(go run ./specs/tools/legacy-name internal cmd 2>/dev/null) || { echo '✗ 旧名计数器不可运行'; exit 2; }
printf '%s\n' "$_legacy" | grep -qx 'prod=0' || { echo '✗ 生产旧名未归零'; exit 1; }
printf '%s\n' "$_legacy" | grep -qx 'test=0' || { echo '✗ 测试旧名未归零'; exit 1; }
printf '%s\n' "$_legacy" | grep -qx 'comment=0' || { echo '✗ 注释旧名未归零'; exit 1; }
echo '预期集合：SP1 六面 + D-SP1-01..09 + 四项点名范围 + 三阻断施工合同 + 独立目标件'
echo '实检集合：6/6 + 9/9 + 4/4 + 三role/归属/审计/registry/路由 10/10 + AccessCtx 双轴 4/4 + 旧名 0/0/0'
echo '未知集合：0'
echo '✓ SP1 独立合同通过'
echo '⚠️ 能力边界：甲10 仅核关键字面与两条已知反转句式；改写措辞的语义反转本闸挡不住，由人审承担。'
}

selftest() {
  T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
  tested=0; fail=0
  mutate_and_reject() {
    label=$1; pattern=$2; replacement=$3
    sed "s/$pattern/$replacement/" "$DOC" > "$T/mut.md"
    grep -q "$replacement" "$T/mut.md" || { echo "  ✗ 变异未命中：$label"; fail=1; return; }
    if DOC="$T/mut.md" check >/dev/null 2>&1; then echo "  ✗ 未拦：$label"; fail=1
    else echo "  ✓ 拦住：$label"; tested=$((tested+1)); fi
  }
  mutate_and_reject '允许单池切 DSN（保留原禁令字面）' \
    '不得在一个池内切换 DSN 冒充多企业。' \
    '不得在一个池内切换 DSN 冒充多企业；允许在一个池内切换 DSN 冒充多企业（已放宽）。'
  mutate_and_reject '0/多映射取第一条继续（保留原拒绝字面）' \
    '0 或多结果即拒。' \
    '0 或多结果即拒；已改为取第一条继续。'
  if check >/dev/null 2>&1; then echo '  ✓ 正常件放行'; tested=$((tested+1)); else echo '  ✗ 正常件被拦'; fail=1; fi
  if [ "$fail" -eq 0 ]; then
    echo "--- SP1 字面/已知句式反测 $tested/3 全过"
    echo '⚠️ 能力边界：改写措辞的语义反转本闸挡不住，由人审承担。'
  fi
  return "$fail"
}

case "${1:-}" in --selftest) selftest;; *) check;; esac
