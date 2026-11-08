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

# 仅识别SQL/Go字面DDL中的列声明；排除JSON字段与查询引用。
# 检测器支持CREATE TABLE和ALTER TABLE ADD COLUMN、引号标识符、注释、字面串拼接；
# 不执行动态生成DDL，不声称覆盖运行期字符串拼装。表业务归属清单不是豁免。
MARKERS='YIZU_PEP_BEFORE_RESOLVE YIZU_DENY_NOT_EMPTY_SET YIZU_EXISTENCE_INDISTINGUISHABLE YIZU_MINIMAL_DENY_AUDIT'
scan_tenant() {
  "$SCANNER" .
}
domain_of() {
  printf '%s\n' "$1" | cut -f1
}

check() {
  [ -d internal ] || { echo "✗ 闸依赖缺失：internal/ 不存在"; return 2; }
  HITS=$(scan_tenant) || { echo "✗ schema检测器失败（闸坏，非通过）"; return 2; }
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
  T=$(mktemp -d) || return 2
  n=0; fail=0
  ok(){ n=$((n+1)); echo "  ✓ $1"; }; bad(){ echo "  ✗ 反测未过：$1"; fail=1; }
  mkdir -p "$T/internal/a" "$T/internal/b" "$T/specs/gates"
  : > "$T/specs/gates/tenant-schema-domains.txt"
  run(){ (cd "$T" && check) > "$T/result" 2>&1; }
  marks(){
    printf 'package fixture\n// '
    k=0
    for m in $MARKERS; do k=$((k+1)); [ "$k" -gt "$2" ] || printf '%s(%s) ' "$m" "$1"; done
    printf '\n'
  }
  run && ok '空schema暂过' || bad '空schema误报'
  printf 'package a\ntype Input struct { Tenant string `json:"tenant_key"` }\n' > "$T/internal/a/input.go"
  run && ok 'JSON请求字段不冒充schema' || bad 'JSON误报'
  printf 'CREATE TABLE demo(tenant_id TEXT NOT NULL);\n' > "$T/internal/a/x.sql"
  run && bad '真实租户列漏报' || ok '真实租户列缺四项必拦'
  printf 'package a\n// %s\n' "$MARKERS" > "$T/internal/a/x_test.go"
  run && bad '裸标记误放行' || ok '裸标记不计'
  marks a 4 > "$T/internal/a/x_test.go"
  run && ok '本域四项齐全放行' || bad '齐全仍拒'
  marks a 3 > "$T/internal/a/x_test.go"
  run && bad '缺一放行' || ok '四项同时性'
  marks a 4 > "$T/internal/a/x_test.go"
  printf 'ALTER TABLE demo ADD COLUMN tenant_key TEXT;\n' > "$T/internal/b/y.sql"
  run && bad '跨域借标记' || ok '别域标记不能抵债'
  marks b 4 > "$T/internal/b/y_test.go"
  run && ok '各域各自齐全放行' || bad '各域齐全仍拒'
  # 迁移目录与真实业务域分开；映射只能转移要求，不能免检。
  printf 'demo c\n' > "$T/specs/gates/tenant-schema-domains.txt"
  run && bad '业务归属映射变成豁免' || ok '映射业务域仍缺项必拦'
  mkdir -p "$T/internal/c"; marks c 4 > "$T/internal/c/x_test.go"
  run && ok '真实归属域四项补齐放行' || bad '映射域齐全仍拒'
  rm -rf "$T"
  [ "$fail" -eq 0 ] && echo "--- 反测 $n 项全过（闸有牙）"
  return "$fail"
}

SCANNER=$(mktemp) || exit 2
trap 'rm -f "$SCANNER"' EXIT HUP INT TERM
if ! go build -o "$SCANNER" ./specs/tools/tenant-schema; then
  echo '✗ schema检测器构建失败（闸坏，非通过）'; exit 2
fi
case "${1:-}" in --selftest) selftest; exit $? ;; esac
check; exit $?
