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
  if [ "$_nn" -gt 0 ]; then
    echo "✗ 下列闸未给覆盖见证（跨闸契约第 4 条）：$_no"
    echo "  ⇒ 🔴 恒零只有在「完整覆盖面已验且值为 0」时才能 PASS；"
    echo "     不给范围，它的 ✓ 就无法与「没查/没触发」区分开。"
    return 1
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

  mkg a.sh 'echo "  受检范围：3 件"' 0; mkg b.sh 'echo "  预期集合：2"' 0; mkd a.sh b.sh
  ( cd "$T" && DRIVER=drv.sh sh "$SELF" >/dev/null 2>&1 )
  [ $? -eq 0 ] && ok "不误伤：全部闸都给见证时放行" || bad "全给见证却被拦"

  mkg c.sh 'echo "  ✓ 全部通过"' 0; mkd a.sh c.sh
  ( cd "$T" && DRIVER=drv.sh sh "$SELF" >/dev/null 2>&1 )
  [ $? -eq 1 ] && ok "🔴 拦住：有闸只报「全部通过」而不给受检范围" || bad "缺见证被放行"

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
