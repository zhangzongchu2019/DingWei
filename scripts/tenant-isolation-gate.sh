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
#
# 🔴 二轮（乙域 terra SA-14）：一轮的四个标记是**全局**的，与「哪个领域出现了租户列」不绑定。
#   后果：datad 把四个标记挂上之后，**fanout 或任何新领域再加 tenant_id 可以直接过闸**，
#   而该领域一条负测都没有。断言是「任一领域 schema 上线前四项同时 PASS」，
#   实现却是「全仓存在过这四个字符串即可」—— 又一次**断言作用域大于实际覆盖**。
#   ⇒ 标记改为按领域具名：`YIZU_PEP_BEFORE_RESOLVE(<domain>)`，逐领域各验各的。
#
# 🔴 二轮另一条：一轮 scan_tenant 里有 `grep -v internal/repo/`。而按 §22.2c #3，
#   租户过滤谓词**本来就该落在仓储包里**（调用方不得自带 WHERE）——
#   把仓储包排除掉，等于把**实际实现过滤的那个位置**排除在触发条件之外。已删除该排除。
set -u

# 租户列的识别：按【效果】列举常见命名，不指望只有一种写法
TENANT_COL='tenant_id|tenant_key|tenant_uuid|tenant[[:space:]]+(TEXT|VARCHAR|UUID|INTEGER)'
# 四项负测的登记标记 —— 测试里须写成 `标记(<domain>)`，与出现租户列的领域绑定。
# 🔴 不再接受裸标记：裸标记只能证明「全仓有人写过这四个字符串」，
#    证不了「这个领域有这四项负测」。
MARKERS='YIZU_PEP_BEFORE_RESOLVE YIZU_DENY_NOT_EMPTY_SET YIZU_EXISTENCE_INDISTINGUISHABLE YIZU_MINIMAL_DENY_AUDIT'

scan_tenant() {
  # 🔴 不排除 internal/repo/：过滤谓词按 §22.2c #3 本就该在仓储包里，
  #    排除它等于把实际实现位置排除在触发条件之外（SA-14）。
  #    只排除 TenantCtx 的类型定义件本身（那是类型，不是领域表的租户列）。
  grep -rnE "$TENANT_COL" --include=*.sql --include=*.go internal/ 2>/dev/null \
    | grep -v '_test\.go:' | grep -v 'internal/repo/tenant\.go:'
}

# domain_of 从命中路径推领域：internal/<domain>/... -> <domain>
domain_of() {
  printf '%s\n' "$1" | sed -n 's|^.*internal/\([^/]*\)/.*$|\1|p'
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

  echo "  ⑩ 领域表已出现租户列 $N 处 —— 触发乙组四项负测前置（逐领域验）："
  printf '%s\n' "$HITS" | sed 's/^/      /' | head -10

  DOMAINS=$(printf '%s\n' "$HITS" | while IFS= read -r h; do domain_of "$h"; done | sort -u)
  if [ -z "$DOMAINS" ]; then
    echo "✗ 有租户列命中却推不出领域（路径不是 internal/<domain>/...）—— 判不出归属即 fail-closed"
    return 2
  fi

  rc=0
  for d in $DOMAINS; do
    missing=''
    for m in $MARKERS; do
      # 🔴 标记必须带领域：`YIZU_...(datad)`。裸标记不算数。
      grep -rqsF "$m($d)" --include=*_test.go internal/ || missing="$missing $m"
    done
    if [ -n "$missing" ]; then
      echo "✗ 领域 $d 已有租户列，但其乙组负测尚未就位（§22.2a #3 四项须【同时】验）："
      for m in $missing; do echo "    缺：$m($d)"; done
      rc=1
    else
      echo "    · $d：四项均已就位 ✓"
    fi
  done
  if [ "$rc" -ne 0 ]; then
    echo "  🔴 四项须同时满足，缺一即不得判隔离成立 —— 尤其 PEP 前置拒与 deny 语义【须分开验】，"
    echo "     只验后者会让「先查了再拒」通过验收（查询已落到库上，跨租户读实际发生过）。"
    echo "  🔴 且须【逐领域】各验各的：别的领域挂了标记，不能替这个领域作数（SA-14）。"
    return 1
  fi
  echo "  ⑩ 各领域乙组四项负测均已就位 ✓（存在性由机器判；语义正确性归乙域人审，本闸不代判）"
  return 0
}

