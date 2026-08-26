#!/bin/sh
# 覆盖见证元闸（提交闸 ㉙）—— 核【每个闸】是否给出覆盖见证。
#
# 🔴 立此闸的直接原因（乙域裁定 2026-08-26，跨闸硬契约第 4 条）：
#   「每闸须给覆盖见证：预期集合 · 实检集合 · 未知集合」。
#   ⚠️ **契约没有执行者 ＝ 没有契约** —— 本轮已有三个实例
#   （E.5 自书须增行而无人执行 · 闸输出印着「不得自增自批」而批准方不校验 ·
#    speclint 注释称「对所有人生效」而只看 docs/**）。**本闸就是那个执行者。**
#
# 🔴 本闸自身受同一契约约束，故它自己也打印覆盖见证。
#
# ⚠️ 能力边界：
#   · 判【闸的输出里有没有覆盖见证】——**按效果判**（实跑并读其 stdout），不按脚本源码猜；
#   · 🔴 **判不了见证里的数【对不对】** —— 一个闸可以打印一个错的范围，本闸看不出来，归门方；
#   · 🔴 **本闸过 ≠ 覆盖面真的完整**（GATE-CONTRACT 三之三：闸过不构成语义批准）。
set -u
SELF=$(cd "$(dirname "$0")" && pwd)/$(basename "$0")
DRIVER=${DRIVER:-scripts/speclint-gate.sh}
# 🔴 预期集合由 git/driver 机器派生，**不手写** —— 凡说「全部」，清单就不能手写。
list_gates() { grep -oE '^scripts/[a-z0-9-]+\.sh' "$DRIVER" 2>/dev/null | sort -u | grep -v "$(basename "$SELF")"; }

WITNESS_RE='受检范围|预期集合'
BASELINE=${WITNESS_BASELINE:-specs/gates/gate-witness-baseline.txt}
DEBT=${WITNESS_DEBT:-specs/gates/gate-witness-debt.txt}

# 🔴 **fail-closed 棘轮模式**（乙域 terra 裁定 2026-08-26，逐字实现）：
#   「㉙ 立即接入侧分支的提交/CI 链路，但采用 fail-closed **棘轮**模式，
#     而非接受当前 18 项债务、或让全仓一次性停摆。」
#   ⇒ 阻断条件（任一成立即拒）：
#     ① 缺见证数**增加**；② 既有合规闸**退化**（原合规现不合规）；
#     ③ 派生清单中**新增闸无见证**；④ 债务清单里有闸**缺 owner 或缺到期里程碑**。
#   ⇒ 仅在「缺见证数减少或不变、且无退化」时通过，并**每次打印基线 / 当前 / 增减 / 逐闸列表**。
# ⚠️ **接入时机不归本闸决定** —— 本闸只提供可接入的形态；是否挂进 speclint / CI 归 coordinator。
#   🔴 故本脚本**尚未**被 scripts/speclint-gate.sh 调用（那一步是「接入」，不是「实现」）。

