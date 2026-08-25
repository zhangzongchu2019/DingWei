#!/bin/sh
# 门方复现闸（㉓）—— 用【门方自己给的命令】验「已修/未修」，不用执笔方临时想的判据。
#
# 🔴 立此闸的原因：执笔方报「已修」时给的一行判据，多次**证明的是上一轮修复还在**，
#   而不是这一轮的洞已修（归口方第九次交叉时点破）。
#   ⇒ 判据改由门方给：他问什么，闸就跑什么，输出直接可比。
# ⚠️ 能力边界：本闸证明「门方那条命令现在给出期望值」，
#   **不证明「该发现的语义诉求已完全满足」** —— 后者仍归门方。
set -u
T=${T:-specs/gates/reviewer-repro.txt}

check() {
  [ -f "$T" ] || { echo "✗ 闸依赖缺失：$T"; return 2; }
  _n=0; _bad=0
  while IFS="$(printf '\t')" read -r id want cmd; do
    case "$id" in ''|'#'*) continue;; esac
    _n=$((_n+1))
    _got=$(sh -c "$cmd" 2>/dev/null | tr -d '\r' | tail -1)
    if [ "$_got" != "$want" ]; then
      [ "$_bad" -eq 0 ] && echo "✗ 门方复现不符（用的是门方原命令，非执笔方自拟判据）："
      echo "    $id  期望【$want】 实得【$_got】"
      echo "      \$ $cmd"
      _bad=$((_bad+1))
    fi
  done < "$T"
  [ "$_bad" -gt 0 ] && { echo "  🔴 该发现**未修或修得不对** —— 不得以「上一轮已修」覆盖。"; return 1; }
  echo "  ✓ 门方复现台账 $_n 条全部给出期望值"
  echo "  ⚠️ 本闸证明【那条命令现在给出期望值】，不证明【语义诉求已完全满足】—— 后者归门方。"
  return 0
}

selftest() {
  D=$(mktemp -d) || return 2
  n=0; fail=0
  ok(){ n=$((n+1)); echo "  ✓ $1"; }; bad(){ echo "  ✗ 反测未过：$1"; fail=1; }
  printf 'X\t1\techo 1\n' > "$D/t.txt"
  T="$D/t.txt" check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：期望与实得一致时放行" || bad "一致被拦"
  printf 'X\t1\techo 2\n' > "$D/t.txt"
  T="$D/t.txt" check >/dev/null 2>&1
  [ $? -eq 1 ] && ok "🔴 拦住：实得与门方期望不符（该发现未修或修得不对）" || bad "不符被放行"
  printf 'X\t1\tfalse\n' > "$D/t.txt"
  T="$D/t.txt" check >/dev/null 2>&1
  [ $? -eq 1 ] && ok "拦住：命令失败时实得为空，不得当成通过" || bad "命令失败被当通过"
  T="$D/nosuch" check >/dev/null 2>&1
  [ $? -eq 2 ] && ok "拦住：台账缺失判闸坏（非免检）" || bad "缺台账被当免检"
  rm -rf "$D"
  [ $fail -eq 0 ] && echo "--- 反测 $n 项全过（闸有牙）" || echo "--- 反测未过（闸没牙）"
  return $fail
}
case "${1:-}" in --selftest) selftest;; *) check;; esac
