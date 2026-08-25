#!/bin/sh
# 一键代跑 —— 交给有 PG 的一方（ops）执行，把 stdout 原样贴回即可。
#
# 🔴 本件解决的是一个**分派问题**，不是技术问题：
#   执笔方与乙域门方**均无 PG 环境**（psql/postgres/pg_ctl 均无、docker daemon 不可达、
#   无监听端口、无 DSN），故 RV-乙15 的两问
#   「dw_migrate 能 ALTER TABLE？dw_app 试 ALTER 被拒？」**只能由第三方跑出**。
#   ⚠️ 该问已被追问四次；每次的答案都只能是「我产生不了」。
#   ⇒ 把跑法压成一条命令，降低代跑方的门槛到最低。
#
# 用法：  DB=dw_rls_fixture sh specs/fixtures/rls/RUN.sh
#   或：  DSN='postgres://user@host/db' sh specs/fixtures/rls/RUN.sh
set -u
DB=${DB:-dw_rls_fixture}
PSQL="psql -v ON_ERROR_STOP=0 ${DSN:+-d $DSN}"
[ -n "${DSN:-}" ] || PSQL="psql -v ON_ERROR_STOP=0 -d $DB"

command -v psql >/dev/null 2>&1 || { echo "✗ 本机无 psql —— 本件须在有 PG 的一方执行"; exit 2; }

echo "════════ setup.sql（建面：三 role · FORCE RLS · 逐命令 policy）════════"
$PSQL -f specs/fixtures/rls/setup.sql
for t in t4-ddl-rights t1-write-as-other t2-no-context-deny t3-no-residue; do
  echo
  echo "════════ $t.sql ════════"
  $PSQL -f "specs/fixtures/rls/$t.sql"
done
echo
echo "════════ 请把以上 stdout 原样贴回 ════════"
echo "🔴 与 specs/fixtures/rls/EXPECTED.md 不符处 **不要自行调整脚本** ——"
echo "   贴回原始输出即可；执笔方按差异改合同（更可能是合同写错，而非 PG 行为意外）。"
