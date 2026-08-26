#!/bin/sh
# §18.1 帖子列集一致闸（提交闸 ㉘）—— 列清单须【机器可唯一提取】，且三处一致。
#
# 🔴 立此闸的直接原因（coordinator 实测）：同一节三种抽取法得 **19 / 19 / 21**；
#   原型自述「以 §18.1 为准（12 列）」。⇒ **没有一个可唯一提取的权威清单**，
#   于是「以 §18.1 为准」无处落，下游按列集做的实现与负测**各取一份**。
#   ⚠️ 问题不是「12 还是 19 还是 21」，是**判据与被判物不同源**。
#
# 判据三条：
#   ① 权威清单块（POST-COLUMNS-BEGIN/END）存在且非空 —— 取不到就不能报通过；
#   ② 该块所列 == §18.1 表格实际列（逐个对撞，报出差集）；
#   ③ 原型自述的列数 == 权威清单列数。
#
# ⚠️ 能力边界：本闸判【三处列集是否一致】，🔴 **判不了【列定义对不对】**（该不该有某列、
#   某列该不该入 digest）—— 那是设计判断，归门方。**本闸过 ≠ 列集设计正确。**
set -u
SELF=$(cd "$(dirname "$0")" && pwd)/$(basename "$0")
SPEC=${SPEC:-docs/dingwei-platform-specdoc.md}
PROTO=${PROTO:-docs/dingwei-prototype.html}

authoritative() {
  awk '/POST-COLUMNS-BEGIN/{f=1;next} /POST-COLUMNS-END/{f=0} f && !/^<!--/ && NF' "$SPEC" | tr ' ' '\n' | awk 'NF'
}
table_cols() {
  awk '/^### 18\.1/,/^### 18\.2/' "$SPEC" \
    | grep -oE '^\| \*?\*?`[a-z_]+(\[\])?`' | sed 's/^| \**//; s/`//g' | awk 'NF'
}

check() {
  [ -f "$SPEC" ] || { echo "✗ 闸依赖缺失：$SPEC —— 走不到就不能报数"; return 2; }
  _a=$(authoritative); _an=$(printf '%s\n' "$_a" | awk 'NF' | wc -l | tr -d ' ')
  [ "${_an:-0}" -eq 0 ] && { echo "✗ §18.1 权威列清单块取不到或为空（POST-COLUMNS-BEGIN/END）—— 取不到就不能报通过"; return 2; }
  _t=$(table_cols); _tn=$(printf '%s\n' "$_t" | awk 'NF' | wc -l | tr -d ' ')
  [ "${_tn:-0}" -eq 0 ] && { echo "✗ §18.1 表格列解析不出 —— 解析不出就不能报通过（格式可能变了）"; return 2; }

  _bad=0
  _only_a=$(printf '%s\n' "$_a" | sort | comm -23 - "$(printf '%s\n' "$_t" | sort > /tmp/.pc$$; echo /tmp/.pc$$)")
  _only_t=$(printf '%s\n' "$_t" | sort | comm -13 "$(printf '%s\n' "$_a" | sort > /tmp/.pa$$; echo /tmp/.pa$$)" -)
  rm -f /tmp/.pc$$ /tmp/.pa$$
  if [ -n "$_only_a" ] || [ -n "$_only_t" ]; then
    echo "✗ §18.1 权威清单与表格不一致（清单 $_an · 表格 $_tn）"
    [ -n "$_only_a" ] && echo "    只在清单里：$(printf '%s' "$_only_a" | tr '\n' ' ')"
    [ -n "$_only_t" ] && echo "    只在表格里：$(printf '%s' "$_only_t" | tr '\n' ' ')"
    _bad=1
  fi
  if [ -f "$PROTO" ]; then
    # 🔴 v2（丙域升阻断 ＋ 归口方复核后自查）：v1 只认「以 §18.1 为准（N 列」这一种写法，
    #   而原型实际有两种（"以 §18.1 为准（12 列" / "§18.1 已正面定义 12 列"），**一处都没匹配上**；
    #   更坏的是 v1 在零命中时**跳过并打印「原型自述 $(printf "%s" "$_pl" | tr "\n" "/") 列逐个已核」** —— 🔴 **空集当通过，还自称已核**。
    #   本席刚立的 GATE-CONTRACT 通则，本席自己的闸就在违反。
    #   ⇒ v2 改为：取【与 §18.1 同行】的所有「N 列」，逐个比对；**一处都取不到即 UNKNOWN(exit 2)**，不折叠为 PASS。
    _pl=$(grep -n '§18\.1' "$PROTO" | grep -oE '[0-9]+ *列' | grep -oE '[0-9]+' | sort -u)
    if [ -z "$_pl" ]; then
      echo "✗ 原型中未取到任何与 §18.1 同行的列数自述 —— 取不到就不能报「已核」（GATE-CONTRACT 三态：UNKNOWN 不折叠为 PASS）"
      rm -f /tmp/.pc$$ /tmp/.pa$$ 2>/dev/null
      return 2
    fi
    for _pn in $_pl; do
      if [ "$_pn" != "$_an" ]; then
        echo "✗ 原型自述列数 $_pn ≠ §18.1 权威清单列数 $_an —— 「以 §18.1 为准」无处落"
        _bad=1
      fi
    done
  fi
  [ "$_bad" -gt 0 ] && { echo "  ⇒ 🔴 判据与被判物必须同源可算：清单是唯一提取点，表格与原型都须与它对齐。"; return 1; }
  echo "  ✓ §18.1 列集三处一致（权威清单 $_an 列，表格 $_tn 列，原型自述 $(printf "%s" "$_pl" | tr "\n" "/") 列逐个已核）"
  echo "  ⚠️ 本闸判【三处是否一致】，🔴 判不了【列定义对不对】（该不该有某列、该不该入 digest）—— 归门方。"
  return 0
}

