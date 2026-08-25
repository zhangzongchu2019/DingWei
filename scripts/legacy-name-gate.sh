#!/bin/sh
# 旧模型名棘轮（㉑）—— 受控符号集合 {TenantCtx, PendingTenantCtx, NewTenantCtx} 的**出现数**，
# 按 prod / test / comment **三类各自**只许降不许升，终值 prod=0。
#
# 🔴 立此闸的原因（甲域 sol，R2 delta ②）：
#   §21.1c 写「迁移期不得新增 TenantCtx」，而执笔方称由 ⑦ 执行 —— 那句是假的
#   （⑦ 只按效果统计 PendingTenantCtx 造出的上下文债）。规矩没有执行者＝没写。
#
# 🔴 **本闸首版自己犯了它要防的那个错**（甲域 sol，delta-b）：
#   首版用 `grep -c` 逐文件计数 —— 那数的是【含词行数】，不是【出现数】：
#     · 同一行出现两次只记 1 ⇒ **在已有行上再加一个旧名不涨数**；
#     · 注释 / 声明 / 测试混计 ⇒ **删一行注释可抵消新增一行生产用法**。
#   实证：`printf "TenantCtx TenantCtx\n" | grep -c TenantCtx` → 1，而 `grep -o | wc -l` → 2。
#   ⚠️ 「按行计数却断言出现数」是本仓老病，与 pending 债那条同族 ——
#     而本闸正是为防这类而立的。
#   ⇒ 改为 Go AST 逐 Ident 计数（同一行多个各记一次），注释另行词法统计；
#     **三类分别棘轮**，因为合计一个数天然允许互相抵消。
set -u

BASE=${BASE:-specs/gates/legacy-name-baseline.txt}
SRC=${SRC:-"internal cmd"}
TOOL=${TOOL:-./specs/tools/legacy-name}

check() {
  [ -f "$BASE" ] || { echo "✗ 闸依赖缺失：$BASE 不存在 —— 无基线即无棘轮，闸坏了"; return 2; }
  _out=$(go run "$TOOL" $SRC 2>&1) || { echo "✗ 计数器运行失败 —— 跑不了就不能报数"; printf '%s\n' "$_out" | head -3; return 2; }
  _bad=0
  for k in prod test comment; do
    _n=$(printf '%s\n' "$_out" | grep -oE "^$k=[0-9]+" | cut -d= -f2)
    _b=$(grep -oE "^legacy_$k=[0-9]+" "$BASE" | cut -d= -f2)
    [ -n "$_b" ] || { echo "✗ $BASE 缺 legacy_$k 基线项 —— 闸坏了（合计一个数会让三类互相抵消）"; return 2; }
    echo "  $k：$_n（基线 $_b$( [ "$k" = prod ] && echo "，终值 0" )）"
    if [ "${_n:-0}" -gt "${_b:-0}" ]; then
      echo "✗ $k 类出现数回升（$_b → $_n）—— §21.1c：迁移期不得新增，新写代码一律用 AccessCtx"
      _bad=$((_bad+1))
    fi
  done
  [ "$_bad" -gt 0 ] && { echo "  🔴 **三类分别棘轮**：合计持平不算数 —— 一降一升会互相抵消。"; return 1; }
  echo "  ✓ 三类均未回升；prod 终值 0，尚未达标（迁移执行归 SP1）"
  echo "  ⚠️ 本闸判【出现数】，判不了【改对了没有】—— 改名而语义仍单层，它看不出来。"
  return 0
}

selftest() {
  T=$(mktemp -d) || return 2
  n=0; fail=0
  ok(){ n=$((n+1)); echo "  ✓ $1"; }; bad(){ echo "  ✗ 反测未过：$1"; fail=1; }
  mkdir -p "$T/src"
  _mk(){ printf 'package p\n\n%s\n' "$1" > "$T/src/a.go"; }
  _base(){ printf 'legacy_prod=%s\nlegacy_test=%s\nlegacy_comment=%s\n' "$1" "$2" "$3" > "$T/b.txt"; }
  _run(){ BASE="$T/b.txt" SRC="$T/src" TOOL="$(pwd)/specs/tools/legacy-name" check >/dev/null 2>&1; }

  _mk 'var a TenantCtx'; _base 1 0 0; _run
  [ $? -eq 0 ] && ok "不误伤：与基线持平放行" || bad "持平被拦"

  _mk 'var a, b TenantCtx'; _base 1 0 0
  # ⚠️ 上一行两个声明共用一个类型名 ⇒ Ident 仍是 1；下面才是真的同行两次
  _mk 'var a TenantCtx; var b TenantCtx'; _base 1 0 0; _run
  [ $? -eq 1 ] && ok "🔴 拦住：**同一行**新增第二个旧名（行计数看不见，甲域 delta-b）" || bad "同行新增被放行"

  # 🔴 甲域要的另一条：一降一升、合计持平，仍须拒
  _mk '// TenantCtx 注释
var a TenantCtx
var b TenantCtx'
  _base 1 0 2      # 基线：prod=1 comment=2；实际 prod=2 comment=1 ⇒ 合计持平
  _run
  [ $? -eq 1 ] && ok "🔴 拦住：一降一升、合计持平（注释减 1、生产加 1）—— 分类棘轮才拦得住" || bad "合计持平被放行"

  _mk 'var a TenantCtx'; _base 5 5 5; _run
  [ $? -eq 0 ] && ok "不误伤：低于基线（已改掉一批）放行" || bad "下降被误拦"

  # 🔴 甲域 sol（㉑ 复核）：扫描根不存在时原实现计 0 并放行 ——
  #   「走不到」被当成「走过了，没有」。**缺失与不适用必须分开。**
  _base 1 0 0
  BASE="$T/b.txt" SRC="$T/nosuch-root" TOOL="$(pwd)/specs/tools/legacy-name" check >/dev/null 2>&1
  [ $? -eq 2 ] && ok "拦住：扫描根不存在判闸坏（非「计 0 放行」）" || bad "缺根目录被当成零命中放行"

  # 🔴 重叠名称：PendingTenantCtx 内含 TenantCtx，子串计数会把一处计两次
  _mk '// PendingTenantCtx 只应计一次'; _base 0 0 1; _run
  [ $? -eq 0 ] && ok "不误伤：重叠名称按标识符边界精确匹配（一处只归一个目标）" || bad "重叠名称被重复计数"

  _mk 'var a tenantCtxKey'; _base 0 0 0; _run
  [ $? -eq 0 ] && ok "不误伤：复合标识符 tenantCtxKey 不算（判受控符号集合，不判子串）" || bad "子串被误计"

  printf 'legacy_prod=1\n' > "$T/b.txt"; _mk 'var a TenantCtx'
  BASE="$T/b.txt" SRC="$T/src" TOOL="$(pwd)/specs/tools/legacy-name" check >/dev/null 2>&1
  [ $? -eq 2 ] && ok "拦住：基线缺 test/comment 项判闸坏（合计一个数会让三类互相抵消）" || bad "缺分类基线被当免检"

  BASE="$T/nosuch.txt" _run
  [ $? -eq 2 ] && ok "拦住：基线件缺失判闸坏（非免检）" || bad "件缺失被当免检"

  rm -rf "$T"
  [ $fail -eq 0 ] && echo "--- 反测 $n 项全过（闸有牙）" || echo "--- 反测未过（闸没牙）"
  return $fail
}

case "${1:-}" in --selftest) selftest;; *) check;; esac
