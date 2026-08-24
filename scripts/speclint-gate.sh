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
echo "✓ 三道门全过"
