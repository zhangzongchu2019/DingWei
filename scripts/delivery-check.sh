#!/bin/sh
# 投递目录自检 —— 防「同名歧义」与「文件名与内容版本不符」。
#
# 立此闸的直接原因（2026-08-24，两次）：
#   ① 共享目录里 dingwei-prototype.html 装的是 v1.6，而该名与仓内路径同名 ⇒ 按路径名找会命中旧件
#   ② 同一个坑在 SpecDoc 侧还有一份（dingwei-platform-specdoc.md 装的是 v3.3），
#      本席修完原型侧没回头扫 —— 「改了规矩没同步引用点」。
# 靠记性会漏第二处，靠这个脚本不会。
#
# 用法: scripts/delivery-check.sh <投递目录>
set -u
D=${1:-}
[ -n "$D" ] && [ -d "$D" ] || { echo "用法: $0 <投递目录>"; exit 2; }
FAIL=0

echo "① 投递名的版本 vs 件内自述版本"
# 🔴 T-2（丙域 kimi 终审）重写:去版本号新规落地后,原规则整体失效 ——
#    ② 把「仓内名无版本号」一律判「名已烂」,对 5 件全误报;
#    ① 对无版本号文件零输出、静默;③ 只认 README 不认 MANIFEST。
#    ⇒ 判据重定:【仓内不带版本号(避免名字烂)】+【投递名带版本号(避免同目录多版歧义)】,
#      两侧规则相反是有意的,不是矛盾。
# 🔴 两种投递目录，判据不同 —— 这是【形态区分】，不是给自己开例外：
#   A 滚动投递目录：同一份东西会投多版 ⇒ 投递名必须带版本号，否则同目录多版无法区分
#   B 锚定快照包：一次导出、统一锚、MANIFEST 逐件钉 sha256 ⇒ 每件只有一版，同名无歧义
#     ⚠️ B 的代价更高：MANIFEST 必须覆盖目录内每一件且 sha256 全部对得上（见④），
#        少一件或对不上即不成立 —— 比「名字带版本号」严格，不是更宽松。
SNAP=0
if [ -f "$D/MANIFEST.md" ] && grep -q "统一锚" "$D/MANIFEST.md" 2>/dev/null; then
  SNAP=1; echo "  ⓘ 本目录为【锚定快照包】（有 MANIFEST 与统一锚）—— 同名判据见②④"
fi
FOUND=0
for f in "$D"/*.md "$D"/*.html; do
  [ -f "$f" ] || continue
  b=$(basename "$f")
  case "$b" in SUPERSEDED-*|README*|MANIFEST*|DELIVERY-STATUS.md|*-REVIEW-*|*REREVIEW*|*FINAL*|CHECKLIST-*|speclint*) continue ;; esac
  FOUND=$((FOUND+1))
  nver=$(printf '%s' "$b" | grep -oE 'v[0-9]+\.[0-9]+' | head -1)
  # ⚠️ 只在【件头 40 行】取版本自述：正文里提到别的件的版本会被误判成它自己的
  #    —— 丙域终审时本席这条就把某 checklist 引用的「原型 v1.8」当成了它的版本。
  head_txt=$(head -40 "$f")
  cver=$(printf '%s' "$head_txt" | grep -m1 -oE '\*\*版本\*\*: v[0-9]+\.[0-9]+' | grep -oE 'v[0-9]+\.[0-9]+')
  [ -n "$cver" ] || cver=$(printf '%s' "$head_txt" | grep -m1 -oE '<title>[^<]*v[0-9]+\.[0-9]+' | grep -oE 'v[0-9]+\.[0-9]+' | head -1)
  if [ -z "$cver" ]; then
    echo "  – $b：件内无版本自述，跳过版本比对（非通过）"
  elif [ -z "$nver" ]; then
    if [ "$SNAP" -eq 1 ]; then
      echo "  ✓ $b $cver（快照包，版本由 MANIFEST 钉）"
    else
      echo "  ✗ $b：件内为 $cver，但投递名不带版本号 —— 滚动目录同名会歧义"; FAIL=1
    fi
  elif [ "$nver" != "$cver" ]; then
    echo "  ✗ $b：投递名 $nver ≠ 件内 $cver"; FAIL=1
  else
    echo "  ✓ $b $cver"
  fi
done
[ "$FOUND" -eq 0 ] && { echo "  ✗ 目录中没有可比对的正文件 —— 投递自检无对象，不得判通过"; FAIL=1; }

echo "② 是否存在与仓内路径同名的投递件（按路径名找会命中它）"
SAME=0
for p in $(git ls-files 'docs/*.md' 'docs/*.html' 'specs/*/*.md' 2>/dev/null); do
  b=$(basename "$p")
  case "$b" in DELIVERY-STATUS.md|README*) continue ;; esac
  [ -f "$D/$b" ] || continue
  SAME=$((SAME+1))
  if [ "$SNAP" -ne 1 ]; then
    echo "  ✗ $b 与仓内路径同名 —— 滚动目录须带版本号"; FAIL=1
  fi
done
if [ "$SAME" -eq 0 ]; then echo "  ✓ 无同名件"
elif [ "$SNAP" -eq 1 ]; then echo "  ✓ 快照包允许同名（$SAME 件），凭 MANIFEST 逐件钉指纹区分"; fi

echo "③ README 是否声明了「当前有效件」"
# 🔴 清单可以是 README 的「当前有效件」段，也可以是 MANIFEST.md（丙域 kimi：原只认前者）
if grep -q "当前有效件" "$D"/README* 2>/dev/null; then
  echo "  ✓ README 有「当前有效件」段"
elif [ -f "$D/MANIFEST.md" ] && grep -q "sha256" "$D/MANIFEST.md"; then
  echo "  ✓ MANIFEST.md 带指纹清单"
else
  echo "  ✗ 既无 README「当前有效件」段，也无带指纹的 MANIFEST.md"; FAIL=1
fi

if [ "$SNAP" -eq 1 ]; then
  echo "④ 快照包判据：MANIFEST 须覆盖每一件且 sha256 全部对得上"
  miss=0; bad=0
  for f in "$D"/*; do
    [ -f "$f" ] || continue
    b=$(basename "$f")
    case "$b" in MANIFEST.md|README*) continue ;; esac
    if ! grep -qF "\`$b\`" "$D/MANIFEST.md" 2>/dev/null; then
      echo "  ✗ $b 未列入 MANIFEST"; miss=$((miss+1)); FAIL=1; continue
    fi
    want=$(grep -F "\`$b\`" "$D/MANIFEST.md" | grep -oE "[0-9a-f]{64}" | head -1)
    got=$(sha256sum "$f" | cut -d" " -f1)
    [ "$want" = "$got" ] || { echo "  ✗ $b sha256 不符（MANIFEST ${want:-无} / 实算 $got）"; bad=$((bad+1)); FAIL=1; }
  done
  [ $miss -eq 0 ] && [ $bad -eq 0 ] && echo "  ✓ MANIFEST 覆盖完整且指纹全部一致"
fi

[ "$FAIL" -eq 0 ] && echo "--- 投递目录自检通过" || echo "--- 投递目录自检未过"
exit $FAIL
