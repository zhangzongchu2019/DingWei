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
EXLIST=${EXLIST:-specs/gates/superseded-claims-exceptions.txt}
# 🔴 **冻结基线已删除**（乙域 terra 收尾裁定 2026-08-26 第三条）：
#   「冻结基线不容作为通过条件：**待批准 ＝ 拒绝**；`PASS WITH REGISTERED DEBT`
#     不能返回成功、也不能被聚合为合规 PASS。」
#   ⇒ 本闸此前那条旧抑制已**在源头消掉**（不是被批准，也不是记成债务），故基线件一并删除。

# 🔴 v3（乙域 terra 复验 fix @ 434b8fe）：例外判据整体移入**三闸共用**的公共库。
#   移的原因不是复用省事，是 terra 那条裁定的字面要求 ——
#   「style-ok / status-ok / path-ok 等抑制**均属特权例外**」：
#   通则若只落在一条通道里，它就还不是通则。⇒ 一份实现，三闸共用。
# 🔴 执笔方会话名在库内**写死**，不再读 `AUTHOR_SESSION` 环境变量
#   （terra 变异 2：改一个环境变量，执笔方即可把自己从「作者」名单里摘出去自批）。
_LIB=${EXC_LIB:-specs/gates/lib/exceptions.sh}
[ -f "$_LIB" ] || { echo "✗ 闸依赖缺失：$_LIB 不存在 —— 闸坏了，不是免检"; exit 2; }
. "$_LIB"

# 受控理由码的后置条件见 specs/gates/REASON-CODES.md，实现见公共库 exc_code_ok()。
#   ⚠️ 判【码与行形态是否相符】，🔴 判不了【豁免该不该给】，也判不了【码选得对不对】——归门方。
# 🔴 **`style-ok:` 已从本 SKIP 中移除**（乙域 RV-乙18）：
#   自由文本豁免＝闸有一个没人看的后门 —— 闸只看有没有那四个字，不看理由是否属实。
#   实例：以「跨行误命中」为由压住两处【行内就有】的旧断言，闸照旧返回 0。
#   ⇒ 改为**结构化登记例外**（$EXLIST），逐条指名件、理由非空、门方批准，执笔方不得自批。
SKIP='沿革|历史时点|【RV|已被取代|已撤销|重裁|旧模型名|旧口径'
# 🔴 RV 标注常写成【行内注释】,与旧断言同在一行。
#   首版按行跳过含「【RV|」的行 ⇒ **评审方标注「这里有旧断言」这个动作本身,
#   把那处旧断言从闸的视野里抹掉了** —— 标得越对,闸越看不见。
#   ⇒ 改为【先剥掉标注、再判剩余部分】。
#   ⚠️ 原写「剥离时不得连 style-ok 一起剥掉」—— **该条随 RV-乙18 作废**：
#     style-ok 对本闸已不再构成豁免，豁免只走 $EXLIST 的结构化登记例外。
_strip_rv() {
  sed 's/【RV[^】]*】[^<]*//g' \
    | sed 's/<!--\([^>]*style-ok:[^>]*\)-->/STYLEOK_KEEP\1/g' \
    | sed 's/<!--[^>]*-->//g'
}