check() {
  [ -f "$DRIVER" ] || { echo "✗ 闸依赖缺失：$DRIVER —— 覆盖范围取不到 ⇒ UNKNOWN(exit 2)"; return 2; }
  _exp=$(list_gates); _en=$(printf '%s\n' "$_exp" | awk 'NF' | wc -l | tr -d ' ')
  [ "${_en:-0}" -eq 0 ] && { echo "✗ 预期集合为空（从 $DRIVER 派生不出闸清单）—— 取不到就不能报通过 ⇒ exit 2"; return 2; }

  _ok=''; _no=''; _unk=''
  for g in $_exp; do
    if [ ! -x "$g" ] && [ ! -f "$g" ]; then _unk="$_unk $g(缺件)"; continue; fi
    _out=$(sh "$g" 2>/dev/null); _rc=$?
    if [ "$_rc" -eq 2 ]; then _unk="$_unk $g(exit2)"; continue; fi
    if printf '%s' "$_out" | grep -qE "$WITNESS_RE"; then _ok="$_ok $g"; else _no="$_no $g"; fi
  done
  _on=$(printf '%s' "$_ok" | wc -w | tr -d ' ')
  _nn=$(printf '%s' "$_no" | wc -w | tr -d ' ')
  _un=$(printf '%s' "$_unk" | wc -w | tr -d ' ')

  # 🔴 本闸自己的覆盖见证（受同一契约约束）
  echo "  【覆盖见证】预期集合：$_en 个闸（由 $DRIVER 机器派生，非手写）"
  echo "             实检集合：$((_on + _nn)) 个（已跑并读其 stdout）"
  echo "             未知集合：$_un 个${_unk:+ ⇒$_unk}"
  echo "             合规 $_on · 缺见证 $_nn"

  [ "$_un" -gt 0 ] && { echo "✗ 未知集合非空 ⇒ 不得报 PASS（跨闸契约第 2 条）"; return 2; }

  # ===== fail-closed 棘轮 =====
  [ -f "$BASELINE" ] || { echo "✗ 闸依赖缺失：$BASELINE —— 基线取不到即无从判增减 ⇒ UNKNOWN(exit 2)"; return 2; }
  _b=$(sed -n 's/^missing_witness=\([0-9]*\).*/\1/p' "$BASELINE" | head -1)
  case "$_b" in ''|*[!0-9]*) echo "✗ $BASELINE 读不到 missing_witness= —— 判闸坏"; return 2 ;; esac
  _bok=$(sed -n 's/^compliant=\([0-9]*\).*/\1/p' "$BASELINE" | head -1)
  case "$_bok" in ''|*[!0-9]*) _bok=0 ;; esac
  echo "  【棘轮】基线缺见证 $_b · 当前 $_nn · 增减 $((_nn - _b))；基线合规 $_bok · 当前 $_on"
  [ -n "$_no" ] && { echo "  缺见证逐闸："; for g in $_no; do
      _row=$(awk -F'\t' -v g="$g" '$0 !~ /^#/ && $1==g {print $2" / 到期 "$3; exit}' "$DEBT" 2>/dev/null)
      echo "    $g　⇒ owner ${_row:-（缺）}"; done; }

  _block=0
  if [ "$_nn" -gt "$_b" ]; then
    echo "✗ 阻断①：缺见证数由基线 $_b 增至 $_nn —— 棘轮只许降不许升"; _block=1
  fi
  if [ "$_on" -lt "$_bok" ]; then
    echo "✗ 阻断②：合规闸由基线 $_bok 退化为 $_on —— 既有合规不得退化"; _block=1
  fi
  # ③④ 债务清单：每个缺见证的闸都须有 owner 与到期里程碑
  if [ ! -f "$DEBT" ]; then
    echo "✗ 阻断④：债务清单 $DEBT 不存在 —— 无 owner/到期即 FAIL（乙域裁定）"; _block=1
  else
    for g in $_no; do
      _o=$(awk -F'\t' -v g="$g" '$0 !~ /^#/ && $1==g && $2!="" && $3!="" {print "y"; exit}' "$DEBT" 2>/dev/null)
      [ "$_o" = y ] || { echo "✗ 阻断③④：$g 未在债务清单中登记 owner ＋ 到期里程碑（新增闸无见证亦走此条）"; _block=1; }
    done
  fi
  [ "$_block" -eq 1 ] && {
    echo "  🔴 恒零只有在「完整覆盖面已验且值为 0」时才能 PASS；不给范围，✓ 就无法与「没查/没触发」区分开。"
    return 1; }
  if [ "$_nn" -gt 0 ]; then
    echo "  ⚠️ PASS WITH REGISTERED DEBT：$_nn 个闸仍未给覆盖见证（基线 $_b，未增长）"
    echo "     🔴 **这不是「全闸合规」** —— 乙域原话：现有合规项不构成全闸合规声明。"
    echo "     🔴 债务不是豁免：每条都有 owner ＋ 到期里程碑，逾期须由门方处置。"
    return 0
  fi
  echo "  ✓ 全部 $_en 个闸均给出覆盖见证"
  echo "  ⚠️ 本闸判【有没有见证】，🔴 判不了【见证里的数对不对】—— 归门方；且本闸过 ≠ 覆盖面真的完整。"
  return 0
}

