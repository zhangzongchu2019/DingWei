#!/bin/sh
# RV 标注完整性闸 —— 标注在移入沿革前，原文不可被改写。
#
# 立此闸的直接原因（丙域 kimi，R2 轮3）：
#   执笔方清理「三级映射」旧模型名时用了**全文替换**，
#   把丙域标注的**正文**一起改了：原话「本行『三级映射』沿用旧模型名」
#   被改成「本行『§21.1 双层结构』沿用旧模型名」—— **语义不通，且篡改了评审方原文**。
#
# 🔴 **标注是 reviewer-owned 的证据，不是可被执笔方顺手改的文本。**
#   清理工具的作用域必须排除【RV…】标注段及其内引文。
#   ⚠️ 与本仓已归纳的「改一处漏一处」是镜像：那是该改的没改，这是**不该改的改了**。
#
# 判据：台账里每条 RV 的**摘要前缀**必须仍能在正文/沿革件中原样找到。
#   摘要在标注入账时抓取，此后不再变 ⇒ 它就是「原文未被改写」的可比对象。
#
# ⚠️ 能力边界：判「摘要前缀是否仍原样存在」，判不了「整段是否一字未改」——
#   后者需逐字快照，成本高；本闸抓的是**被工具批量改写**这一类（必然动到前缀）。
set -u

LEDGER=${LEDGER:-specs/gates/rv-ledger.txt}
DOCS=${DOCS:-'docs/dingwei-platform-specdoc.md docs/dingwei-platform-architecture.md docs/dingwei-prototype.html'}
HIST=${HIST:-docs/dingwei-revision-history.md}
# 🔴 扫描面须按【标注实际住在哪】定，不按目录习惯定：
#   甲域 RV-甲41 标在 `specs/evidence/collect.sh` 上，而本闸原只扫 scripts/prose-style-gate.sh
#   ⇒ 台账里有它、正文里也有它，闸却报「原文被改写」（它根本没看那个文件）。
#   ⚠️ 与 sol R2 轮3 抓的「闸看不见自己的头注」是同一形态，只是换了目录。
GATES=${GATES:-"$(ls scripts/*.sh specs/evidence/*.sh specs/tools/*.sh specs/tools/*.py 2>/dev/null | tr '\n' ' ')"}

check() {
  [ -s "$LEDGER" ] || { echo "✗ 闸依赖缺失：$LEDGER 不存在或为空 —— 无可比对象，闸坏了"; return 2; }
  _bad=0; _n=0
  while IFS="$(printf '\t')" read -r id dom sev st disp file brief; do
    case "$id" in ''|'#'*) continue;; esac
    _n=$((_n+1))
    [ -z "$brief" ] && continue
    # 取摘要前 18 字符作指纹（避开省略号与换行）
    _fp=$(printf '%s' "$brief" | cut -c1-18)
    [ -z "$_fp" ] && continue
    if ! grep -qF "$_fp" $DOCS "$HIST" $GATES 2>/dev/null; then
      [ "$_bad" -eq 0 ] && echo "✗ 有 RV 标注的原文被改写（台账摘要在正文/沿革件中已找不到）："
      echo "    $id（$dom·$sev·$st）原文起始：$_fp"
      _bad=$((_bad+1))
    fi
  done < "$LEDGER"
  if [ "$_bad" -gt 0 ]; then
    echo "  🔴 **标注是 reviewer-owned 的证据，执笔方不得改写其正文** —— 清理/替换工具须排除【RV…】段。"
    echo "  ⇒ 从评审方分支取回原文，或在沿革件中原样保留后再改。"
    return 1
  fi
  # 🔴 台账第 5 列（执笔方处置）须与正文标记同步 ——
  #   执笔方落完处置只跑了闸、没重生成台账，于是台账停在「未处置」而正文已是「采纳」。
  #   ⚠️ 是新建的 docs/REVIEW-STATE.md 第一次运行时抓出来的：
  #     **状态一旦有两份，就必然有一份是旧的。**
  _desync=0
  while IFS="$(printf '\t')" read -r id dom sev st disp file brief; do
    case "$id" in ''|'#'*) continue;; esac
    _doc=$(grep -ohE "【$id\|[^|]+\|[^|]+\|(未决|采纳|驳回|挂账)】" $DOCS "$HIST" $GATES 2>/dev/null \
           | head -1 | sed 's/.*|//; s/】//')
    [ -z "$_doc" ] && continue
    if [ "$_doc" != "$disp" ]; then
      [ "$_desync" -eq 0 ] && echo "✗ 台账第 5 列与正文标记不同步（状态有两份，必有一份是旧的）："
      echo "    $id 正文=【$_doc】 台账=【$disp】"
      _desync=$((_desync+1))
    fi
  done < "$LEDGER"
  [ "$_desync" -gt 0 ] && { echo "  ⇒ 重生成台账后再提交。"; return 1; }
  echo "  ✓ 台账内 $_n 条 RV 的原文均未被改写"
  echo "  ⚠️ 本闸判「摘要前缀是否原样存在」，判不了「整段一字未改」—— 它抓的是被工具批量改写这一类。"
  return 0
}

