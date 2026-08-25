#!/bin/sh
# 状态一致性闸 —— supporting spec 内不得留与【权威状态源】矛盾的现状断言。
#
# 立此闸的直接原因（甲域 sol 结构域复核抓出）：
#   specs/platform/spec.md 在【同一文件内】自相矛盾 —— 门状态表已写「架构评审 PASS /
#   checklist 执行完毕」，同文件下方却仍写「生命周期仍为草案 / 须重走 / 不得被引用」；
#   specs/README.md 同样仍写「尚未执行，须重走」。
#   而 SpecDoc §0.1 权威字段早已是「生效」。
#   ⇒ 同一当前状态多处并存，改一处漏一处 —— 与 ARCH-03 抓过的同形，本轮第四次。
#
# 🔴 治法与前三次一致：**不靠人记得同步，靠机器不让**。
#   权威状态源唯一 = SpecDoc §0.1 的「生命周期」字段；其余件不得自行断言相反状态。
#
# ⚠️ 本闸的能力边界，写明不假装：
#   它按【短语】判，不理解语义。改写成同义的别种说法（例如「本件尚在评审中」）它看不见。
#   ⇒ 它拦的是复发最多的那几种字面形态，不是「所有不一致」。语义层归人审。
#   另：**reviewer-owned 的 checklist* 一律不扫** —— 执笔方不得代改门方的件（§4.1 · §7.2）。
#
#   ⚠️ 短语判据的固有毛病:**提到**这些短语的元文本(例如「此前『尚不得被引用』的限制已解除」)
#      会被误报。本闸不靠猜,给一个**显式行内标记**:该行含 `status-ok:` 即视为「提及而非断言」,
#      且标记后必须写理由。标记数每次运行都打印出来 —— 用标记把问题洗掉这条路,在日志里看得见。
#
# 用法：scripts/status-consistency-check.sh [--selftest]
set -u
SPECDOC=docs/dingwei-platform-specdoc.md
EXC=specs/gates/status-stale-exceptions.txt
# 复发最多的四种字面形态（与本轮实际抓到的一致，不臆造）
PAT='尚未执行|仍为草案|生命周期仍为「草案」|须重走|须按 `checklist.md` 重走|尚不得被|不得被任何子 spec 引用'

authoritative() { # 从权威源读生命周期状态
  sed -n 's/^\*\*生命周期\*\*: .*\*\*\([^*]*\)\*\*.*/\1/p' "$SPECDOC" | head -1
}

excepted() { # excepted <path:line>  -> 0=已登记例外
  sed 's/#.*//' "$EXC" | awk 'NF>0 {print $1}' | grep -qx "$1"
}