selftest() {
  T=$(mktemp -d); n=0; fail=0
  ok(){ n=$((n+1)); echo "  ✓ $1"; }; bad(){ echo "  ✗ 反测未过：$1"; fail=1; }

  if check >/dev/null 2>&1; then ok "现状（无租户列）暂过，且不误报" ; else bad "现状被误判"; fi

  D1=internal/.tenantgate-selftest; D2=internal/.tenantgate-selftest2
  cleanup() { rm -rf "$D1" "$D2"; }
  ALL='YIZU_PEP_BEFORE_RESOLVE YIZU_DENY_NOT_EMPTY_SET YIZU_EXISTENCE_INDISTINGUISHABLE YIZU_MINIMAL_DENY_AUDIT'
  marks() { # marks <domain> <标记数>
    _d=$1; _k=$2; printf 'package t\n// '; _i=0
    for m in $ALL; do _i=$((_i+1)); [ $_i -le "$_k" ] && printf '%s(%s) ' "$m" "$_d"; done; printf '\n'
  }

  # 注入一个租户列 -> 四项负测不存在 -> 必拦
  mkdir -p "$D1"
  printf 'CREATE TABLE demo(tenant_id TEXT NOT NULL);\n' > "$D1/x.sql"
  if check >/dev/null 2>&1; then bad "加了租户列却未要求四项负测"; else ok "拦住：租户列落地而乙组四项未就位"; fi

  # 裸标记（不带领域）不算数 —— 一轮正是靠裸标记就能过
  printf 'package t\n// YIZU_PEP_BEFORE_RESOLVE YIZU_DENY_NOT_EMPTY_SET YIZU_EXISTENCE_INDISTINGUISHABLE YIZU_MINIMAL_DENY_AUDIT\n' \
    > "$D1/x_test.go"
  if check >/dev/null 2>&1; then bad "裸标记（不带领域）被当成该领域已就位"; else ok "拦住：裸标记不算数（标记须绑领域）"; fi

  # 四项带本领域 -> 放行
  marks .tenantgate-selftest 4 > "$D1/x_test.go"
  if check >/dev/null 2>&1; then ok "不误伤：本领域四项就位后放行"; else bad "本领域四项就位仍被拒"; fi

  # 只就位三项 -> 仍须拦（四项须【同时】）
  marks .tenantgate-selftest 3 > "$D1/x_test.go"
  if check >/dev/null 2>&1; then bad "只就位三项也放行了 —— 四项须同时"; else ok "拦住：四项缺一（同时性有牙）"; fi

  # 🔴 SA-14 的骨：A 领域四项齐全，B 领域新加租户列且一条没有 —— 必须拦 B。
  #    一轮此处会放行，因为四个标记是全局的。
  marks .tenantgate-selftest 4 > "$D1/x_test.go"
  mkdir -p "$D2"
  printf 'CREATE TABLE demo2(tenant_id TEXT NOT NULL);\n' > "$D2/y.sql"
  if check >/dev/null 2>&1; then bad "别的领域挂了四项标记，新领域就直接过了（SA-14 未闭）"; \
    else ok "拦住：A 领域标记齐全不能替 B 领域作数（逐领域各验各的）"; fi

  # 两个领域各自齐全 -> 放行
  marks .tenantgate-selftest2 4 > "$D2/y_test.go"
  if check >/dev/null 2>&1; then ok "不误伤：两个领域各自四项齐全后放行"; else bad "两领域各自齐全仍被拒"; fi

  cleanup; rm -rf "$T"
  [ $fail -eq 0 ] && echo "--- 反测 $n 项全过（闸有牙）" || echo "--- 反测未过"
  return $fail
}

case "${1:-}" in --selftest) selftest; exit $? ;; esac
check; exit $?
