#!/bin/sh
# SpecDoc 三道门 —— 正文 lint / 语料 selftest / 变异体。任一不过即失败。
#
# 立此闸的直接原因：2026-08-24，本席在 speclint 退出码为 1 的状态下推了 a165521。
# 当时 lint 与 commit 写在同一批命令里、没有用 && 串，照跑不误。
# 纪律靠记性，机制靠这个脚本 —— 这也是 §29「不靠纪律靠机制」对本仓自身的应用。
#
# 用法：scripts/speclint-gate.sh        （仓根执行）
# 装成本机钩子：ln -sf ../../scripts/speclint-gate.sh .git/hooks/pre-commit
set -u
D=docs
T=$D/tools
DOC=$D/dingwei-platform-specdoc-v2.0.md
COR=$T/speclint-corpus.md
LINT=$T/speclint.py

[ -f "$LINT" ] || { echo "跳过：$LINT 不存在"; exit 0; }
command -v python3 >/dev/null 2>&1 || { echo "✗ 缺 python3"; exit 1; }

FAIL=0
echo "① 正文 lint";       python3 "$LINT" "$DOC"            || FAIL=1
echo "② 语料 selftest";   python3 "$LINT" --selftest "$COR" || FAIL=1
echo "③ 变异体";          python3 "$LINT" --mutate   "$COR" || FAIL=1

if [ "$FAIL" -ne 0 ]; then
  echo "✗ 三道门未过。修完再提交；确需绕过用 git commit --no-verify，"
  echo "  且须在提交信息里写明绕过理由 —— 堵死绕行不如让绕行留痕。"
  exit 1
fi
# ④ 原型改了就必须 bump 件内自述版本。
#    立此判据的直接原因：delivery-check.sh 本是为「件内版本 vs 文件名版本不符」而写，
#    可它跑在【投递时】—— 那时错已进仓。同一个错隔一个 commit 就复发了。
#    ⇒ 闸要装在错能发生的地方，不是错被发现的地方。
PROTO=docs/dingwei-prototype-v0.1.html
if git diff --cached --name-only 2>/dev/null | grep -qx "$PROTO"; then
  NEW=$(grep -o "原型 v[0-9]*\.[0-9]*" "$PROTO" | head -1)
  OLD=$(git show HEAD:"$PROTO" 2>/dev/null | grep -o "原型 v[0-9]*\.[0-9]*" | head -1)
  if [ -n "$OLD" ] && [ "$NEW" = "$OLD" ]; then
    echo "✗ 原型内容已改，但件内自述版本仍为 $NEW —— 请 bump 后再提交"
    echo "  （评审方打开页面看到的是标题里的版本；不 bump 等于告诉他这是旧件）"
    exit 1
  fi
  echo "  ④ 原型版本 $OLD → $NEW ✓"
fi
echo "✓ 三道门全过"