check() {
  [ -f "$SPECDOC" ] || { echo "✗ 闸依赖缺失：$SPECDOC 不存在 —— 无权威状态源即无从判定"; return 2; }
  [ -f "$EXC" ]     || { echo "✗ 闸依赖缺失：$EXC 不存在"; return 2; }
  LIFE=$(authoritative)
  [ -n "$LIFE" ] || { echo "✗ 权威状态源里读不到「生命周期」字段 —— 判闸坏，非通过"; return 2; }

  # 例外清单自校验：路径须真实存在、理由不得为空（同 export-exclude 的治法）
  _bad=0
  sed 's/#.*//' "$EXC" | awk 'NF>0' > /tmp/.sc.$$
  while IFS= read -r line; do
    _p=$(printf '%s' "$line" | awk '{print $1}')
    _why=$(printf '%s' "$line" | cut -s -d' ' -f2-)
    _f=${_p%%:*}
    [ -n "$_why" ] || { echo "✗ 例外没有理由：$_p —— 没有理由的例外等于默默豁免"; _bad=1; }
    [ -f "$_f" ]   || { echo "✗ 例外指向不存在的文件：$_f —— 清单漂移，闸坏"; _bad=1; }
  done < /tmp/.sc.$$
  rm -f /tmp/.sc.$$
  [ "$_bad" -eq 0 ] || return 2

  echo "  ⑨ 权威状态源（$SPECDOC §0.1）生命周期＝【$LIFE】"
  # 整份打印例外清单 —— 靠往清单里加行把问题洗掉这条路，必须在日志里看得见。
  _n=0
  sed 's/#.*//' "$EXC" | awk 'NF>0' | while IFS= read -r l; do echo "     例外：$l"; done
  _n=$(sed 's/#.*//' "$EXC" | awk 'NF>0' | wc -l | tr -d ' ')
  [ "$_n" -eq 0 ] && echo "     （例外清单为空）"

  case "$LIFE" in
    生效) ;;   # 仅在「生效」态下，下列过期断言才构成矛盾
    *) echo "  ⑨ 生命周期非「生效」，本闸不判过期断言（避免反向误伤）✓"; return 0;;
  esac

  # 先算全部命中,再据此做两件事:①判未登记的;②判【已失效的例外】。
  HITS=$(grep -rnE "$PAT" specs docs/dingwei-architecture-design.md --include=*.md 2>/dev/null \
        | grep -v '/checklist' \
        | grep -v "^$SPECDOC:" \
        | grep -v "^$EXC:" \
        | grep -v '^specs/gates/' \
        | grep -v '^specs/evidence/reference-index.md:' \
        | grep -v '^scripts/' \
        | grep -v 'status-ok:')
  rc=0; left=''
  for h in $(printf '%s\n' "$HITS" | awk -F: 'NF>0 {print $1":"$2}'); do
    excepted "$h" || { left="$left $h"; rc=1; }
  done
  # 🔴 例外必须【仍然对应一处真命中】,否则就是死例外。
  #   死例外的害处不是占地方,是它会在将来**遮住同一行号上新出现的真命中** ——
  #   而那时没人会想到「是不是有条旧例外还赖在这」。⇒ 失效即须删除,由闸来提醒。
  #   ⚠️ 这个洞是本闸上线当天就暴露的:上游改稿后我无法从闸的输出判断例外还需不需要。
  _stale=''
  for e in $(sed 's/#.*//' "$EXC" | awk 'NF>0 {print $1}'); do
    printf '%s\n' "$HITS" | awk -F: 'NF>0 {print $1":"$2}' | grep -qx "$e" || _stale="$_stale $e"
  done
  if [ -n "$_stale" ]; then
    echo "✗ 下列例外已失效（该位置已无过期断言），请从 $EXC 删除："
    for e in $_stale; do echo "    $e"; done
    echo "  留着死例外会在将来遮住同一行号上新出现的真命中。"
    return 1
  fi

  if [ "$rc" -eq 1 ]; then
    echo "✗ 生命周期已「生效」，但下列位置仍断言相反的现状（未登记例外）："
    for h in $left; do
      f=${h%%:*}; l=${h##*:}
      printf '    %s\n      %s\n' "$h" "$(sed -n "${l}p" "$f" | cut -c1-90)"
    done
    echo "  修法二选一：① 把该处改成与权威源一致；② 若属不应由执笔方改的件，登记进 $EXC 并写明理由与归属。"
    return 1
  fi
  # 🔴 status-ok 标记必须带【成形】理由：只写标记不写理由＝用标记把断言洗白
  #   （甲域 sol 实测：此前只 grep -v 整行跳过，空理由仍 rc=0 —— 声明大于实现）。
  _badmark=$(grep -rn 'status-ok:' specs docs/dingwei-architecture-design.md --include=*.md 2>/dev/null \
    | awk '{ r=$0; sub(/.*status-ok:/,"",r); gsub(/[ \t]/,"",r); if (length(r)<8) print }')
  if [ -n "$_badmark" ]; then
    echo "✗ status-ok 标记后理由为空或过短（<8 个非空白字符）——标记必须带理由，否则它本身就是一条洗白通道："
    printf '%s\n' "$_badmark" | head -5
    return 1
  fi
  _mk=$(grep -rc 'status-ok:' specs docs/dingwei-architecture-design.md --include=*.md 2>/dev/null | awk -F: '{t+=$2} END{print t+0}')
  echo "  ⑨ 无未登记的过期状态断言 ✓（行内 status-ok 标记 $_mk 处，理由非空已校验）"
  [ "$_n" -gt 0 ] && echo "  ⑨ ⚠️ PASS WITH REGISTERED DEBT：状态例外 $_n 条——登记是债务可见化，不是修复；例外的新增与批准属 owner/门方评审面（本闸只验登记、理由与漂移，不验批准）。"
  return 0
}

