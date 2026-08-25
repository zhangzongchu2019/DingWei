#!/bin/sh
# 冲突标记恒零闸 —— 交付件里不得存在未解决的 Git 合并冲突标记。
#
# 🔴 立此闸的直接原因（甲域 sol，R2 轮4 · RV-甲40）：
#   `docs/dingwei-prototype.html` 的导出函数里**提交了一段未解决的冲突**
#   （<<<<<<< HEAD / ======= / >>>>>>> origin/…，两变体仅差 v1.30 与 v1.27），
#   而提交闸 ①–⑰ 与 analyze **全绿**。
#   ⚠️ 该处恰好落在 JS 块注释内，所以语法没坏 —— 于是连「文件还能不能跑」都没暴露它。
#   🔴 **「全闸通过」在这里的含义是「没有一道闸看这个」**，不是「没问题」。
#
# ⚠️ 判据要按【行首精确形态】，不能用子串：
#   本仓 `docs/dingwei-*-charter-v1.*.md` 用 32 个 `=` 作分隔线，
#   若写成「含 ======= 即命中」会把它们全部误报。冲突标记是**行首、恰好 7 个字符**。
set -u

SCOPE=${SCOPE:-"docs/dingwei-platform-specdoc.md docs/dingwei-architecture-design.md docs/dingwei-prototype.html docs/dingwei-revision-history.md docs/DELIVERY-STATUS.md $(ls scripts/*.sh 2>/dev/null) $(ls specs/tools/*.py specs/tools/*.sh 2>/dev/null) $(ls specs/evidence/*.sh 2>/dev/null)"}
RE='^(<<<<<<< |=======$|>>>>>>> )'

check() {
  _bad=0; _n=0
  for f in $SCOPE; do
    [ -f "$f" ] || continue
    _n=$((_n+1))
    # ⚠️ 标注正文里会**引述**这些标记（RV-甲40 就引了）⇒ 只按行首判，引述在行中不受影响。
    _h=$(grep -anE "$RE" "$f" 2>/dev/null || true)
    if [ -n "$_h" ]; then
      [ "$_bad" -eq 0 ] && echo "✗ 交付件含未解决的 Git 冲突标记："
      printf '%s\n' "$_h" | sed "s|^|    $f:|" | cut -c1-120
      _bad=$((_bad+1))
    fi
  done
  if [ "$_bad" -gt 0 ]; then
    echo "  🔴 冲突标记可以藏在注释里，**语法不坏、闸也不看** ⇒ 「全闸通过」在此只意味着没人看这里。"
    echo "  ⇒ 解冲突、保留唯一现行实现后再提交；不得两存。"
    return 1
  fi
  echo "  ✓ 冲突标记恒零（$_n 件受检）"
  echo "  ⚠️ 本闸只判【标记形态】，判不了【合并选对了没有】—— 那归人审。"
  return 0
}

selftest() {
  T=$(mktemp -d) || return 2
  n=0; fail=0
  ok(){ n=$((n+1)); echo "  ✓ $1"; }; bad(){ echo "  ✗ 反测未过：$1"; fail=1; }

  printf '干净正文\n' > "$T/a.md"
  SCOPE="$T/a.md" check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：干净件放行" || bad "干净件被拦"

  for mk in '<<<<<<< HEAD' '=======' '>>>>>>> origin/x'; do
    printf '正文\n%s\n尾\n' "$mk" > "$T/a.md"
    SCOPE="$T/a.md" check >/dev/null 2>&1
    [ $? -eq 1 ] && ok "拦住：行首 $(printf '%s' "$mk" | cut -c1-7)" || bad "$mk 被放行"
  done

  # 🔴 正是 RV-甲40 的真实形态：标记落在块注释内，语法不坏
  printf '/* 说明\n<<<<<<< HEAD\nA\n=======\nB\n>>>>>>> origin/y\n*/\n' > "$T/a.md"
  SCOPE="$T/a.md" check >/dev/null 2>&1
  [ $? -eq 1 ] && ok "🔴 拦住：冲突标记藏在块注释内（甲域 RV-甲40 的真实形态）" || bad "注释内冲突被放行"

  # ⚠️ 不误伤：本仓 charter 件用 32 个 = 作分隔线
  printf '标题\n================================\n正文\n' > "$T/a.md"
  SCOPE="$T/a.md" check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：32 个 = 的分隔线不是冲突标记（子串判据会全部误报）" || bad "分隔线被误判为冲突"

  # ⚠️ 不误伤：RV 标注里【引述】这些标记（甲40 本身就引了）
  printf '【RV-甲40|甲|阻断|采纳】正文含 <<<<<<< / ======= / >>>>>>> 标记\n' > "$T/a.md"   # rv-fixture
  SCOPE="$T/a.md" check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：标注中行内引述标记不算冲突（只按行首判）" || bad "引述被误判"

  rm -rf "$T"
  [ $fail -eq 0 ] && echo "--- 反测 $n 项全过（闸有牙）" || echo "--- 反测未过（闸没牙）"
  return $fail
}

case "${1:-}" in --selftest) selftest;; *) check;; esac
