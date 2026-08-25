#!/bin/sh
# specs/README.md 的结构表 vs 仓内 specs/ 实际内容 —— 两边都不许有余数。
#
# 立此闸的直接原因：README 的结构块原是手写树形，自称是 specs/ 的结构，
# 实际 15 件只列了 7 件 —— specs/evidence/ 三件与 specs/tools/export-bundle.sh 早已漏列，
# specs/gates/ 两件与 export-exclude.txt 是新增后未同步。
# 「改了没同步引用点」是本席复发最多的一条；靠记性同步索引，必然再漏。
# ⇒ 与 export-bundle.sh 同源：凡说「全部」，清单就不能只靠人手维护。
#
# 用法：scripts/specs-index-check.sh [README] ；--selftest 跑反测
set -u
DEF_README=specs/README.md

# 用 git ls-files（索引）而非工作树 —— 提交闸跑在 pre-commit，新件已入暂存区，
# 用工作树会把未暂存的临时文件也算进来，判据就跟着噪声走。
listed()  { sed -n 's/^| `\(specs\/[^`]*\)` |.*/\1/p' "$1" | sort -u; }
actual()  { git ls-files -c -- specs | sort -u; }

check() {
  _rm=$1
  [ -f "$_rm" ] || { echo "✗ 闸依赖缺失：$_rm 不存在"; return 2; }
  L=$(listed "$_rm"); A=$(actual)
  [ -z "$L" ] && { echo "✗ $_rm 的结构表里一条 specs/ 路径都没解析出来 —— 表被改坏了或格式变了，判闸坏"; return 2; }
  # ⚠️ 变量名用 _ct 前缀，不用 T —— check() 与 selftest() 都建临时目录，
  #    同名会让 check() 的 rm -rf 把 selftest 的临时目录删掉。
  #    这个撞车已经真发生过一次：反测第 ④ 项当时打了 ✓，而它是【假通过】——
  #    临时文件建不出来 → check 收到不存在的文件 → 返回 2，恰好等于该项的预期值。
  #    「闸报通过，但它测的不是它以为的东西」，正是本仓反复在点的那一类。
  _ct=$(mktemp -d); printf '%s\n' "$L" > "$_ct/l"; printf '%s\n' "$A" > "$_ct/a"
  MISS=$(comm -13 "$_ct/l" "$_ct/a")   # 仓内有、表里没有
  EXTRA=$(comm -23 "$_ct/l" "$_ct/a")  # 表里有、仓内没有
  rm -rf "$_ct"
  rc=0
  if [ -n "$MISS" ]; then
    echo "✗ 仓内有而 $_rm 未列（新增件没同步索引）："; printf '%s\n' "$MISS" | sed 's/^/    /'; rc=1
  fi
  if [ -n "$EXTRA" ]; then
    echo "✗ $_rm 列了但仓内没有（索引已漂移）："; printf '%s\n' "$EXTRA" | sed 's/^/    /'; rc=1
  fi
  [ $rc -eq 0 ] && echo "  ⑧ specs/ 索引与实际一致 ✓（$(printf '%s\n' "$A" | wc -l | tr -d ' ') 件，两边无余数）"
  return $rc
}

selftest() {
  T=$(mktemp -d); fail=0; n=0
  ok(){ n=$((n+1)); echo "  ✓ $1"; }; bad(){ echo "  ✗ 反测未过：$1"; fail=1; }

  # ① 现状必须通过 —— 否则后面几项不算数
  check "$DEF_README" >/dev/null 2>&1
  [ $? -eq 0 ] && ok "现状一致（闸不误报）" || bad "现状被误判为不一致"

  # ② 表里删掉一行 -> 必须被拦（这正是「新增件没同步索引」的形态）
  grep -v '`specs/gates/boundary-baseline.txt`' "$DEF_README" > "$T/less.md"
  check "$T/less.md" >/dev/null 2>&1
  [ $? -eq 1 ] && ok "拦住：仓内有而表里漏列" || bad "漏列未被拦"

  # ③ 表里多一条不存在的路径 -> 必须被拦（索引漂移，例如删了件没删索引）
  awk '1; /^\| `specs\/README.md`/ && !d {print "| `specs/nosuch/ghost.md` | 反测用,仓内并不存在 |"; d=1}' \
      "$DEF_README" > "$T/more.md"
  check "$T/more.md" >/dev/null 2>&1
  [ $? -eq 1 ] && ok "拦住：表里有而仓内没有（索引漂移）" || bad "漂移未被拦"

  # ④ 表被改坏到一条都解析不出 -> 判「闸坏了」而非通过
  grep -v '^| `specs/' "$DEF_README" > "$T/broken.md"
  # 先自证夹具真的建出来了 —— 否则 check 会因「文件不存在」返回 2，
  # 与本项的预期值撞上，于是打出一个【假 ✓】。夹具没建成就不是通过，是没测。
  if [ ! -s "$T/broken.md" ]; then bad "夹具未建成，本项【没测】，不得记通过"; else
    check "$T/broken.md" >/dev/null 2>&1
    [ $? -eq 2 ] && ok "表解析不出任何路径时判闸坏（非通过）" || bad "表被改坏却当成通过"
  fi

  rm -rf "$T"
  [ $fail -eq 0 ] && echo "--- 反测 $n 项全过（闸有牙）" || echo "--- 反测未过"
  return $fail
}

case "${1:-}" in --selftest) selftest; exit $? ;; esac
check "${1:-$DEF_README}"; exit $?
