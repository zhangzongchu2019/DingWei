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
EXLIST=${EXLIST:-specs/gates/status-consistency-exceptions.txt}
# 🔴 **冻结基线已删除**（乙域 terra 收尾裁定 2026-08-26 第三条）：
#   「冻结基线不容作为通过条件：**待批准 ＝ 拒绝**；`PASS WITH REGISTERED DEBT`
#     不能返回成功、也不能被聚合为合规 PASS。」
#   ⇒ 本闸此前那条旧抑制已**在源头消掉**（不是被批准，也不是记成债务），故基线件一并删除。

# 🔴 乙域 terra 全闸契约（2026-08-26）：三类抑制（style-ok / status-ok / path-ok）均属特权例外。
#   本闸原先 `grep -v 'status-ok:'` **整行跳过**，另校「理由 ≥8 个非空白字符」——
#   **那是长度，不是批准**：写满八个字就能把一处真实的过期状态断言洗白。
#   ⇒ 改为与另两闸**同一套**受控例外（公共库 specs/gates/lib/exceptions.sh）。
# ⚠️ 注意区分两张表，别混：
#   · $EXC（status-stale-exceptions.txt）＝「该位置的断言归门方件、执笔方不得代改」的**位置登记**；
#   · $EXLIST ＝ 「该行只是**提及**旧措辞、并非现状断言」的**受控例外**（须门方 approval_ref）。
_LIB=${EXC_LIB:-specs/gates/lib/exceptions.sh}
[ -f "$_LIB" ] || { echo "✗ 闸依赖缺失：$_LIB 不存在 —— 闸坏了，不是免检"; exit 2; }
. "$_LIB"
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
  HITS=$(grep -rnE "$PAT" specs docs/dingwei-platform-architecture.md --include=*.md 2>/dev/null \
        | grep -v '/checklist' \
        | grep -v "^$SPECDOC:" \
        | grep -v "^$EXC:" \
        | grep -v '^specs/gates/' \
        | grep -v '^specs/evidence/reference-index.md:' \
        | grep -v '^scripts/')
  rc=0; left=''; _exn=0; _unkn=0; _unk=''; _inert=0
  for h in $(printf '%s\n' "$HITS" | awk -F: 'NF>0 {print $1":"$2}'); do
    excepted "$h" && continue
    # 🔴 逐【短语】判受控例外 —— 一行可能同时命中多个短语（实例：
    #   「仍为草案／须重走／不得被引用」一行两命中）。
    #   例外必须逐个短语绑定：**只批了其中一个，另一个不得搭便车**。
    _hf=${h%%:*}; _hl=${h##*:}
    _phr=$(sed -n "${_hl}p" "$_hf" | grep -oE "$PAT" | sort -u)
    _left_phr=''
    for _pp in $_phr; do
      exc_decide "$EXLIST" "$_pp" "$_hf" "$_hl"; _erc=$?
      [ "$_erc" -eq 0 ] && { _exn=$((_exn+1)); continue; }
      [ "$_erc" -eq 2 ] && { _unk="$_unk $_hf:$_hl($_pp)"; _unkn=$((_unkn+1)); continue; }
      _left_phr="$_left_phr $_pp"
    done
    [ -z "$(printf '%s' "$_left_phr" | tr -d '[:space:]')" ] && continue
    sed -n "${_hl}p" "$_hf" | grep -q 'status-ok:' && _inert=$((_inert+1))
    left="$left $h"; rc=1
  done
  if [ "$_unkn" -gt 0 ]; then
    echo "✗ UNKNOWN（exit 2）：$_unkn 处受控例外的【批准】无从核验 —— 授权根不可得"
    for u in $_unk; do echo "    $u"; done
    echo "  ⇒ 见 specs/gates/approvals/TRUST-ROOT.md：当前无 ACTIVE 后端（门方待裁）。"
    return 2
  fi
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
    echo "  修法三选一：① 把该处改成与权威源一致；② 若属不应由执笔方改的件，登记进 $EXC 并写明理由与归属；"
    echo "     ③ 若该行只是**提及**旧措辞而非断言现状，到 $EXLIST 登记受控例外（六列 ＋ 门方 approval_ref）。"
    [ "$_inert" -gt 0 ] && echo "  ⚠️ 其中 $_inert 处仍在用行内 \`status-ok:\` —— **那条通道已关闭**（自由文本理由无人核）。"
    return 1
  fi
  # 🔴 原「status-ok 标记须带 ≥8 字理由」的校验**已删除** —— 它测的是长度，不是批准。
  #   ⚠️ **改契约必须回头删掉为旧契约写的校验**：留着它，读者会以为这条通道仍有人把关。
  #   现行内 status-ok 一律不构成豁免，仅作人读注释；仍在用它的行会照常被判（见上）。
  _mk=$(grep -rc 'status-ok:' specs docs/dingwei-platform-architecture.md --include=*.md 2>/dev/null | awk -F: '{t+=$2} END{print t+0}')
  echo "  ⑨ 无未登记的过期状态断言 ✓"
  # 覆盖见证（GATE-CONTRACT 一之二·第 4 条）
  _hn=$(printf '%s\n' "$HITS" | awk 'NF>0' | wc -l | tr -d ' ')
  echo "     受检范围（预期集合）：specs/**.md ＋ docs/dingwei-platform-architecture.md，短语 $(printf '%s' "$PAT" | awk -F'|' '{print NF}') 种"
  echo "     实检集合：$_hn 处短语命中逐处判（口径：不扫 reviewer-owned checklist*，不扫 specs/gates/、scripts/）"
  echo "     未知集合：$_unkn"
  echo "     行内 status-ok 标记 $_mk 处 —— 🔴 **已不构成豁免**，仅供人读"
  if [ "$_exn" -gt 0 ]; then
    echo "  ⚠️ 其中 $_exn 处经【受控例外】放行（$EXLIST，approval_ref 已解析核验）"
    echo "     🔴 闸只验了批准记录存在且四元组对得上；**这条豁免该不该给，仍归门方**。"
  fi
  # 🔴 乙域收尾裁定：登记而未获逐项批准的例外，**不得返回成功、不得被聚合为合规 PASS**。
  if [ "$_n" -gt 0 ]; then
    echo "✗ 状态例外 $_n 条已登记但**未获逐项批准** ⇒ 本闸不报通过（待批准 ＝ 拒绝）"
    echo "  ⇒ 二选一：把该处改成与权威源一致；或按 specs/gates/approvals/TRUST-ROOT.md 取门方 approval_ref。"
    return 1
  fi
  echo "  🔴 过了只等于「清单形态内的过期断言已清零」；**绝不等于**语义层一致，也不等于例外已获批准。"
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

  # ===== 受控例外组（丙机制精确版：门方独占仓 ＋ 受保护 ref ＋ 不可变 commit）=====
  # 🔴 **改契约必须回头删掉为旧契约写的反测**：原 ⑦⑧ 测的是「status-ok 理由是否够长」，
  #   而该通道已整体关闭 —— 留着它们，读者会以为长度校验仍是这条通道的把关人。
  REV=zzc-chatgpt56terrazhangzongchu-2642
  . specs/gates/lib/approval-fixture.sh
  _OT=$EXC_TRUSTROOT; _OG=${EXC_GITDIR:-.}
  EXC_TRUSTROOT="$T/trust.md"; EXC_GITDIR="$T/appro"
  _fps(){ sed -n "$2p" "$1" | tr -d '[:space:]' | sha256sum | cut -c1-16; }

  # ⑦ 🔴 真实违规 ＋ 行内编造抑制 ⇒ 必拒（terra 点名要的那一半）
  printf '本件生命周期仍为「草案」，尚未执行。<!-- status-ok: 我说这只是提及而已 -->\n' > specs/.sc-selftest2.md
  EXLIST="$T/none.txt" check >/dev/null 2>&1
  [ $? -eq 1 ] && ok "🔴 拦住：真实的过期状态断言 ＋ 行内编造的 status-ok ⇒ 拒（自由文本不再豁免）" \
                || bad "行内 status-ok 仍能整行跳过"

  # ⑧ 经受控例外的合法对象 ⇒ 过；码 mention-not-assertion 的后置条件＝短语落在「」内
  printf '此前「尚未执行」的限制随生效解除。\n' > specs/.sc-selftest2.md
  _f8=$(_fps specs/.sc-selftest2.md 1)
  _CMT=$(exc_fixture_make "$T" "$REV" ST-TEST-01 尚未执行 specs/.sc-selftest2.md "$_f8" mention-not-assertion)
  [ -n "$_CMT" ] || bad "🔴 批准夹具没造出来 —— 下列反测全部无效"
  printf 'ST-TEST-01\t尚未执行\tspecs/.sc-selftest2.md\t%s\tmention-not-assertion\t%s@%s#AP-01\n' "$_f8" "$REV" "$_CMT" > "$T/ex.txt"
  EXLIST="$T/ex.txt" check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "经受控例外的合法对象放行（短语落在「」内 ＋ 受保护 ref 可达 ＋ 四元组匹配）" || bad "合规受控例外被误拦"

  # ⑨ 🔴 码的后置条件有牙：同一条例外，短语**不在引号内**时必拒
  printf '本件尚未执行，须重走。\n' > specs/.sc-selftest2.md
  _f9=$(_fps specs/.sc-selftest2.md 1)
  _C9=$(exc_fixture_make "$T" "$REV" ST-TEST-02 尚未执行 specs/.sc-selftest2.md "$_f9" mention-not-assertion)
  printf 'ST-TEST-02\t尚未执行\tspecs/.sc-selftest2.md\t%s\tmention-not-assertion\t%s@%s#AP-01\n' "$_f9" "$REV" "$_C9" > "$T/ex.txt"
  EXLIST="$T/ex.txt" check >/dev/null 2>&1
  [ $? -eq 1 ] && ok "🔴 拦住：报「提及而非断言」但短语并不在引号内（码有后置条件）" || bad "码选错仍放行"

  # ⑩ 🔴 一行两命中，例外只绑其中一个 ⇒ 另一个**不得搭便车**
  printf '此处却仍写「仍为草案」，并且本件尚未执行。\n' > specs/.sc-selftest2.md
  _fa=$(_fps specs/.sc-selftest2.md 1)
  _CA=$(exc_fixture_make "$T" "$REV" ST-TEST-03 仍为草案 specs/.sc-selftest2.md "$_fa" mention-not-assertion)
  printf 'ST-TEST-03\t仍为草案\tspecs/.sc-selftest2.md\t%s\tmention-not-assertion\t%s@%s#AP-01\n' "$_fa" "$REV" "$_CA" > "$T/ex.txt"
  EXLIST="$T/ex.txt" check >/dev/null 2>&1
  [ $? -eq 1 ] && ok "🔴 拦住：同一行第二个短语命中不被第一个的例外一并豁免（粒度是短语，不是行）" \
                || bad "一条例外把整行的所有命中一起豁免了"

  # ⑪ 🔴 授权根无 ACTIVE 后端（＝仓内当前实况）⇒ exit 2
  printf '此前「尚未执行」的限制随生效解除。\n' > specs/.sc-selftest2.md
  _fc=$(_fps specs/.sc-selftest2.md 1)
  _CC=$(exc_fixture_make "$T" "$REV" ST-TEST-01 尚未执行 specs/.sc-selftest2.md "$_fc" mention-not-assertion)
  printf 'ST-TEST-01\t尚未执行\tspecs/.sc-selftest2.md\t%s\tmention-not-assertion\t%s@%s#AP-01\n' "$_fc" "$REV" "$_CC" > "$T/ex.txt"
  exc_fixture_trust "$T" "$REV" ''
  EXLIST="$T/ex.txt" check >/dev/null 2>&1
  [ $? -eq 2 ] && ok "🔴 授权根不可得 ⇒ UNKNOWN exit 2（不是 PASS，也不是 FAIL）" || bad "授权根不可得时闸给了结论"
  exc_fixture_trust "$T" "$REV" reviewer-owned-protected-ref

  EXC_TRUSTROOT=$_OT; EXC_GITDIR=$_OG
  rm -f specs/.sc-selftest2.md
  rm -f specs/.sc-selftest2.md

  # ⑨ 🔴 **本条随乙域收尾裁定反转**（2026-08-26）：
  #   原写「登记债务时亮明 PASS WITH REGISTERED DEBT ⇒ rc 0 ⇒ ok」——
  #   terra：「待批准 ＝ 拒绝；`PASS WITH REGISTERED DEBT` 不能返回成功、
  #   也不能被聚合为合规 PASS。」⇒ 登记而未获逐项批准的例外，闸**不得报通过**。
  #   ⚠️ **改契约必须回头改为旧契约写的反测** —— 留着它就等于把「登记即放行」锁定成期望语义。
  echo '本件生命周期仍为「草案」，尚未执行。' > specs/.sc-selftest.md
  printf 'specs/.sc-selftest.md:1 反测用,债务登记示例\n' >> "$EXC"
  out=$(check 2>/dev/null); rc_=$?
  cp "$T/exc.bak" "$EXC"; rm -f specs/.sc-selftest.md
  [ $rc_ -ne 0 ] && ok "🔴 登记例外但未获逐项批准 ⇒ 不得报通过（待批准＝拒绝）" \
                 || bad "登记即放行 —— PASS WITH REGISTERED DEBT 仍被当成通过"

  rm -rf "$T"
  [ $fail -eq 0 ] && echo "--- 反测 $n 项全过（闸有牙）" || echo "--- 反测未过"
  return $fail
}

case "${1:-}" in --selftest) selftest; exit $? ;; esac
check; exit $?