selftest() {
  T=$(mktemp -d) || { echo "✗ 夹具目录建不出 ⇒ selftest FAIL"; return 2; }
  n=0; fail=0
  ok(){ n=$((n+1)); echo "  ✓ $1"; }; bad(){ echo "  ✗ 反测未过：$1"; fail=1; }
  mk(){ set -f
        { echo "### 18.1 x"; echo "| 列 | 用途 |"; for c in $1; do echo "| \`$c\` | u |"; done
          echo "<!-- POST-COLUMNS-BEGIN -->"; echo "$2"; echo "<!-- POST-COLUMNS-END -->"; echo "### 18.2 y"; } > "$T/s.md"
        _c=$(awk '/^### 18\.1/,/^### 18\.2/' "$T/s.md" | grep -cE '^\| `[a-z_]+`')
        [ "$_c" -eq "$(echo $1|wc -w)" ] || { set +f; echo "✗ 夹具自检失败（表格解析 $_c）⇒ selftest FAIL"; exit 2; }
        set +f; }
  mkp(){ printf '以 §18.1 为准（%s 列）\n' "$1" > "$T/p.html"; }

  mk "a b c" "a b c"; mkp 3; SPEC="$T/s.md" PROTO="$T/p.html" sh "$SELF" >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：三处一致时放行" || bad "一致被拦"

  mk "a b c d" "a b c"; mkp 3; SPEC="$T/s.md" PROTO="$T/p.html" sh "$SELF" >/dev/null 2>&1
  [ $? -eq 1 ] && ok "🔴 拦住：表格多一列而清单未同步（改一处漏一处）" || bad "表格漂移被放行"

  mk "a b c" "a b c"; mkp 12; SPEC="$T/s.md" PROTO="$T/p.html" sh "$SELF" >/dev/null 2>&1
  [ $? -eq 1 ] && ok "🔴 拦住：原型自述列数 ≠ 权威清单（正是实际发生的形态）" || bad "原型失真被放行"

  _o=$(mk "a b d" "a b c"; mkp 3; SPEC="$T/s.md" PROTO="$T/p.html" sh "$SELF" 2>&1)
  printf '%s' "$_o" | grep -q '只在清单里' && printf '%s' "$_o" | grep -q '只在表格里' \
    && ok "差集双向报出（不是只说『不一致』）" || bad "差集未双向报出"

  mk "a b c" ""; mkp 3; SPEC="$T/s.md" PROTO="$T/p.html" sh "$SELF" >/dev/null 2>&1
  [ $? -eq 2 ] && ok "🔴 清单块为空 ⇒ exit 2（取不到就不能报通过）" || bad "空清单却报通过"

  # 🔴 v2 反测：原型有【多种写法】时须逐个比对（v1 只认一种，两处都没匹配上）
  mk "a b c" "a b c"
  { printf '以 §18.1 为准（3 列）\n'; printf '§18.1 已正面定义 12 列字段集\n'; } > "$T/p.html"
  SPEC="$T/s.md" PROTO="$T/p.html" sh "$SELF" >/dev/null 2>&1
  [ $? -eq 1 ] && ok "🔴 原型多种写法：只要有一处列数不符即拒（v1 漏检了另一种写法）" \
                || bad "另一种写法的过期列数被漏检"

  # 🔴 v2 反测：原型中一处列数自述都取不到 ⇒ UNKNOWN(exit 2)，不得报「已核」
  mk "a b c" "a b c"; printf '本屏不提列数\n' > "$T/p.html"
  SPEC="$T/s.md" PROTO="$T/p.html" sh "$SELF" >/dev/null 2>&1
  [ $? -eq 2 ] && ok "🔴 原型列数自述取不到 ⇒ exit 2，不折叠为 PASS（v1 在此跳过并打印「已核」）" \
                || bad "零命中却报已核 —— 空集当通过"

  SPEC="$T/nofile.md" sh "$SELF" >/dev/null 2>&1
  [ $? -eq 2 ] && ok "🔴 件缺失 ⇒ exit 2" || bad "件缺失却报通过"

  _err=$( ( mk "a b c" "a b c"; mkp 3; SPEC="$T/s.md" PROTO="$T/p.html" sh "$SELF" >/dev/null ) 2>&1 )
  [ -z "$_err" ] && ok "🔴 通过时 stderr 干净" || bad "通过时有 stderr：$_err"

  rm -rf "$T"
  [ $fail -eq 0 ] && { echo "  列集一致闸反测 $n/$n ✓"; return 0; }
  return 1
}
case "${1:-}" in --selftest) selftest;; *) check;; esac
