#!/bin/sh
# 乙组·隔离判据的【前置依赖闸】—— 领域表一旦出现租户列，§22.2a #3 四项负测必须同时就位。
#
# 立此闸的直接原因（乙域 terra 2026-08-25 裁定）：
#   terra 判：SP7a 只落 TenantCtx 类型与预留注入位，**不得判为租户隔离完成，也跑不通乙组四项** ——
#   SchemaDDL 当前无 tenant 列 ⇒ 无物理过滤谓词、无 A→B 资源可测，
#   四项（PEP 前置拒且 spy=0 / deny 非空集 / 存在性不可区分 / 最小化拒绝审计）**一项都验不了**。
#   他同时要求：SP7b 承接这些能力时，须把依赖写成**显式依赖／验收闸**，
#   「任一领域 schema 上线前四项同时 PASS」，**不是把 #3 或乙组豁免掉**。
#
# 🔴 本闸就是那个显式依赖，且是**提前布防**的形态：
#   现在租户列还不存在，四项本来就验不了，所以现在它恒过 —— 但它不是摆设：
#   **谁第一个往领域表里加 tenant 列，谁就会被它拦下**，除非四项负测同时就位。
#   ⇒ 把「将来别忘了」变成「将来做不到就过不去」。这是本轮第五次做同一个动作。
#
# ⚠️ 能力边界，写明不假装：
#   本闸判的是「四项负测是否**存在**」（按标记名），判不了「它们写得对不对」——
#   那是乙域门方的活（§22.2a 四项须人审确认语义）。存在性是机器能给的最强保证，不多宣称。
set -u

# 租户列的识别：按【效果】列举常见命名，不指望只有一种写法
TENANT_COL='tenant_id|tenant_key|tenant_uuid|tenant[[:space:]]+(TEXT|VARCHAR|UUID|INTEGER)'
# 四项负测的登记标记 —— 测试里写这些标记即视为“该项已就位”
MARKERS='YIZU_PEP_BEFORE_RESOLVE YIZU_DENY_NOT_EMPTY_SET YIZU_EXISTENCE_INDISTINGUISHABLE YIZU_MINIMAL_DENY_AUDIT'

scan_tenant() {
  grep -rnE "$TENANT_COL" --include=*.sql --include=*.go internal/ 2>/dev/null \
    | grep -v '_test\.go:' | grep -v 'internal/repo/' | grep -v 'tenant\.go:'
}

check() {
  [ -d internal ] || { echo "✗ 闸依赖缺失：internal/ 不存在"; return 2; }
  HITS=$(scan_tenant)
  N=$(if [ -z "$HITS" ]; then echo 0; else printf '%s\n' "$HITS" | wc -l | tr -d ' '; fi)

  if [ "$N" -eq 0 ]; then
    echo "  ⑩ 领域表尚无租户列（$N 处）⇒ 乙组四项本就无从验证，本闸暂过。"
    echo "     🔴 这【不是】隔离已实现：按乙域 terra 裁定，SP7a 至多只能标"
    echo "     「基座/接口准备完成（且甲组三项须另验合格）」，不得标 SP7a 总完工、不得称隔离已实现。"
    echo "     ⚠️ 谁第一个往领域表加 tenant 列，本闸就会要求四项负测同时就位。"
    return 0
  fi

  echo "  ⑩ 领域表已出现租户列 $N 处 —— 触发乙组四项负测前置："
  printf '%s\n' "$HITS" | sed 's/^/      /' | head -10
  missing=''
  for m in $MARKERS; do
    grep -rqs "$m" --include=*_test.go internal/ || missing="$missing $m"
  done
  if [ -n "$missing" ]; then
    echo "✗ 租户列已落地，但下列乙组负测尚未就位（§22.2a #3 四项须【同时】验）："
    for m in $missing; do echo "    缺：$m"; done
    echo "  🔴 四项须同时满足，缺一即不得判隔离成立 —— 尤其 PEP 前置拒与 deny 语义【须分开验】，"
    echo "     只验后者会让「先查了再拒」通过验收（查询已落到库上，跨租户读实际发生过）。"
    return 1
  fi
  echo "  ⑩ 乙组四项负测均已就位 ✓（存在性由机器判；语义正确性归乙域人审，本闸不代判）"
  return 0
}

selftest() {
  T=$(mktemp -d); n=0; fail=0
  ok(){ n=$((n+1)); echo "  ✓ $1"; }; bad(){ echo "  ✗ 反测未过：$1"; fail=1; }

  if check >/dev/null 2>&1; then ok "现状（无租户列）暂过，且不误报" ; else bad "现状被误判"; fi

  # 注入一个租户列 -> 四项负测不存在 -> 必拦
  mkdir -p internal/.tenantgate-selftest
  printf 'CREATE TABLE demo(tenant_id TEXT NOT NULL);\n' > internal/.tenantgate-selftest/x.sql
  if check >/dev/null 2>&1; then bad "加了租户列却未要求四项负测"; else ok "拦住：租户列落地而乙组四项未就位"; fi

  # 四项就位 -> 放行（否则修法把正常路径堵死）
  printf 'package t\n// YIZU_PEP_BEFORE_RESOLVE YIZU_DENY_NOT_EMPTY_SET\n// YIZU_EXISTENCE_INDISTINGUISHABLE YIZU_MINIMAL_DENY_AUDIT\n' \
    > internal/.tenantgate-selftest/x_test.go
  if check >/dev/null 2>&1; then ok "不误伤：四项标记就位后放行"; else bad "四项就位仍被拒"; fi

  # 只就位三项 -> 仍须拦（四项须【同时】）
  printf 'package t\n// YIZU_PEP_BEFORE_RESOLVE YIZU_DENY_NOT_EMPTY_SET YIZU_EXISTENCE_INDISTINGUISHABLE\n' \
    > internal/.tenantgate-selftest/x_test.go
  if check >/dev/null 2>&1; then bad "只就位三项也放行了 —— 四项须同时"; else ok "拦住：四项缺一（同时性有牙）"; fi

  rm -rf internal/.tenantgate-selftest "$T"
  [ $fail -eq 0 ] && echo "--- 反测 $n 项全过（闸有牙）" || echo "--- 反测未过"
  return $fail
}

case "${1:-}" in --selftest) selftest; exit $? ;; esac
check; exit $?