selftest() {
  T=$(mktemp -d); n=0; fail=0
  ok(){ n=$((n+1)); echo "  ✓ $1"; }; bad(){ echo "  ✗ 反测未过：$1"; fail=1; }

  # ① 现状必须通过
  if check >/dev/null 2>&1; then ok "现状一致（闸不误报）"; else bad "现状被误判"; fi

  # ② 注入一处未登记的过期断言 -> 必拦
  echo '本件生命周期仍为「草案」，尚未执行。' > specs/.sc-selftest.md
  if check >/dev/null 2>&1; then bad "未登记的过期断言被放行"; else ok "拦住：未登记的过期断言"; fi
  rm -f specs/.sc-selftest.md

  # ③ reviewer-owned 的 checklist 不得被扫（执笔方不得代改门方的件）
  echo '本清单写着 尚未执行。' > specs/platform/checklist.sc-selftest.md
  if check >/dev/null 2>&1; then ok "不扫 reviewer-owned checklist（不代改门方件）"; else bad "误扫了 checklist"; fi
  rm -f specs/platform/checklist.sc-selftest.md

  # ④ 例外缺理由 -> 判闸坏，不得当成通过
  cp "$EXC" "$T/exc.bak"; printf 'specs/README.md:1\n' >> "$EXC"
  check >/dev/null 2>&1; [ $? -eq 2 ] && ok "例外缺理由判闸坏（非通过）" || bad "缺理由的例外被放行"
  cp "$T/exc.bak" "$EXC"

  # ⑤ 例外指向不存在的文件 -> 判闸坏
  printf 'specs/nosuch.md:1 反测用\n' >> "$EXC"
  check >/dev/null 2>&1; [ $? -eq 2 ] && ok "例外漂移判闸坏（非通过）" || bad "漂移的例外被放行"
  cp "$T/exc.bak" "$EXC"

  # ⑥ 死例外须被判出（该位置已无过期断言却仍登记着）
  printf 'specs/README.md:1 反测用,该行并无过期断言\n' >> "$EXC"
  if check >/dev/null 2>&1; then bad "死例外被放行"; else ok "拦住:已失效的例外(须删除)"; fi
  cp "$T/exc.bak" "$EXC"

  # ⑦ status-ok 空理由 -> 必拦（此前只 grep -v 跳过，空理由也过）
  printf '随便一句正常话 <!-- status-ok: -->\n' > specs/.sc-selftest2.md
  if check >/dev/null 2>&1; then bad "status-ok 空理由被放行"; else ok "拦住：status-ok 空理由"; fi
  rm -f specs/.sc-selftest2.md

  # ⑧ status-ok 理由过短/纯符号 -> 必拦
  printf '随便一句正常话 <!-- status-ok: 嗯 -->\n' > specs/.sc-selftest2.md
  if check >/dev/null 2>&1; then bad "status-ok 过短理由被放行"; else ok "拦住：status-ok 理由过短"; fi
  rm -f specs/.sc-selftest2.md

  # ⑨ 登记债务时输出须亮明 REGISTERED DEBT（不得无条件「全过」）
  echo '本件生命周期仍为「草案」，尚未执行。' > specs/.sc-selftest.md
  printf 'specs/.sc-selftest.md:1 反测用,债务登记示例\n' >> "$EXC"
  out=$(check 2>/dev/null); rc_=$?
  cp "$T/exc.bak" "$EXC"; rm -f specs/.sc-selftest.md
  case "$out" in
    *"REGISTERED DEBT"*) [ $rc_ -eq 0 ] && ok "登记债务时亮明 PASS WITH REGISTERED DEBT" || bad "债务亮明但退出码不对";;
    *) bad "登记债务时未亮明 REGISTERED DEBT";;
  esac

  rm -rf "$T"
  [ $fail -eq 0 ] && echo "--- 反测 $n 项全过（闸有牙）" || echo "--- 反测未过"
  return $fail
}

case "${1:-}" in --selftest) selftest; exit $? ;; esac
check; exit $?
