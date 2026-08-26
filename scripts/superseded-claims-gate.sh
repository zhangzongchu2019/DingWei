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
AUTHOR_SESSION=${AUTHOR_SESSION:-zzc-opus50tchangshark-2642}   # 执笔方；批准方不得为它（不得自增自批）
CODES='cross-line-false-positive historical-narrative rv-annotation is-current-rule evidence-of-absence'

# 🔴 受控理由码的后置条件（specs/gates/REASON-CODES.md）——乙域裁定归执笔方实现。
#   立此机制的原因：理由此前是自由文本、只校非空；执笔方曾以「跨行误命中」为由
#   压住两处【行内就有】的旧断言而闸返 0。⇒ 判据交给码，叙述留给人。
#   ⚠️ 判【码与行形态是否相符】，🔴 判不了【豁免该不该给】，也判不了【码选得对不对】——归门方。
_code_ok() {   # $1=code  $2=件  $3=行号  $4=被抑制短语
  _c=$1; _cf=$2; _cl=$3; _cp=$4
  _line=$(sed -n "${_cl}p" "$_cf")
  case " $CODES " in *" $_c "*) : ;; *) return 1 ;; esac        # 未知码一律拒
  case "$_c" in
    cross-line-false-positive)
      # 🔴 该行【行内不得】含被抑制短语——执笔方那次编的理由会被这条拆穿
      printf '%s' "$_line" | grep -qF "$_cp" && return 1 || return 0 ;;
    historical-narrative)
      printf '%s' "$_line" | grep -qE '现有|现为|目前' && return 1
      printf '%s' "$_line" | grep -qE 'v[0-9]+\.[0-9]+|[0-9]{4}-[0-9]{2}|之前|当时|原[为写]' && return 0 || return 1 ;;
    rv-annotation)
      printf '%s' "$_line" | grep -q '【RV' && return 0 || return 1 ;;
    is-current-rule)
      grep -n '〔权威定义〕' "$_cf" >/dev/null 2>&1 || return 1
      awk -v p="$_cp" '/〔权威定义〕/{f=1} f && index($0,p){hit=1} END{exit !hit}' "$_cf" && return 0 || return 1 ;;
    evidence-of-absence)
      _obj=$(printf '%s' "$_line" | grep -oE '`[^`]+`' | head -1 | tr -d '`')
      [ -n "$_obj" ] && [ ! -e "$_obj" ] && return 0 || return 1 ;;
  esac
  return 1
}
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

  _bad=0
  for f in $DOCS; do
    [ -f "$f" ] || { echo "✗ 闸依赖缺失：$f 不存在"; return 2; }
    _hit=0
    while IFS="$(printf '\t')" read -r old cur why; do
      case "$old" in ''|'#'*) continue;; esac
      _lines=$(cat -n "$f" | _strip_rv | grep -F "$old" 2>/dev/null | grep -vE "$SKIP" | sed 's/^[[:space:]]*\([0-9]*\)[[:space:]]*/\1:/')
      [ -z "$_lines" ] && continue
      # 🔴 结构化登记例外 v2（归口方变异验证抓出 v1 两个洞，2026-08-26）：
      #   洞 A：v1 只匹配【短语＋件】，无位置维度 ⇒ 一条例外豁免该件内该短语的**所有出现**，
      #        包括未来新写进去的真残留。⇒ v2 **逐处绑定行指纹**，行一改例外即失效（fail-closed）。
      #   洞 B：v1 **从不校验批准方**，而输出印着「执笔方不得自增自批」。
      #        ⇒ v2 **批准方须实**：空 / 含未决词 / 为执笔方本人，**均不构成批准**。
      #   🔴 规则没有执行者 ＝ 没有规则 —— 这个病此前正出现在修这个病的件里。
      _exhit=0
      if [ -f "$EXLIST" ]; then
        _n=1
        printf '%s\n' "$_lines" | while IFS= read -r _ln; do :; done
        # 逐条命中行分别查例外：行指纹须匹配
        _remain=""
        for _one in $(printf '%s\n' "$_lines" | cut -d: -f1); do
          _txt=$(sed -n "${_one}p" "$f" | tr -d '[:space:]')
          _fp=$(printf '%s' "$_txt" | sha256sum | cut -c1-16)
          _rc=$(awk -F'\t' -v o="$old" -v ff="$f" -v fp="$_fp" -v me="$AUTHOR_SESSION" '
                $1==o && $2==ff && $3==fp && $4!="" && $5!="" \
                && $5 !~ /待|未|pending|PENDING|TODO/ && $5 != me {print $4; exit}' "$EXLIST" 2>/dev/null)
          _code=$(printf '%s' "$_rc" | sed 's/：.*//; s/:.*//' | tr -d ' ')
          if [ -n "$_code" ] && _code_ok "$_code" "$f" "$_one" "$old"; then
            _exn=$((${_exn:-0}+1))
          else
            _remain="$_remain $_one"
          fi
        done
        [ -z "$(printf '%s' "$_remain" | tr -d ' ')" ] && _exhit=1
      fi
      [ "$_exhit" -eq 1 ] && continue
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
    echo "  ⚠️ 确属沿革叙述者：改写为明确的历史时点表述，"
    echo "     或到 $EXLIST 逐条登记（须指名件 ＋ 理由非空 ＋ 门方批准）。"
    echo "     🔴 行内 style-ok 对本闸【已不再豁免】——自由文本理由无人复核（RV-乙18）。"
    return 1
  fi
  echo "  ✓ 清单内 $_n 条已撤销断言在正文均已清零"
  echo "     受检范围：$(printf '%s' "$DOCS" | wc -w | tr -d ' ') 件；🔴 **口径＝已剥 RV 标注段**（先剥标注再判剩余 —— 指出问题的那句话不该被判成问题本身）"
  _exn=${_exn:-0}
  if [ "$_exn" -gt 0 ]; then
    echo "  ⚠️ 其中 $_exn 处经【登记例外】放行（$EXLIST）——**登记是债务可见化，不是修复**；"
    echo "     🔴 例外的新增与批准属门方/业主评审面，执笔方不得自增自批。"
  fi
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

  # 🔴 **本组两条是 RV-乙18 的直接产物，反转了原断言。**
  #   原反测写的是「不误伤：带 style-ok 理由的例外放行 ⇒ ok」——
  #   **那把绕过面锁定成了期望语义**：闸不看理由是否属实，只看有没有那四个字，
  #   而反测又把这个行为断言为正确 ⇒ **再加多少条反测都测不出来，除非改契约本身。**
  #   现契约：style-ok 对本闸不再豁免；豁免只走结构化登记例外。
  mk; printf '本节仍写旧说法 <!-- style-ok: 引述历史 -->\n' >> "$T/a.md"
  EXLIST="$T/none.txt" run
  [ $? -eq 1 ] && ok "🔴 拦住：仅有行内 style-ok【不再】豁免（RV-乙18 反转此断言）" \
                || bad "自由文本 style-ok 仍能绕过 —— 契约未真正改掉"

  # ⚠️ 本组随 v2 改契约同步重写 —— 🔴 **改契约必须回头改为旧契约写的反测**，
  #    否则它要么误报，要么把旧行为再拉回来。（v1 三列 → v2 五列，含行指纹与批准方。）
  _fp0(){ sed -n "$2p" "$1" | tr -d '[:space:]' | sha256sum | cut -c1-16; }
  mk; printf '沿革：v3.2 之前本节采用旧说法处理。\n' >> "$T/a.md"
  _l0=$(grep -n '本节采用' "$T/a.md" | cut -d: -f1); _f0=$(_fp0 "$T/a.md" "$_l0")

  # ⚠️ 随 v3 同步：理由字段已由自由文本改为【受控码】。**改契约必须回头改旧反测**——本轮第二次踩。
  printf '旧说法\t%s\t%s\thistorical-narrative：沿革叙述\t门方sol\n' "$T/a.md" "$_f0" > "$T/ex.txt"
  EXLIST="$T/ex.txt" run
  [ $? -eq 0 ] && ok "登记例外五列齐（短语＋件＋行指纹＋受控码＋真批准方）放行" || bad "合规登记例外被误拦"

  # ⚠️ 原此处另有一条「空理由」反测 —— **与 v3 的「空码一律拒」测的是同一维，已删**。
  #    🔴 改契约时不仅要改旧反测，**重复维度的旧条目该删就删**：
  #    两条测同一维，会让人以为覆盖面更宽，而实际没有；且其中一条失效时另一条仍绿。

  # ===== v3 反测：受控理由码（乙域点名要的四条 ＋ 拆穿执笔方那次编造理由的一条）=====
  _fpz(){ sed -n "$2p" "$1" | tr -d '[:space:]' | sha256sum | cut -c1-16; }
  _mkex(){ printf '旧说法\t%s\t%s\t%s\t门方sol\n' "$T/a.md" "$2" "$1" > "$T/exC.txt"; }

  # 🔴 ① 未知码一律拒（不做「未知即放行」）
  mk; printf '本节采用旧说法处理。\n' >> "$T/a.md"
  _lz=$(grep -n '本节采用' "$T/a.md" | cut -d: -f1); _fz=$(_fpz "$T/a.md" "$_lz")
  _mkex "no-such-code：随便写的" "$_fz"; EXLIST="$T/exC.txt" run
  [ $? -eq 1 ] && ok "🔴 未知理由码一律拒（不做「未知即放行」）" || bad "未知码被放行"

  # 🔴 ② 空码一律拒（空码＝没码）
  _mkex "：只有补充说明没有码" "$_fz"; EXLIST="$T/exC.txt" run
  [ $? -eq 1 ] && ok "🔴 空码一律拒（空码 ＝ 没码）" || bad "空码被放行"

  # 🔴 ③ 行内真实命中：码写 cross-line-false-positive，而短语【就在本行】⇒ 必拒
  #    这正是执笔方 6d8ae86 那次编造理由的形态，本条把它当场拆穿。
  _mkex "cross-line-false-positive：编造的理由" "$_fz"; EXLIST="$T/exC.txt" run
  [ $? -eq 1 ] && ok "🔴 行内真实命中 ＋ 码称「跨行误命中」⇒ 必拒（拆穿执笔方那次编造的理由）" \
                || bad "编造的跨行理由仍能豁免 —— 码没有后置条件"

  # ④ 码与行形态相符时放行：historical-narrative ＋ 明确时点表述
  mk; printf '沿革：v3.2 之前曾采用旧说法。\n' >> "$T/a.md"
  _lh=$(grep -n 'v3.2 之前' "$T/a.md" | cut -d: -f1)
  _mkex "historical-narrative：沿革叙述" "$(_fpz "$T/a.md" "$_lh")"; EXLIST="$T/exC.txt" run
  [ $? -eq 0 ] && ok "不误伤：historical-narrative ＋ 明确时点表述 ⇒ 放行" || bad "合规码被误拦"

  # 🔴 ⑤ historical-narrative 但用【现在时】写旧状态 ⇒ 拒（沿革通则）
  mk; printf '本节现有旧说法 的写法。\n' >> "$T/a.md"
  _ln2=$(grep -n '本节现有' "$T/a.md" | cut -d: -f1)
  _mkex "historical-narrative：沿革叙述" "$(_fpz "$T/a.md" "$_ln2")"; EXLIST="$T/exC.txt" run
  [ $? -eq 1 ] && ok "🔴 沿革码 ＋ 现在时（现有/现为/目前）⇒ 拒（沿革不得用现在时描述旧状态）" \
                || bad "现在时旧状态被当沿革放行"

  # ===== v2 反测：归口方两个变异测试的固化（2026-08-26） =====
  _fp(){ sed -n "$2p" "$1" | tr -d '[:space:]' | sha256sum | cut -c1-16; }

  # 🔴 变异 A：同件内【合法句 ＋ 真残留】各一处，例外只登记合法句那一行
  #    v1 因粒度只到「短语＋件」，把两处一起放行；v2 须只放行被指纹绑定的那一处。
  mk; printf '合法句：旧说法 在此为现行禁令本身\n本节仍按旧说法 的旧模型描述归属链\n' >> "$T/a.md"
  _l1=$(grep -n '合法句' "$T/a.md" | cut -d: -f1)
  printf '旧说法\t%s\t%s\t现行禁令本身\t门方sol\n' "$T/a.md" "$(_fp "$T/a.md" "$_l1")" > "$T/exA.txt"
  EXLIST="$T/exA.txt" run
  [ $? -eq 1 ] && ok "🔴 变异A 拦住：同件内真残留【不被】合法句的例外一并豁免（v1 在此放行）" \
                || bad "例外仍按件级泛化 —— 真残留被一起放行"

  # 🔴 变异 B：批准方留空 / 写未决词 / 写执笔方本人 —— 三者均不构成批准
  mk; printf '本节采用旧说法处理。\n' >> "$T/a.md"
  _l2=$(grep -n '本节采用' "$T/a.md" | cut -d: -f1); _f2=$(_fp "$T/a.md" "$_l2")
  for _who in "" "待门方批准（执笔方迁入，未自批）" "zzc-opus50tchangshark-2642"; do
    printf '旧说法\t%s\t%s\t理由在\t%s\n' "$T/a.md" "$_f2" "$_who" > "$T/exB.txt"
    EXLIST="$T/exB.txt" run
    [ $? -eq 1 ] || { bad "批准方=[$_who] 竟构成批准"; break; }
  done
  [ $fail -eq 0 ] && ok "🔴 变异B 拦住：批准方为空 / 未决词 / 执笔方本人，均不构成批准（登记≠获批准）"

  # 行指纹绑定：行内容一改，例外即失效（fail-closed，须重新批准）
  printf '旧说法\t%s\t0000000000000000\t理由在\t门方sol\n' "$T/a.md" > "$T/exC.txt"
  EXLIST="$T/exC.txt" run
  [ $? -eq 1 ] && ok "🔴 行指纹不匹配 ⇒ 例外失效（行一改须重新批准）" || bad "指纹不匹配仍放行"

  printf '旧说法\t%s/别的件.md\t%s\t理由\t门方sol\n' "$T" "$_f2" > "$T/ex3.txt"
  EXLIST="$T/ex3.txt" run
  [ $? -eq 1 ] && ok "🔴 拦住：登记例外【件不匹配】不放行（例外不得跨件泛化）" || bad "跨件泛化被放行"

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
