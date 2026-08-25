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
  # 🔴 判据不能是「锚 == HEAD」：本闸在 pre-commit 跑，此刻 HEAD 还是上一个提交，
  #   而证据包是【本次提交中】重生成的 ⇒ 恒不相等，形成死循环。本闸首版即如此，当场卡住自己。
  #   ⇒ 改判「证据锚不落后于**最后一次改动评审对象**的提交」——评审对象＝三件正文 + 闸与工具。
  # 🔴 乙域 terra R2 轮3：本闸在【冻结锚】上实测拒绝 —— 且那是结构性的，不是偶发。
  #   证据包在 pre-commit 生成，那一刻 HEAD 尚是上一个提交 ⇒ 它记的锚
  #   **永远比它所在的那个提交少一个**。而「打锚那次提交」本身通常就改了评审对象，
  #   于是判据「不落后于评审对象末次改动」在**任何冻结锚上恒为 FAIL**。
  #   ⚠️ 执笔方只在 pre-commit 上下文验过它，**没在评审上下文验过** —— 又一次「验了一部分报成全部」。
  #   ⇒ 判据改为：证据锚须为 HEAD **或 HEAD 的直接父**（即「本次提交中生成」这一形态），
  #     且自证据锚起、除【生成证据包的那一个提交】外，评审对象无其他改动。
  _last=$(git log -1 --format=%H -- docs/dingwei-platform-specdoc.md docs/dingwei-architecture-design.md \
            docs/dingwei-prototype.html scripts specs/tools 2>/dev/null)
  # ⚠️ 不用 `rev-list --count`：**合并历史上它会把并入的分支一并计数** —— 实测 merge 后
  #   「证据锚 → 其子提交」的距离变成 2，判据当场失效。改用【第一父链】判相邻，
  #   它才对应「证据在 pre-commit 生成，锚 = 该提交的父」这一真实形态。
  _n=$(git rev-list --count --first-parent "$_a..$_last" 2>/dev/null || echo 99)
  if [ -z "$_last" ] || [ "${_n:-99}" -le 1 ] || git merge-base --is-ancestor "$_last" "$_a" 2>/dev/null; then
    echo "  ✓ 证据包为当期产出（证据锚 $(echo "$_a"|cut -c1-12)，距评审对象末次改动 ${_n:-0} 个提交 ≤1）"
    echo "  ⚠️ 本闸判不了【内容是否真由该锚跑出】—— 那要重跑 collect.sh 比对，归门方抽验。"
    return 0
  fi
  echo "✗ 证据包早于最后一次评审对象改动：证据锚 $(echo "$_a"|cut -c1-12)，评审对象末次改动 $(echo "$_last"|cut -c1-12)，其间 $_n 个提交。"
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

  printf '生成时刻：X\n分支头：`%s`\n' "$(git rev-list --max-parents=0 HEAD | head -1)" > "$T/e.md"
  EV="$T/e.md" check >/dev/null 2>&1
  [ $? -eq 1 ] && ok "🔴 拦住：证据锚早于评审对象末次改动（非当期产出）" || bad "过期证据包被放行"

  # 🔴 乙域 terra 指出的结构性缺陷：证据锚天然比其所在提交少一个（pre-commit 生成），
  #   若判据要求「不落后于评审对象末次改动」，则在**任何冻结锚上恒 FAIL**。
  # 构造需与判据同源：取【评审对象末次改动】的父提交作证据锚 —— 那正是冻结锚上的真实形态
  #（证据在 pre-commit 生成，锚 = 该提交的父）。
  #   ⚠️ 首版反测用 HEAD~1，而 HEAD~1 未必改过评审对象 ⇒ 距离 >1，反测自己构造错了。
  _lastc=$(git log -1 --format=%H -- docs/dingwei-platform-specdoc.md docs/dingwei-architecture-design.md \
             docs/dingwei-prototype.html scripts specs/tools 2>/dev/null)
  printf '生成时刻：X\n分支头：`%s`\n' "$(git rev-parse "$_lastc^" 2>/dev/null || git rev-parse HEAD)" > "$T/e.md"
  EV="$T/e.md" check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "🔴 不误伤：证据锚为【评审对象末次改动】的父（冻结锚上的正常形态）须放行" \
    || bad "冻结锚上恒判 FAIL —— 闸在评审时点系统性误判"

  # 🔴 钉住首版的死循环：pre-commit 时 HEAD 尚是上一个提交，
  #   若判据写成「锚 == HEAD」则证据包永远追不上，闸把自己卡死。
  printf '生成时刻：X\n分支头：`%s`\n' "$(git rev-parse HEAD)" > "$T/e.md"
  EV="$T/e.md" check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：证据锚 == HEAD 时放行（不得因 pre-commit 时序把自己卡死）" \
    || bad "证据锚等于 HEAD 却被拦 —— 判据把自己卡死了"

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
