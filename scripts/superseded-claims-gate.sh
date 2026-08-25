#!/bin/sh
# 已撤销断言闸 —— 裁定改了，引用点必须跟着改。
#
# 立此闸的直接原因（R2 轮1，三域【各自独立】标出同一处）：
#   C.3 重裁为 C.3a 后，§22.2c 仍写「只做受控仓储 API，不叠 RLS」——与 C.3a 正面两存。
#   同型在本轮已复发四次：C.3a · §21.1 租户双层 · D1→D0 晋升关闭 · §15.5 企业间无交流。
#
# 🔴 **执笔方在写 C.3a 那笔提交时，亲手写下「这属『改规则未同步引用点』，此处显式绕开并留记」——
#    然后就真的绕开了。留记不是修；写下警告不等于让警告生效。**
#
# ⚠️ 能力边界，写明不假装：
#   本闸判的是「**清单里的旧表述还在不在正文**」，判不了：
#     · 清单是否完备（漏登记的旧断言它看不见）
#     · 新表述本身对不对
#   🔴 **不得因本闸通过而宣称「全部引用点已同步」** —— 只能说「清单内的旧断言已清零」。
set -u

LIST=${LIST:-specs/gates/superseded-claims.txt}
DOCS=${DOCS:-'docs/dingwei-platform-specdoc.md docs/dingwei-architecture-design.md docs/dingwei-prototype.html'}
SKIP='沿革|历史时点|style-ok:|【RV\||已被取代|已撤销|重裁|旧模型名|旧口径'

check() {
  [ -f "$LIST" ] || { echo "✗ 闸依赖缺失：$LIST 不存在 —— 闸坏了，不是免检"; return 2; }
  _n=$(sed 's/#.*//' "$LIST" | awk -F'\t' 'NF>=3' | wc -l | tr -d ' ')
  [ "$_n" -eq 0 ] && { echo "✗ $LIST 无有效条目（须 <旧断言> TAB <现行> TAB <依据>）—— 闸坏了"; return 2; }

  _bad=0
  for f in $DOCS; do
    [ -f "$f" ] || { echo "✗ 闸依赖缺失：$f 不存在"; return 2; }
    _hit=0
    while IFS="$(printf '\t')" read -r old cur why; do
      case "$old" in ''|'#'*) continue;; esac
      _lines=$(grep -nF "$old" "$f" 2>/dev/null | grep -vE "$SKIP")
      [ -z "$_lines" ] && continue
      _c=$(printf '%s\n' "$_lines" | wc -l | tr -d ' ')
      [ "$_hit" -eq 0 ] && { echo "✗ $f：出现已撤销的断言"; _hit=1; _bad=1; }
      echo "    「$old」$_c 处 ⇒ 现行：$cur"
      printf '%s\n' "$_lines" | head -3 | cut -c1-110 | sed 's/^/       /'
      [ "$_c" -gt 3 ] && echo "       …（共 $_c 处）"
    done <<EOF
$(sed 's/#.*//' "$LIST" | awk -F'\t' 'NF>=3')
EOF
    [ "$_hit" -eq 0 ] && echo "  ✓ $f 无清单内的已撤销断言"
  done

  if [ "$_bad" -ne 0 ]; then
    echo "  🔴 裁定改了，引用点必须跟着改 —— 「留记」不算修。"
    echo "  ⚠️ 确属沿革叙述者，行内加 style-ok: <理由>，或改写为明确的历史时点表述。"
    return 1
  fi
  echo "  ✓ 清单内 $_n 条已撤销断言在正文均已清零"
  echo "  ⚠️ 本闸判不了【清单是否完备】—— 漏登记的旧断言它看不见，那仍归门方。"
  return 0
}

selftest() {
  T=$(mktemp -d) || return 2
  n=0; fail=0
  ok(){ n=$((n+1)); echo "  ✓ $1"; }; bad(){ echo "  ✗ 反测未过：$1"; fail=1; }
  L="$T/l.txt"; printf '旧说法\t新说法\t某依据\n' > "$L"
  mk(){ printf '# 正文\n\n应当如何。\n' > "$T/a.md"; }
  run(){ LIST="$L" DOCS="$T/a.md" check >/dev/null 2>&1; }

  mk; run; [ $? -eq 0 ] && ok "干净正文放行（不误报）" || bad "干净正文被误判"

  mk; printf '本节采用旧说法处理。\n' >> "$T/a.md"; run
  [ $? -eq 1 ] && ok "🔴 拦住：正文出现已撤销断言" || bad "已撤销断言被放行"

  mk; printf '沿革：曾采用旧说法。\n' >> "$T/a.md"; run
  [ $? -eq 0 ] && ok "不误伤：沿革叙述里的旧断言放行" || bad "沿革叙述被误拦"

  mk; printf '本节仍写旧说法 <!-- style-ok: 引述历史 -->\n' >> "$T/a.md"; run
  [ $? -eq 0 ] && ok "不误伤：带 style-ok 理由的例外放行" || bad "带理由的例外被误拦"

  mk; printf '【RV|甲|阻断|未决】本节仍写旧说法，须改\n' >> "$T/a.md"; run
  [ $? -eq 0 ] && ok "不误伤：RV 标注里引述旧断言不算违规（两闸不打架）" || bad "RV 标注被当违规"

  : > "$L"; mk; run
  [ $? -eq 2 ] && ok "拦住：清单为空判闸坏（空清单不得当免检）" || bad "空清单被当免检"

  printf '旧说法\t新说法\t某依据\n' > "$L"
  LIST="$T/nosuch" DOCS="$T/a.md" check >/dev/null 2>&1
  [ $? -eq 2 ] && ok "拦住：清单缺失判闸坏（非免检）" || bad "清单缺失被当免检"

  rm -rf "$T"
  [ $fail -eq 0 ] && echo "--- 反测 $n 项全过（闸有牙）" || echo "--- 反测未过（闸没牙，不得依赖它的 ✓）"
  return $fail
}

case "${1:-}" in --selftest) selftest; exit $? ;; esac
check; exit $?