selftest() {
  T=$(mktemp -d) || return 2
  n=0; fail=0
  ok(){ n=$((n+1)); echo "  ✓ $1"; }; bad(){ echo "  ✗ 反测未过：$1"; fail=1; }
  L="$T/l.txt"
  printf 'RV-丙05\t丙\t非阻断\t未决\t采纳\tx.md\t本行「三级映射」沿用旧模型名——后略\n' > "$L"
  mk(){ : > "$T/h.md"; : > "$T/g.sh"; }

  mk; printf '【RV-丙05|丙|非阻断|采纳】本行「三级映射」沿用旧模型名——后略\n' > "$T/a.md"   # rv-fixture
  LEDGER="$L" DOCS="$T/a.md" HIST="$T/h.md" GATES="$T/g.sh" check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "原文未改时放行" || bad "原文未改却被拦"

  mk; printf '【RV-丙05|丙|非阻断|采纳】本行「§21.1 双层结构」沿用旧模型名——后略\n' > "$T/a.md"   # rv-fixture
  LEDGER="$L" DOCS="$T/a.md" HIST="$T/h.md" GATES="$T/g.sh" check >/dev/null 2>&1
  [ $? -eq 1 ] && ok "🔴 拦住：标注正文被全文替换改写（正是丙域抓到的那一类）" || bad "被改写的标注放行了"

  mk; : > "$T/a.md"; printf '沿革：RV-丙05 原文「本行「三级映射」沿用旧模型名——后略」，已采纳。\n' > "$T/h.md"
  LEDGER="$L" DOCS="$T/a.md" HIST="$T/h.md" GATES="$T/g.sh" check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：标注已移入沿革件且原文保留时放行" || bad "落沿革仍被拦"

  mk; : > "$T/a.md"
  LEDGER="$L" DOCS="$T/a.md" HIST="$T/h.md" GATES="$T/g.sh" check >/dev/null 2>&1
  [ $? -eq 1 ] && ok "拦住：标注整条消失（原文既不在正文也不在沿革）" || bad "标注消失被放行"

  : > "$L"
  LEDGER="$L" DOCS="$T/a.md" HIST="$T/h.md" GATES="$T/g.sh" check >/dev/null 2>&1
  [ $? -eq 2 ] && ok "拦住：台账为空判闸坏（无可比对象不得当免检）" || bad "空台账被当免检"

  rm -rf "$T"
  [ $fail -eq 0 ] && echo "--- 反测 $n 项全过（闸有牙）" || echo "--- 反测未过（闸没牙）"
  return $fail
}

case "${1:-}" in --selftest) selftest; exit $? ;; esac
check; exit $?
