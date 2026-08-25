#!/bin/sh
# 附录 E 认领闸 —— 「未认领恒为 0」必须能算出来。
#
# 立此闸的直接原因（甲域 sol · 乙域 terra 同指，R1→R2）：
#   §0.3 写「每个子 spec **必须认领**附录 E 中属于自己的行」，
#   并把「附录 E 中无任何子 spec 认领的行数」定为**恒零判据**。
#   而 E.1–E.4 的表**没有 owner 列**，全文也无独立 `E-ID → SP` 映射
#   ⇒ **该判据在结构上无从计算**，作不了 gate、验不了 SP0–SP8 全覆盖。
#
# 🔴 与本仓已归纳的「判据在结构上恒为假/无法成立」同族（前例：用 set 去 count 重复）。
#   **写下恒零判据而没有承载它的数据，等于没有判据。**
#
# ⚠️ 能力边界，写明不假装：
#   本闸判「每行有没有 owner_sp、值在不在受控枚举内」，
#   **判不了「认领得对不对」** —— 那要人读该行的实质，归门方。
#   🔴 **不得因本闸通过而宣称「附录 E 已被正确认领」**，只能说「每行都有主」。
set -u

DOC=${DOC:-docs/dingwei-platform-specdoc.md}
OWNERS='SP0 SP1 SP2 SP3 SP4 SP5 SP6 SP7a SP7b SP8 横切'

check() {
  [ -f "$DOC" ] || { echo "✗ 闸依赖缺失：$DOC 不存在 —— 闸坏了，不是免检"; return 2; }
  _out=$(awk -v owners="$OWNERS" '
    BEGIN{ n=split(owners,O," "); for(i=1;i<=n;i++) ok[O[i]]=1 }
    /^## E\.[134]([^0-9]|$)/ { intbl=1; next }
    /^## E\.[0-9]/ || /^# /   { intbl=0 }
    intbl && /^\|[[:space:]]*\*?\*?[0-9]/ {
      total++
      line=$0; sub(/[[:space:]]*$/,"",line)
      k=split(line,c,"|"); last=c[k-1]
      gsub(/^[[:space:]]+|[[:space:]]+$/,"",last)
      split(last,parts,"·"); good=1
      for(j=1;j<=length(parts);j++){ p=parts[j]; gsub(/[[:space:]]/,"",p); if(!(p in ok)) good=0 }
      if(last=="" || good==0){ bad++; if(bad<=5) printf "      %s\n", substr($0,1,90) }
    }
    END{ printf "TOTAL=%d BAD=%d\n", total, bad+0 }' "$DOC")
  _t=$(printf '%s\n' "$_out" | grep -o 'TOTAL=[0-9]*' | cut -d= -f2)
  _b=$(printf '%s\n' "$_out" | grep -o 'BAD=[0-9]*' | cut -d= -f2)

  if [ "${_t:-0}" -eq 0 ]; then
    echo "✗ 未在 $DOC 中找到附录 E.1/E.3/E.4 的数据行 —— 闸坏了，不是免检"; return 2
  fi
  echo "  附录 E 可认领行：$_t · 无主或 owner 非法：$_b"
  if [ "${_b:-0}" -gt 0 ]; then
    printf '%s\n' "$_out" | grep -v 'TOTAL='
    echo "✗ 「附录 E 中无任何子 spec 认领的行数」应恒为 0，实为 $_b。"
    echo "  🔴 恒零判据须有数据承载 —— 没有 owner_sp 列时，该判据在结构上无从计算。"
    echo "  ⚠️ owner_sp 取值须在受控枚举内：$OWNERS（多主用 · 连接）"
    return 1
  fi
  echo "  ✓ 附录 E 未认领行恒为 0（$_t 行全部有主）"
  echo "  ⚠️ 本闸判不了【认领得对不对】—— 那要读该行实质，归门方。"
  return 0
}

selftest() {
  T=$(mktemp -d) || return 2
  n=0; fail=0
  ok(){ n=$((n+1)); echo "  ✓ $1"; }; bad(){ echo "  ✗ 反测未过：$1"; fail=1; }
  hdr='## E.1 x

| # | 项 | 出处 | owner_sp |
|---|---|---|---|
'
  mk(){ printf '%s' "$hdr" > "$T/d.md"; }
  run(){ DOC="$T/d.md" check >/dev/null 2>&1; }

  mk; printf '| 1 | 甲 | §5.2 | SP2 |\n| 2 | 乙 | §21 | SP1 |\n## E.5 尾\n' >> "$T/d.md"; run
  [ $? -eq 0 ] && ok "全部有主时放行" || bad "全部有主却被拦"

  mk; printf '| 1 | 甲 | §5.2 | SP2 |\n| 2 | 乙 | §21 |\n## E.5 尾\n' >> "$T/d.md"; run
  [ $? -eq 1 ] && ok "🔴 拦住：有行无 owner_sp（恒零判据不成立）" || bad "无主行被放行"

  mk; printf '| 1 | 甲 | §5.2 | SP99 |\n## E.5 尾\n' >> "$T/d.md"; run
  [ $? -eq 1 ] && ok "拦住：owner_sp 不在受控枚举内" || bad "非法 owner 被放行"

  mk; printf '| 1 | 甲 | §5.2 | SP3·SP4 |\n## E.5 尾\n' >> "$T/d.md"; run
  [ $? -eq 0 ] && ok "不误伤：多主（SP3·SP4）合法" || bad "多主被误拦"

  mk; printf '## E.5 尾\n' >> "$T/d.md"; run
  [ $? -eq 2 ] && ok "拦住：找不到数据行判闸坏（空表不得当免检）" || bad "空表被当免检"

  DOC="$T/nosuch.md" check >/dev/null 2>&1
  [ $? -eq 2 ] && ok "拦住：件缺失判闸坏（非免检）" || bad "件缺失被当免检"

  rm -rf "$T"
  [ $fail -eq 0 ] && echo "--- 反测 $n 项全过（闸有牙）" || echo "--- 反测未过（闸没牙）"
  return $fail
}

case "${1:-}" in --selftest) selftest; exit $? ;; esac
check; exit $?
