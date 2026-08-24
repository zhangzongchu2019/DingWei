#!/bin/sh
# 投递目录自检 —— 防「同名歧义」与「文件名与内容版本不符」。
#
# 立此闸的直接原因（2026-08-24，两次）：
#   ① 共享目录里 dingwei-prototype-v0.1.html 装的是 v1.6，而该名与仓内路径同名 ⇒ 按路径名找会命中旧件
#   ② 同一个坑在 SpecDoc 侧还有一份（dingwei-platform-specdoc-v2.0.md 装的是 v3.3），
#      本席修完原型侧没回头扫 —— 「改了规矩没同步引用点」。
# 靠记性会漏第二处，靠这个脚本不会。
#
# 用法: scripts/delivery-check.sh <投递目录>
set -u
D=${1:-}
[ -n "$D" ] && [ -d "$D" ] || { echo "用法: $0 <投递目录>"; exit 2; }
FAIL=0

echo "① 文件名声明的版本 vs 文件内容的版本"
for f in "$D"/*.md "$D"/*.html; do
  [ -f "$f" ] || continue
  case "$(basename "$f")" in SUPERSEDED-*|README*|*-REVIEW-*|*REREVIEW*|*FINAL*|speclint*) continue ;; esac
  nver=$(basename "$f" | grep -oE 'v[0-9]+\.[0-9]+' | head -1)
  cver=$(grep -m1 -oE '\*\*版本\*\*: v[0-9]+\.[0-9]+' "$f" 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+')
  [ -z "$cver" ] && cver=$(grep -m1 -oE '原型 v[0-9]+\.[0-9]+' "$f" 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+')
  [ -z "$nver" ] || [ -z "$cver" ] && continue
  if [ "$nver" != "$cver" ]; then
    echo "  ✗ $(basename "$f"): 文件名说 $nver，内容是 $cver"; FAIL=1
  else
    echo "  ✓ $(basename "$f") $cver"
  fi
done

echo "② 是否存在与仓内路径同名的投递件（按路径名找会命中它）"
for p in $(git ls-files 'docs/*.md' 'docs/*.html' 2>/dev/null); do
  b=$(basename "$p")
  if [ -f "$D/$b" ]; then
    echo "  ✗ $b 与仓内路径同名 —— 改名带版本号，或加 SUPERSEDED- 前缀"; FAIL=1
  fi
done
[ "$FAIL" -eq 0 ] && echo "  ✓ 无同名件"

echo "③ README 是否声明了「当前有效件」"
if grep -q "当前有效件" "$D"/README* 2>/dev/null; then echo "  ✓ 有"; else echo "  ✗ 缺「当前有效件」段"; FAIL=1; fi

[ "$FAIL" -eq 0 ] && echo "--- 投递目录自检通过" || echo "--- 投递目录自检未过"
exit $FAIL
