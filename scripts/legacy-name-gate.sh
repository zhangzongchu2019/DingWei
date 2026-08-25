#!/bin/sh
# 旧模型名棘轮 —— `TenantCtx` 等单层模型标识符只许降不许升。
#
# 🔴 立此闸的直接原因（甲域 sol，delta 复核 cd96a13 第 ② 组）：
#   §21.1c 正文写着「迁移期不得新增 TenantCtx 调用点：提交闸 ⑦ 的按领域棘轮已盯住调用点数」——
#   **这句是假的**。⑦（pending-debt）按效果判，只统计 `PendingTenantCtx` 造出的上下文债，
#   **一般的 TenantCtx 出现它根本不看**。
#   ⚠️ 于是「不得新增」这条规矩**没有任何执行者**，写了等于没写。
#   ⇒ 本闸补上那个执行者：一般 TenantCtx 出现数按基线棘轮，只许降不许升，终值 0。
#
# ⚠️ 能力边界：判「出现数」，判不了「改对了没有」——
#   把 TenantCtx 改名成 AccessCtx 而语义仍是单层，本闸看不出来，那归人审。
set -u

BASE=${BASE:-specs/gates/legacy-name-baseline.txt}
PAT=${PAT:-'TenantCtx'}

_count() {
  git ls-files 'internal/*.go' 'internal/**/*.go' 'cmd/**/*.go' 2>/dev/null \
    | while IFS= read -r f; do [ -f "$f" ] && grep -c "$PAT" "$f" 2>/dev/null; done \
    | awk '{s+=$1} END{print s+0}'
}

check() {
  [ -f "$BASE" ] || { echo "✗ 闸依赖缺失：$BASE 不存在 —— 无基线即无棘轮，闸坏了"; return 2; }
  _b=$(grep -oE '^legacy_TenantCtx=[0-9]+' "$BASE" | cut -d= -f2)
  [ -n "$_b" ] || { echo "✗ $BASE 无 legacy_TenantCtx 基线项 —— 闸坏了"; return 2; }
  _n=$(_count)
  echo "  旧模型名 \`$PAT\` 出现：$_n 处（基线 $_b，终值 0）"
  if [ "$_n" -gt "$_b" ]; then
    echo "✗ 旧模型名出现数回升（$_b → $_n）—— §21.1c：迁移期不得新增，新写代码一律用 AccessCtx"
    echo "  🔴 该规矩此前**没有执行者**：⑦ 只按效果统计 PendingTenantCtx，不看一般出现。"
    return 1
  fi
  [ "$_n" -lt "$_b" ] && echo "  ⓘ 已下降（$_b → $_n）：改完一批请同步下调基线，棘轮才不会松回去"
  echo "  ✓ 未回升；终值 0，尚未达标（迁移执行归 SP1）"
  echo "  ⚠️ 本闸判【出现数】，判不了【改对了没有】—— 改名而语义仍单层，它看不出来。"
  return 0
}

selftest() {
  T=$(mktemp -d) || return 2
  n=0; fail=0
  ok(){ n=$((n+1)); echo "  ✓ $1"; }; bad(){ echo "  ✗ 反测未过：$1"; fail=1; }

  printf 'legacy_TenantCtx=%s\n' "$(_count)" > "$T/b.txt"
  BASE="$T/b.txt" check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：与基线持平时放行" || bad "持平被拦"

  printf 'legacy_TenantCtx=%s\n' "$(( $(_count) - 1 ))" > "$T/b.txt"
  BASE="$T/b.txt" check >/dev/null 2>&1
  [ $? -eq 1 ] && ok "🔴 拦住：出现数高于基线（新增旧模型名，甲域 delta ②）" || bad "回升被放行"

  printf 'legacy_TenantCtx=%s\n' "$(( $(_count) + 5 ))" > "$T/b.txt"
  BASE="$T/b.txt" check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：低于基线（已改掉一批）放行" || bad "下降被误拦"

  printf 'x=1\n' > "$T/b.txt"
  BASE="$T/b.txt" check >/dev/null 2>&1
  [ $? -eq 2 ] && ok "拦住：基线项缺失判闸坏（非免检）" || bad "缺基线被当免检"

  BASE="$T/nosuch.txt" check >/dev/null 2>&1
  [ $? -eq 2 ] && ok "拦住：基线件缺失判闸坏（非免检）" || bad "件缺失被当免检"

  rm -rf "$T"
  [ $fail -eq 0 ] && echo "--- 反测 $n 项全过（闸有牙）" || echo "--- 反测未过（闸没牙）"
  return $fail
}

case "${1:-}" in --selftest) selftest;; *) check;; esac
