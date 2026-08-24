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
  # 状态类文件【就该同名】：它是拉取式状态源，同名才让人找得到，
  # 且它自带「生成自 commit」可自判过期。规则对它不适用 —— 这是规则的边界，不是例外网开一面。
  case "$b" in DELIVERY-STATUS.md|README*) continue ;; esac
  [ -f "$D/$b" ] || continue
  # 同名是否有害，取决于【仓内文件名会不会烂】：
  #   仓内名带的版本 == 该文件内容自述的版本 ⇒ 名字没烂，同名无歧义，放行；
  #   两者不等（如 dingwei-prototype-v0.1.html 里装着 v1.13）⇒ 按路径名找会命中旧件，必须改名投递。
  # ⚠️ 判据是「名字有没有烂」，不是「有没有版本号」——后者挡不住 v0.1 里装 v1.13 这种。
  rnv=$(printf '%s' "$b" | grep -oE 'v[0-9]+\.[0-9]+' | head -1)
  rcv=$(grep -m1 -oE '\*\*版本\*\*: v[0-9]+\.[0-9]+' "$p" 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+')
  [ -n "$rcv" ] || rcv=$(grep -m1 -oE '原型 v[0-9]+\.[0-9]+' "$p" 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+')
  if [ -n "$rnv" ] && [ "$rnv" = "$rcv" ]; then
    echo "  ✓ $b 同名但仓内名未烂（名 $rnv ＝ 内容 $rcv），无歧义"
  else
    echo "  ✗ $b 与仓内路径同名，且仓内名已烂（名 ${rnv:-无} vs 内容 ${rcv:-未知}）—— 投递须改名"; FAIL=1
  fi
done
[ "$FAIL" -eq 0 ] && echo "  ✓ 无同名件"

echo "③ README 是否声明了「当前有效件」"
if grep -q "当前有效件" "$D"/README* 2>/dev/null; then echo "  ✓ 有"; else echo "  ✗ 缺「当前有效件」段"; FAIL=1; fi

[ "$FAIL" -eq 0 ] && echo "--- 投递目录自检通过" || echo "--- 投递目录自检未过"
exit $FAIL
