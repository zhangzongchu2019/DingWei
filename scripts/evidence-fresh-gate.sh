#!/bin/sh
# 证据包时效闸 —— 证据必须产自被评审的那个锚。
#
# 立此闸的直接原因（甲域 sol，R2 轮1）：
#   R2 评审包里的 `specs/evidence/EVIDENCE.md` 头写「分支头 d0322c6」，
#   而本轮冻结锚是 review-r2 —— 两锚之间改了 detector、Option 收口测试、conformance suite 等评审对象。
#
# 🔴 sol 的区分照录，它比「过期了」准得多：
#   「快照 MANIFEST 只能证明**过期 EVIDENCE 文件本身没被篡改**，
#     **不能证明其内容是冻结锚产出**。」
#   ⇒ 指纹一致 ≠ 内容当期。两个都要，缺一不可。
#
# ⚠️ 能力边界：本闸判「头块所载锚是否为当前 HEAD 的祖先且非落后」，
#   判不了「内容是否真由该锚跑出来」—— 那要重跑 collect.sh 比对，成本高，归门方抽验。
set -u

EV=${EV:-specs/evidence/EVIDENCE.md}

check() {
  [ -f "$EV" ] || { echo "✗ 闸依赖缺失：$EV 不存在 —— 闸坏了，不是免检"; return 2; }
  _a=$(grep -m1 -oE '分支头[^`]*`([0-9a-f]{7,40})`' "$EV" | grep -oE '[0-9a-f]{7,40}')
  [ -n "$_a" ] || { echo "✗ $EV 头块无「分支头」锚 —— 无法判定时效，闸坏了"; return 2; }
  _h=$(git rev-parse HEAD 2>/dev/null) || { echo "✗ 非 git 工作树，闸坏了"; return 2; }
  if [ "$_a" = "$(echo "$_h" | cut -c1-${#_a})" ]; then
    echo "  ✓ 证据包生成自当前 HEAD（$_a）"
    echo "  ⚠️ 本闸判不了【内容是否真由该锚跑出】—— 那要重跑 collect.sh 比对，归门方抽验。"
    return 0
  fi
  _n=$(git rev-list --count "$_a..HEAD" 2>/dev/null || echo '?')
  echo "✗ 证据包不是当前锚的产出：头块写 $_a，HEAD 为 $(echo "$_h"|cut -c1-12)，其间 $_n 个提交。"
  echo "  🔴 **快照指纹一致只证明该文件没被篡改，不证明其内容是当期产出** —— 两者是不同的保证。"
  echo "  ⇒ 重跑 specs/evidence/collect.sh 后再提交。"
  return 1
}

selftest() {
  T=$(mktemp -d) || return 2
  n=0; fail=0
  ok(){ n=$((n+1)); echo "  ✓ $1"; }; bad(){ echo "  ✗ 反测未过：$1"; fail=1; }
  _h=$(git rev-parse HEAD)

  printf '生成时刻：X\n分支头：`%s`\n' "$_h" > "$T/e.md"
  EV="$T/e.md" check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：锚等于 HEAD 时放行" || bad "当期证据包被误拦"

  printf '生成时刻：X\n分支头：`%s`\n' "$(git rev-parse HEAD~3 2>/dev/null || echo 0000000)" > "$T/e.md"
  EV="$T/e.md" check >/dev/null 2>&1
  [ $? -eq 1 ] && ok "🔴 拦住：锚落后于 HEAD（证据非当期产出）" || bad "过期证据包被放行"

  printf '生成时刻：X\n没有锚\n' > "$T/e.md"
  EV="$T/e.md" check >/dev/null 2>&1
  [ $? -eq 2 ] && ok "拦住：头块无锚判闸坏（非免检）" || bad "无锚被当免检"

  EV="$T/nosuch.md" check >/dev/null 2>&1
  [ $? -eq 2 ] && ok "拦住：证据件缺失判闸坏（非免检）" || bad "件缺失被当免检"

  rm -rf "$T"
  [ $fail -eq 0 ] && echo "--- 反测 $n 项全过（闸有牙）" || echo "--- 反测未过（闸没牙）"
  return $fail
}

case "${1:-}" in --selftest) selftest; exit $? ;; esac
check; exit $?