check() {
  [ -f "$LIST" ] || { echo "✗ 闸依赖缺失：$LIST 不存在 —— 闸坏了，不是免检"; return 2; }
  _n=$(sed 's/#.*//' "$LIST" | awk -F'\t' 'NF>=3' | wc -l | tr -d ' ')
  [ "$_n" -eq 0 ] && { echo "✗ $LIST 无有效条目（须 <旧断言> TAB <现行> TAB <依据>）—— 闸坏了"; return 2; }

  _bad=0; _exn=0; _unkn=0; _unk=''; _seen=0
  for f in $DOCS; do
    [ -f "$f" ] || { echo "✗ 闸依赖缺失：$f 不存在"; return 2; }
    _seen=$((_seen+1))
    _hit=0
    while IFS="$(printf '\t')" read -r old cur why; do
      case "$old" in ''|'#'*) continue;; esac
      _lines=$(cat -n "$f" | _strip_rv | grep -F "$old" 2>/dev/null | grep -vE "$SKIP" | sed 's/^[[:space:]]*\([0-9]*\)[[:space:]]*/\1:/')
      [ -z "$_lines" ] && continue
      # 🔴 受控例外 v3（乙域 terra 复验 fix @ 434b8fe，2026-08-26）：
      #   v2 的两个洞（粒度只到「短语＋件」、批准方从不校验）已修；
      #   v3 修的是**第三个、也是更深的一个**：v2 校了「批准方」这个**字符串**，
      #   而 terra 用两个变异证明字符串不是证据 ——
      #     填字面「terra」放行；把 AUTHOR_SESSION 改个名就能自批。
      #   ⇒ v3 要 `approval_ref` 解析到 reviewer-owned 台账，四元组逐项对上；
      #     **授权根不可得 ⇒ UNKNOWN(2)，不是 PASS**。
      _remain=""
      for _one in $(printf '%s\n' "$_lines" | cut -d: -f1); do
        exc_decide "$EXLIST" "$old" "$f" "$_one"; _erc=$?
        # ⚠️ rc 必须**当场接住**：写成 `if exc_decide …; then …; fi` 之后再读 `$?`，
        #   读到的是 if 复合命令的状态（条件为假且无 else 时为 0），UNKNOWN 会被读成 PASS。
        #   —— 又一次「判据落在了别的东西上」，本轮第 N 次，故写死在此。
        [ "$_erc" -eq 0 ] && { _exn=$((_exn+1)); continue; }
        [ "$_erc" -eq 2 ] && { _unk="$_unk $f:$_one(「$old」)"; _unkn=$((_unkn+1)); continue; }
        _remain="$_remain $_one"
      done
      _lines=$(printf '%s\n' "$_lines" | awk -v keep="$_remain" '
                BEGIN{n=split(keep,a," "); for(i=1;i<=n;i++) k[a[i]]=1}
                { split($0,b,":"); if (b[1] in k) print }')
      [ -z "$(printf '%s' "$_lines" | tr -d '[:space:]')" ] && continue
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

  # 🔴 UNKNOWN 先于 FAIL/PASS 判：授权根不可得时，闸没有资格给结论。
  if [ "$_unkn" -gt 0 ]; then
    echo "✗ UNKNOWN（exit 2）：$_unkn 处登记例外的【批准】无从核验 —— 授权根不可得"
    for u in $_unk; do echo "    $u"; done
    echo "  ⇒ 见 specs/gates/approvals/TRUST-ROOT.md：当前无 ACTIVE 后端（门方待裁）。"
    echo "  🔴 **不得把「我验不了这条批准」折算成「这条批准成立」** —— 那正是空集当通过的同族。"
    return 2
  fi
  if [ "$_bad" -ne 0 ]; then
    echo "  🔴 裁定改了，引用点必须跟着改 —— 「留记」不算修。"
    echo "  ⚠️ 确属沿革叙述者：改写为明确的历史时点表述，"
    echo "     或到 $EXLIST 逐条登记（六列：claim_id ＋ 被抑制对象 ＋ 件 ＋ 行指纹 ＋ 理由码 ＋ approval_ref）。"
    echo "     🔴 行内 style-ok 对本闸【已不再豁免】——自由文本理由无人复核（RV-乙18）；"
    echo "        且 approval_ref 须解析到门方台账，写个门方的名字**不算批准**（terra 变异 1）。"
    return 1
  fi
  echo "  ✓ 清单内 $_n 条已撤销断言在正文均已清零"
  # 🔴 覆盖见证（GATE-CONTRACT 一之二·第 4 条：预期集合 · 实检集合 · 未知集合）
  echo "     受检范围（预期集合）：$(printf '%s' "$DOCS" | wc -w | tr -d ' ') 件 × $_n 条清单短语"
  echo "     实检集合：$_seen 件实读；🔴 **口径＝已剥 RV 标注段**（先剥标注再判剩余 —— 指出问题的那句话不该被判成问题本身）"
  echo "     未知集合：$_unkn（非空即上面已 return 2）"
  if [ "$_exn" -gt 0 ]; then
    echo "  ⚠️ 其中 $_exn 处经【受控例外】放行（$EXLIST，approval_ref 已解析核验）"
    echo "     🔴 闸只验了批准记录**存在且四元组对得上**；**这条豁免该不该给，仍归门方**。"
  fi
  echo "  ⚠️ 本闸判不了【清单是否完备】—— 漏登记的旧断言它看不见，那仍归门方。"
  echo "  🔴 过了只等于「清单内短语在正文已清零」；**绝不等于**清单完备、或例外已获批准。"
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

  # 🔴 **本组两条是 RV-乙18 的直接产物，反转了原断言。**
  #   原反测写的是「不误伤：带 style-ok 理由的例外放行 ⇒ ok」——
  #   **那把绕过面锁定成了期望语义**：闸不看理由是否属实，只看有没有那四个字，
  #   而反测又把这个行为断言为正确 ⇒ **再加多少条反测都测不出来，除非改契约本身。**
  #   现契约：style-ok 对本闸不再豁免；豁免只走结构化登记例外。
  mk; printf '本节仍写旧说法 <!-- style-ok: 引述历史 -->\n' >> "$T/a.md"
  EXLIST="$T/none.txt" run
  [ $? -eq 1 ] && ok "🔴 拦住：仅有行内 style-ok【不再】豁免（RV-乙18 反转此断言）" \
                || bad "自由文本 style-ok 仍能绕过 —— 契约未真正改掉"

  # ===== v3 反测：受控例外 = 键结构 ＋ 【可验证的批准锚】 =====
  # ⚠️ **改契约必须回头改为旧契约写的反测** —— v2 的五列（含「批准方」自由文本）在此整组重写。
  # 🔴 本组的存在理由是 terra 的两个变异（判于 fix @ 434b8fe）：
  #   变异 1：第 5 列填字面「terra」，仓内并无其批准记录 ⇒ v2 rc=0；
  #   变异 2：`AUTHOR_SESSION` 由环境变量覆盖后执笔方自批 ⇒ v2 rc=0。
  #   ⇒ **旧反测覆盖不到它们，不是因为条数不够，是因为契约本身把「字符串」当成了「证据」。**
  _fp0(){ sed -n "$2p" "$1" | tr -d '[:space:]' | sha256sum | cut -c1-16; }
  REV=zzc-chatgpt56terrazhangzongchu-2642
  . specs/gates/lib/approval-fixture.sh
  _OT=$EXC_TRUSTROOT; _OG=${EXC_GITDIR:-.}
  EXC_TRUSTROOT="$T/trust.md"; EXC_GITDIR="$T/appro"
  _ex(){  # <claim_id> <对象> <件> <指纹> <码> <approval_ref>
    printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$1" "$2" "$3" "$4" "$5" "$6" > "$T/ex.txt"; }
  _runex(){ EXLIST="$T/ex.txt" run; }

  # 夹具：一处**真实命中**（明确时点表述，故 historical-narrative 的后置条件成立）
  # ⚠️ 此处不能用「沿革：…」开头 —— 那个词在 $SKIP 里，整行会被跳过，
  #   于是本组每一条测的都不是例外机制，而是「命中集为空」。
  mk; printf '本节 v3.2 之前采用旧说法处理。\n' >> "$T/a.md"
  _lh=$(grep -n 'v3.2 之前' "$T/a.md" | cut -d: -f1); _fh=$(_fp0 "$T/a.md" "$_lh")
  EXLIST="$T/none.txt" run
  [ $? -eq 1 ] || bad "🔴 本组夹具未构成真命中 —— 下列受控例外反测全部无效"

  _CMT=$(exc_fixture_make "$T" "$REV" SC-TEST-01 旧说法 "$T/a.md" "$_fh" historical-narrative)
  [ -n "$_CMT" ] || bad "🔴 批准夹具（门方独占仓）没造出来 —— 下列反测全部无效"

  # ① 🔴 terra 要的正例：approval_ref 解析到【不可变 commit ＋ 记录 ID】且四元组逐项对上 ⇒ 过
  _ex SC-TEST-01 旧说法 "$T/a.md" "$_fh" historical-narrative "$REV@$_CMT#AP-01"
  _runex; [ $? -eq 0 ] && ok "经受控例外的合法对象放行（受保护 ref 可达 ＋ 不可变 commit ＋ 四元组匹配）" \
                       || bad "合规受控例外被误拦"

  # ② 🔴 变异 1（terra 实测）：approval_ref 只写门方**名字**、不指 commit ⇒ 形状即拒
  _ex SC-TEST-01 旧说法 "$T/a.md" "$_fh" historical-narrative "$REV"
  _runex; [ $? -eq 1 ] && ok "🔴 变异1 拦住：approval_ref 只有门方名字（写个名字不算批准）" || bad "伪造门方显示名仍能豁免"

  # ③ 🔴 指到一个**不可达**的 commit（合法 40 位 hex，但不在受保护 ref 上）⇒ UNKNOWN
  _ex SC-TEST-01 旧说法 "$T/a.md" "$_fh" historical-narrative "$REV@0123456789abcdef0123456789abcdef01234567#AP-01"
  _runex; [ $? -eq 2 ] && ok "🔴 commit 在受保护 ref 上不可达 ⇒ 验证失败 ＝ UNKNOWN(2)，不是 PASS" || bad "不可达 commit 被放行或被判成 FAIL"

  # ④ 🔴 分支名 / 短 sha 不是不可变引用 ⇒ 拒
  for _bad_ref in "$REV@review/approvals-test#AP-01" "$REV@$(printf '%s' "$_CMT" | cut -c1-8)#AP-01"; do
    _ex SC-TEST-01 旧说法 "$T/a.md" "$_fh" historical-narrative "$_bad_ref"
    _runex; [ $? -eq 1 ] || { bad "非不可变引用 [$_bad_ref] 竟被受理"; break; }
  done
  [ $fail -eq 0 ] && ok "🔴 分支名 / 短 sha 均不构成【不可变 commit】⇒ 拒"

  # ⑤ 🔴 四元组失配（记录绑的是另一处指纹）⇒ 拒
  _CMT2=$(exc_fixture_make "$T" "$REV" SC-TEST-01 旧说法 "$T/a.md" 0000000000000000 historical-narrative)
  _ex SC-TEST-01 旧说法 "$T/a.md" "$_fh" historical-narrative "$REV@$_CMT2#AP-01"
  _runex; [ $? -eq 1 ] && ok "🔴 拦住：批准记录与四元组失配（批准是对某一处的批准，不是对某个人的信任）" || bad "失配的批准记录仍放行"

  # ⑥ 🔴 变异 2（terra 实测）：覆盖 AUTHOR_SESSION 环境变量后，执笔方仍不能自批
  _CMT3=$(exc_fixture_make "$T" zzc-opus50tchangshark-2642 SC-TEST-01 旧说法 "$T/a.md" "$_fh" historical-narrative)
  _ex SC-TEST-01 旧说法 "$T/a.md" "$_fh" historical-narrative "zzc-opus50tchangshark-2642@$_CMT3#AP-01"
  AUTHOR_SESSION=not-the-author _runex
  [ $? -eq 1 ] && ok "🔴 变异2 拦住：覆盖 AUTHOR_SESSION 仍不能自批（执笔方名写死，不读环境变量）" \
               || bad "改环境变量即可自批 —— 判据落在被判方可控的面上"
  _CMT=$(exc_fixture_make "$T" "$REV" SC-TEST-01 旧说法 "$T/a.md" "$_fh" historical-narrative)

  # ⑦ approval_ref 为空 / 未决词 ⇒ 拒
  for _ref in "" "待门方批准" "$REV@$_CMT#pending"; do
    _ex SC-TEST-01 旧说法 "$T/a.md" "$_fh" historical-narrative "$_ref"
    _runex; [ $? -eq 1 ] || { bad "approval_ref=[$_ref] 竟构成批准"; break; }
  done
  [ $fail -eq 0 ] && ok "🔴 拦住：approval_ref 空 / 未决 / pending 均不构成批准"

  # ⑧ 🔴 批准人不在【同一 commit 内的】门方名册 ⇒ 拒
  #    名册也取自受保护 ref 的同一 commit —— 不取工作区那份（工作区在执笔方写权内）
  git -C "$T/appro" rm -q --cached reviewers.txt >/dev/null 2>&1
  : > "$T/appro/reviewers.txt"
  git -C "$T/appro" add -A >/dev/null 2>&1
  git -C "$T/appro" -c user.name=fx -c user.email=fx@example.invalid commit -q -m empty-roster >/dev/null 2>&1
  _CMT4=$(git -C "$T/appro" rev-parse HEAD)
  _ex SC-TEST-01 旧说法 "$T/a.md" "$_fh" historical-narrative "$REV@$_CMT4#AP-01"
  _runex; [ $? -eq 1 ] && ok "🔴 拦住：批准人不在同一 commit 内的门方名册中" || bad "名册外的人也能批准"
  _CMT=$(exc_fixture_make "$T" "$REV" SC-TEST-01 旧说法 "$T/a.md" "$_fh" historical-narrative)

  # ⑨ 🔴🔴 授权根无 ACTIVE 后端（＝仓内当前实况）⇒ **exit 2（UNKNOWN）**
  #    钉住本轮最核心的一条：**「我验不了」不得折算成「验过了」，也不等于「不合格」。**
  exc_fixture_trust "$T" "$REV" ''
  _ex SC-TEST-01 旧说法 "$T/a.md" "$_fh" historical-narrative "$REV@$_CMT#AP-01"
  _runex; [ $? -eq 2 ] && ok "🔴 授权根不可得（无 ACTIVE 后端）⇒ UNKNOWN exit 2，不折算为 PASS" \
                       || bad "授权根不可得时闸给了结论（应为 exit 2）"
  # 🔴 该门方无 REF 声明（受保护 ref 不可取）⇒ 同样 UNKNOWN
  printf 'ACTIVE: reviewer-owned-protected-ref\n' > "$T/trust.md"
  _runex; [ $? -eq 2 ] && ok "🔴 该门方无受保护 ref 声明 ⇒ 取不到 ⇒ UNKNOWN exit 2" || bad "无 REF 声明时给了结论"
  exc_fixture_trust "$T" "$REV" reviewer-owned-protected-ref

  # 🔴 授权根的【注释行】不得生效 —— 钉住 2026-08-26 那个自造缺陷：
  #   上一版闸直接读散文件 TRUST-ROOT.md，其「启用方式」示例的行首形态与真配置一样，
  #   于是**文档示例把后端激活了**，而文档同一页还写着「当前零 ACTIVE」。
  #   ⚠️ 判据与被判物必须同源；散文件里写着的东西不该能改变机器的判定。
  printf '# ACTIVE: reviewer-owned-protected-ref\n#REF: %s review/approvals-test approvals/rec.approvals\n' "$REV" > "$T/trust.md"
  _ex SC-TEST-01 旧说法 "$T/a.md" "$_fh" historical-narrative "$REV@$_CMT#AP-01"
  _runex; [ $? -eq 2 ] && ok "🔴 授权根里【注释掉的】ACTIVE/REF 行不生效（文档示例不得激活后端）" \
                       || bad "注释行竟激活了后端 —— 散文能改变机器判定"
  exc_fixture_trust "$T" "$REV" reviewer-owned-protected-ref

  # ⑩ 行指纹不匹配 ⇒ 例外失效（内容一改须重新批准）
  _ex SC-TEST-01 旧说法 "$T/a.md" 0000000000000000 historical-narrative "$REV@$_CMT#AP-01"
  _runex; [ $? -eq 1 ] && ok "🔴 行指纹不匹配 ⇒ 例外失效（内容哈希，非行号）" || bad "指纹不匹配仍放行"

  # ⑪ 跨件泛化 ⇒ 拒
  _ex SC-TEST-01 旧说法 "$T/别的件.md" "$_fh" historical-narrative "$REV@$_CMT#AP-01"
  _runex; [ $? -eq 1 ] && ok "🔴 拦住：例外【件不匹配】不放行（例外不得跨件泛化）" || bad "跨件泛化被放行"

  # ⑫ 未知码 / 空码 ⇒ 拒（码不在表内即拒，不做「未知即放行」）
  for _c in "no-such-code：随便写的" ""; do
    _ex SC-TEST-01 旧说法 "$T/a.md" "$_fh" "$_c" "$REV@$_CMT#AP-01"
    _runex; [ $? -eq 1 ] || { bad "理由码=[$_c] 竟被放行"; break; }
  done
  [ $fail -eq 0 ] && ok "🔴 未知码 / 空码一律拒（空码 ＝ 没码）"

  # ⑬ 🔴 码与行形态不符：行内真实命中却报「跨行误命中」⇒ 必拒
  mk; printf '本节采用旧说法处理。\n' >> "$T/a.md"
  _lz=$(grep -n '本节采用' "$T/a.md" | cut -d: -f1); _fz=$(_fp0 "$T/a.md" "$_lz")
  _C5=$(exc_fixture_make "$T" "$REV" SC-TEST-02 旧说法 "$T/a.md" "$_fz" cross-line-false-positive)
  _ex SC-TEST-02 旧说法 "$T/a.md" "$_fz" cross-line-false-positive "$REV@$_C5#AP-01"
  _runex; [ $? -eq 1 ] && ok "🔴 行内真实命中 ＋ 码称「跨行误命中」⇒ 必拒（拆穿那次编造的理由）" \
                       || bad "编造的跨行理由仍能豁免 —— 码没有后置条件"

  # ⑭ 🔴 沿革码 ＋ 现在时写旧状态 ⇒ 拒
  mk; printf '本节现有旧说法 的写法。\n' >> "$T/a.md"
  _ln2=$(grep -n '本节现有' "$T/a.md" | cut -d: -f1); _f2=$(_fp0 "$T/a.md" "$_ln2")
  _C6=$(exc_fixture_make "$T" "$REV" SC-TEST-03 旧说法 "$T/a.md" "$_f2" historical-narrative)
  _ex SC-TEST-03 旧说法 "$T/a.md" "$_f2" historical-narrative "$REV@$_C6#AP-01"
  _runex; [ $? -eq 1 ] && ok "🔴 沿革码 ＋ 现在时（现有/现为/目前）⇒ 拒" || bad "现在时旧状态被当沿革放行"

  # ⑮ 🔴 变异 A：同件内【合法句 ＋ 真残留】各一处，例外只绑合法句那一行
  mk; printf '合法句：v3.2 之前旧说法 曾为口径\n本节仍按旧说法 的旧模型描述归属链\n' >> "$T/a.md"
  _l1=$(grep -n '合法句' "$T/a.md" | cut -d: -f1); _f1=$(_fp0 "$T/a.md" "$_l1")
  _C7=$(exc_fixture_make "$T" "$REV" SC-TEST-04 旧说法 "$T/a.md" "$_f1" historical-narrative)
  _ex SC-TEST-04 旧说法 "$T/a.md" "$_f1" historical-narrative "$REV@$_C7#AP-01"
  _runex; [ $? -eq 1 ] && ok "🔴 变异A 拦住：同件内真残留【不被】合法句的例外一并豁免" \
                       || bad "例外仍按件级泛化 —— 真残留被一起放行"

  EXC_TRUSTROOT=$_OT; EXC_GITDIR=$_OG

  mk; printf '【RV|甲|阻断|未决】本节仍写旧说法，须改\n' >> "$T/a.md"; run
  [ $? -eq 0 ] && ok "不误伤：RV 标注里引述旧断言不算违规（两闸不打架）" || bad "RV 标注被当违规"

  # 🔴 钉住首版的真缺口：标注写成【行内注释】与旧断言同行时，
  #   按行跳过会让「标注这个动作」把旧断言从视野里抹掉 —— 标得越对，闸越看不见。
  # ⚠️ 本条**已随 RV-乙18 作废**：style-ok 对本闸不再豁免，
  #   故「它写在 HTML 注释里也须生效」这个期望本身已不成立，删之。
  #   🔴 **改契约时必须回头删掉为旧契约写的反测** —— 留着它，
  #     要么反测自相矛盾，要么它会把旧行为再拉回来。

  mk; printf '本节采用旧说法处理。<!-- 【RV|丙|非阻断|未决】此处引用过期，须改 -->\n' >> "$T/a.md"; run
  [ $? -eq 1 ] && ok "🔴 拦住：旧断言与 RV 行内注释同行时仍须被判（不得被标注遮蔽）" \
    || bad "行内 RV 注释把同行的旧断言遮蔽了"

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