selftest() {
  T=$(mktemp -d) || { echo "✗ 夹具目录建不出 ⇒ selftest FAIL"; return 2; }
  n=0; fail=0; ok(){ n=$((n+1)); echo "  ✓ $1"; }; bad(){ echo "  ✗ 反测未过：$1"; fail=1; }
  mkdir -p "$T/scripts"
  mkg(){ printf '#!/bin/sh\n%s\nexit %s\n' "$2" "$3" > "$T/scripts/$1"; chmod +x "$T/scripts/$1"; }
  mkd(){ : > "$T/drv.sh"; for x in "$@"; do echo "scripts/$x" >> "$T/drv.sh"; done; }

  # 棘轮夹具：基线与债务清单（本闸自 2026-08-26 起为 fail-closed 棘轮，故反测须自带这两件）
  mkbl(){ printf 'missing_witness=%s\ncompliant=%s\n' "$1" "$2" > "$T/bl.txt"; }
  mkdebt(){ : > "$T/debt.txt"; for x in "$@"; do printf 'scripts/%s\t执笔方\tSP7a 收口\n' "$x" >> "$T/debt.txt"; done; }
  RAT="WITNESS_BASELINE=$T/bl.txt WITNESS_DEBT=$T/debt.txt"
  runw(){ ( cd "$T" && env WITNESS_BASELINE="$T/bl.txt" WITNESS_DEBT="$T/debt.txt" DRIVER=drv.sh sh "$SELF" >/dev/null 2>&1 ); }

  mkbl 0 2; mkdebt
  mkg a.sh 'echo "  受检范围：3 件"' 0; mkg b.sh 'echo "  预期集合：2"' 0; mkd a.sh b.sh
  runw; [ $? -eq 0 ] && ok "不误伤：全部闸都给见证时放行" || bad "全给见证却被拦"

  # 🔴 缺见证 ＋ 债务清单里没登记 ⇒ 拒（乙域第 ④ 条：无 owner/到期即 FAIL）
  mkbl 1 1; mkdebt
  mkg c.sh 'echo "  ✓ 全部通过"' 0; mkd a.sh c.sh
  runw; [ $? -eq 1 ] && ok "🔴 拦住：缺见证的闸未登记 owner ＋ 到期里程碑" || bad "无 owner/到期被放行"

  # 🔴 棘轮：登记了 owner/到期，且未超基线 ⇒ 放行为 REGISTERED DEBT（不是「全闸合规」）
  mkbl 1 1; mkdebt c.sh
  _wo=$( cd "$T" && env WITNESS_BASELINE="$T/bl.txt" WITNESS_DEBT="$T/debt.txt" DRIVER=drv.sh sh "$SELF" 2>&1 ); _wr=$?
  case "$_wo" in
    *"REGISTERED DEBT"*) [ $_wr -eq 0 ] && ok "🔴 棘轮：债务已登记且未增长 ⇒ 放行但亮 REGISTERED DEBT（非全闸合规）" \
                                        || bad "债务亮明但退出码不对";;
    *) bad "债务未增长时未亮明 REGISTERED DEBT";;
  esac

  # 🔴 阻断①：缺见证数**增加**即拒（棘轮只许降不许升）
  mkbl 0 2; mkdebt c.sh
  runw; [ $? -eq 1 ] && ok "🔴 阻断①：缺见证数由基线 0 增至 1 ⇒ 拒（棘轮只许降不许升）" || bad "缺见证增长被放行"

  # 🔴 阻断②：既有合规闸退化即拒
  mkbl 1 2; mkdebt c.sh
  runw; [ $? -eq 1 ] && ok "🔴 阻断②：合规数由基线 2 退化为 1 ⇒ 拒（既有合规不得退化）" || bad "合规退化被放行"

  # 🔴 基线缺失 ⇒ UNKNOWN(2)，不是 PASS 也不是 FAIL
  ( cd "$T" && env WITNESS_BASELINE="$T/nosuch" WITNESS_DEBT="$T/debt.txt" DRIVER=drv.sh sh "$SELF" >/dev/null 2>&1 )
  [ $? -eq 2 ] && ok "🔴 基线取不到 ⇒ UNKNOWN exit 2（无从判增减，不得报通过）" || bad "基线缺失时给了结论"
  mkbl 1 1; mkdebt c.sh

  mkg d.sh 'echo x' 2; mkd a.sh d.sh
  ( cd "$T" && DRIVER=drv.sh sh "$SELF" >/dev/null 2>&1 )
  [ $? -eq 2 ] && ok "🔴 未知集合非空（有闸 exit 2）⇒ 本闸亦 exit 2，不折叠为 PASS" || bad "未知被吞"

  mkd nosuch.sh
  ( cd "$T" && DRIVER=drv.sh sh "$SELF" >/dev/null 2>&1 )
  [ $? -eq 2 ] && ok "🔴 闸脚本缺件 ⇒ 计入未知集合 ⇒ exit 2" || bad "缺件被当合规"

  : > "$T/empty.sh"
  ( cd "$T" && DRIVER=empty.sh sh "$SELF" >/dev/null 2>&1 )
  [ $? -eq 2 ] && ok "🔴 预期集合为空（派生不出清单）⇒ exit 2（取不到就不能报通过）" || bad "空清单报通过"

  ( cd "$T" && DRIVER=nofile.sh sh "$SELF" >/dev/null 2>&1 )
  [ $? -eq 2 ] && ok "🔴 driver 缺失 ⇒ exit 2" || bad "driver 缺失却报通过"

  _o=$( ( mkg a.sh 'echo "  受检范围：3 件"' 0; mkd a.sh; cd "$T" && DRIVER=drv.sh sh "$SELF" ) 2>&1 )
  printf '%s' "$_o" | grep -q '预期集合' && printf '%s' "$_o" | grep -q '未知集合' \
    && ok "🔴 本闸自身也打印覆盖见证（受同一契约约束）" || bad "本闸自己不给见证"

  rm -rf "$T"
  [ $fail -eq 0 ] && { echo "  覆盖见证元闸反测 $n/$n ✓"; return 0; }
  return 1
}
case "${1:-}" in --selftest) selftest;; *) check;; esac
