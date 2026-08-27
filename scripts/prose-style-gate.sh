#!/bin/sh
# 体例合规闸 —— 正文不得含【评审过程痕迹】。
#
# 立此闸的直接原因（R1 执笔方自评 G-1，2026-08-25）：
#   SpecDoc 头部体例白纸黑字写着：
#     「本件为规范体——只写『应当如何』。讨论过程、被否方案、推导与自我更正**一律不入正文**，
#       需要溯源时查沿革依据。」
#   而实测正文命中：SpecDoc 83 行 / 架构设计 41 行（架构密度 5.1%）。
#   🔴 **文件自己立的体例，自己破了，且没有任何东西在看着它。**
#
# 后果不是洁癖问题，是三条实打实的：
#   ① 正文体积随评审轮次单调增长，而设计本身没变；
#   ② 每个新门方必须重读全部历史争议才能读懂现行条款；
#   ③ 正文里同时并存 v0.5 / v0.10 / v0.17 / v0.19 四个时点的自述 ——
#      **「基准不同」是从文件内部就开始的**，不只是消息往返造成的。
#
# ⚠️ 能力边界，写明不假装：
#   本闸判的是「**是否出现指向特定评审轮次/版本/评审方的表述**」，
#   判不了「这段话是不是过程痕迹」——那是语义。
#   故它只抓**有明确指纹**的那几类，抓不到的靠人审（同 E.4 lint 覆盖不到项）。
#   🔴 **不得因本闸通过而宣称「正文已无过程痕迹」。**
#
# ── 评审标注（RV）模式 ──（业主 2026-08-25 定：三轮收官；想说的在文档中标出，不再另发消息）
#   业主要「标在文档里」，而体例条要「讨论过程不入正文」—— 两条都成立，解法是**结构上可分离**：
#     · **轮内**：标注可在文档里，省掉消息往返；但每条须字段齐全，机器可清点。
#     · **收官**：`未决` 必须为 0；`驳回` 须带理由；`挂账` 须带责任人与时点；
#       随后全部标注移入沿革件，**正文零标注**。
#   🔴 **收官不等于清空**：任一标注不得就这么消失 —— 必须落到
#     采纳 / 驳回(带理由) / 挂账(带责任人+时点) 三者之一。否则「收官」就是假装闭合。
#
#   标注形态（可见、可 grep、与正文结构上分离）：
#     【RV|<域>|<严重度>|<状态>】<内容>
#       域     = 甲 | 乙 | 丙 | 执笔
#       严重度 = 阻断 | 非阻断
#       状态   = 未决 | 采纳 | 驳回 | 挂账
#
# 　　⟹ **【采纳】** 甲域这条打的正是本闸头注自己写的那句「收官 ≠ 清空：任一标注不得消失」——
#     🔴 **把规矩写进头注，不等于闸能执行它。** 已落三件：
#     ① 每条 RV 分配**稳定 ID**（`RV-<域><序号>`，正文与台账同源）
#     ② 新增 reviewer-owned 台账 `specs/gates/rv-ledger.txt`
#     ③ `--final` 增判：**台账内每条 ID 必须在正文或沿革件中出现**，
#        直接删标注而不写沿革 ⇒ 判「标注消失」并拒。
#     ⚠️ 能力边界：本闸判「ID 还在不在」，判不了「沿革里写得对不对」。
set -u

DOCS=${DOCS:-'docs/dingwei-platform-specdoc.md docs/dingwei-platform-architecture.md docs/dingwei-prototype.html'}
# 沿革记录件是【专门存放】过程痕迹的地方,本闸不扫它。
HIST=${HIST:-'docs/dingwei-revision-history.md'}
EXLIST=${EXLIST:-specs/gates/prose-style-exceptions.txt}
# 🔴 **冻结基线已删除**（乙域 terra 收尾裁定 2026-08-26 第三条）：
#   「冻结基线不容作为通过条件：**待批准 ＝ 拒绝**；`PASS WITH REGISTERED DEBT`
#     不能返回成功、也不能被聚合为合规 PASS。」
#   ⇒ 本闸此前那条旧抑制已**在源头消掉**（不是被批准，也不是记成债务），故基线件一并删除。

# 🔴 乙域 terra 全闸契约（2026-08-26）：`style-ok` / `status-ok` / `path-ok` 等抑制均属特权例外。
#   本闸是 `style-ok` 的**第二个**使用者（第一个是已撤销断言闸，已于 RV-乙18 完成受控化）。
#   ⚠️ 上一轮把「三类抑制」读成了「三个闸各一类」，于是漏掉了本闸 ——
#     **同一个标记被两个闸各自解释，改了一个不等于改了这个标记。**
#   ⇒ 本闸的 style-ok 同样改为受控例外（同一套公共库）。
_LIB=${EXC_LIB:-specs/gates/lib/exceptions.sh}
[ -f "$_LIB" ] || { echo "✗ 闸依赖缺失：$_LIB 不存在 —— 闸坏了，不是免检"; exit 2; }
. "$_LIB"

# 痕迹指纹 —— 只取指向【特定评审轮次 / 版本 / 评审方】的表述。
#   刻意【不】收「复核」「评审」等词:它们在规范里是正当术语
#   （如「不可回滚迁移的复核要求」），收进来就是误伤,会逼人绕过闸。
# ⚠️ 只认「甲域/乙域/丙域」认不出评审人真名：「本部回应 sol 的总则」「照录 sol 的区分」
#   就是这样活过两轮的（丙域 R2 轮3 抓出）。真名须按【归属句式】判，不能裸匹配 ——
#   sol 是 solid/console/resolve 的子串，裸匹配会在 CSS 里误报 15 次以上（实测）。
# 🔴 「执笔方」此前**根本不在本表里**（甲域 sol R2 轮5 · RV-甲45）：
#   它是过程叙述最常见的标记，而体例闸从来没在看 —— 原型正文里 40 行自述
#   （「执笔方建议／倾向／此前实测／当时判定／须收回／直接编了示例数据」）
#   连过四轮体例闸全绿。
# ⚠️ 不能裸匹配「执笔方」：规范里有**关于该角色的规定**（§0.1「执笔方不得宣布自己的
#   spec 生效」、§4.1「执笔方不得代填 checklist」），那是现行判据，不是自述。
#   ⇒ 按【自述句式】判：执笔方 ＋ 表态/追忆动词。规定句式（不得/须/应）不在内。
PAT_ROLE='甲域|乙域|丙域|本席|(sol|terra|kimi)[ 　]*的|照录[ 　]*(sol|terra|kimi)|回应[ 　]*(sol|terra|kimi)|执笔方[ 　]*(建议|认为|倾向|推荐|答|补|此前|当时|原来|自查|新写|造它|回写时会|不猜|这几轮|清单上)'
PAT_VER='v[0-9]+\.[0-9]+[[:space:]]*(更正|补|压缩|删去|收窄|改写|新增|撤回|再次)'
PAT_HIST='打脸|撤回为|原句已撤回|此前写|本段此前|原文写成|原表|已是第[二三四五六七八九]次|第[二三四五六七八九]次'
# 🔴 二轮扩面（丙域 kimi R2 轮3「体例分离二轮·门方先标」）：
#   一轮的指纹只抓「谁在第几版说了什么」，抓不到**不点名的过程叙述** ——
#   「本轮已定」「本次为初版」「此前一直写着」「不是被门方抓出的」这类，
#   同样是【讨论过程与自我更正】，同样随下一轮过期，只是没写出评审方的名字。
#   ⚠️ 一轮报「痕迹 90→0」正是被这一层漏掉的 —— **闸的口径不等于事实的口径**。
# 🔴 甲域 sol R2 轮6（RV-甲48）：轮5 清「执笔方」自述后，同族的**另一批**仍在 ——
#   「业主本轮／业主本条／业主问／本次对齐／本轮所有对齐」共 12 处。
#   ⚠️ 它们与「执笔方…」是同一件事的另一面：**把某一次评审会话的语境写进现行契约**。
#     裁定内容留正文，「谁在哪一轮问的/定的」不留。
PAT_PROC='本轮已定|本轮闭的是|本轮最大工作量|本次为初版|本次补|本次是|此前一直写着|不是被门方抓出|本阶段实战教训|本轮新增|曾同时表|业主本轮|业主本条|业主问|本次对齐|本轮所有对齐|本轮正好|业主修正'
PAT_ALL="$PAT_ROLE|$PAT_VER|$PAT_HIST|$PAT_PROC"

scan() { # scan <file> -> 命中行
  # 🔴 RV 标注行不计入痕迹：标注【本来就要】指名评审域与轮次,那是它的用途。
  #   两个机制若不互相认识就会打架 —— 本闸立起来后第一次真用 RV 就撞上了。
  #   ⚠️ 这不是给 RV 开后门:RV 有自己的判据(rvcheck:字段受控枚举、收官须清零)。
  #     排除的是【重复管辖】,不是【不管】。
  # 🔴 `grep -v 'style-ok:'` **已删除**（乙域全闸契约）：自由文本标记不再构成豁免。
  #   ⚠️ `【RV` 仍排除 —— 那是**标注豁免**（评审标注引述本就是它的用途），
  #     按 REASON-CODES 的两分法属**自动、无需登记批准**的一类，与内容豁免不同族。
  grep -nE "$PAT_ALL" "$1" 2>/dev/null | grep -v '【RV'
}

# suppressed <件> <行号> -> 0=经受控例外或冻结基线放行；1=不放行；2=UNKNOWN
_SUP_UNK=0
suppressed() {
  _sf=$1; _sl=$2
  _obj=$(sed -n "${_sl}p" "$_sf" | grep -oE "$PAT_ALL" | head -1)
  [ -n "$_obj" ] || return 1
  exc_decide "$EXLIST" "$_obj" "$_sf" "$_sl"; _src=$?
  [ "$_src" -eq 0 ] && return 0
  [ "$_src" -eq 2 ] && { _SUP_UNK=$((_SUP_UNK+1)); return 2; }
  return 1
}

check() {
  # 🔴 局部计数一律加 _ 前缀:sh 无函数局部变量,叫 n 会覆盖 selftest 的通过计数,
  #    于是「反测 N 项全过」印出来的是最后一次命中数 —— 一个看着合理、其实是别的东西的数字。
  #    本闸自己先栽过这一次,注在此处防复发。
  rc=0; _total=0; _SUP_UNK=0
  for f in $DOCS; do
    [ -f "$f" ] || { echo "✗ 闸依赖缺失：$f 不存在 —— 闸坏了，不是免检"; return 2; }
    hits=$(scan "$f")
    # 逐处判受控例外 / 冻结基线 —— 例外的粒度是【某件某行的某个痕迹指纹】，不是整行标记
    _keep=''
    for _hl in $(printf '%s\n' "$hits" | awk -F: 'NF>0 {print $1}'); do
      suppressed "$f" "$_hl" || _keep="$_keep $_hl"
    done
    hits=$(printf '%s\n' "$hits" | awk -v k="$_keep" 'BEGIN{n=split(k,a," "); for(i=1;i<=n;i++) m[a[i]]=1}
                                                      { split($0,b,":"); if (b[1] in m) print }')
    _n=$(if [ -z "$hits" ]; then echo 0; else printf '%s\n' "$hits" | wc -l | tr -d ' '; fi)
    _total=$((_total+_n))
    if [ "$_n" -gt 0 ]; then
      echo "✗ $f：正文含评审过程痕迹 $_n 处（体例：讨论过程与自我更正一律不入正文）"
      printf '%s\n' "$hits" | head -8 | sed 's/^/    /'
      [ "$_n" -gt 8 ] && echo "    …（共 $_n 处，此处只列前 8）"
      rc=1
    else
      echo "  ✓ $f 正文无过程痕迹指纹"
    fi
    # 🔴 跨企业不得出现 capability/白名单通路（甲域 sol delta ②）：
    #   §22.1c 明定「跨企业在 §22.0/§15.5 下**没有任何 capability 路径**」，
    #   而执笔方按节机械分类做「跨租户→跨企业」映射时，把**描述 capability 通路**的条款
    #   （§22.1b「默认拒，白名单放行」、§22.2a #2「须经 guarded capability」、E.4 #18/#40…）
    #   一并判成了跨企业 —— **自己造出 7 处现行正面冲突**。
    #   ⚠️ 机械替换看的是「这一节大致属哪类」，不是「这一句在说什么」。
    #   ⇒ 判据：跨企业与 capability/白名单/放行 同句出现即拒，
    #     除非该句是**否定式**（无/没有…路径 · 不存在 · 恒为 0 · 禁止 · 误读 · 无白名单）。
    # ⚠️ 首版模式只抓「白名单**放行**／guarded capability」这类**动词形态**，
    #   抓不到「跨企业**白名单条目**」这种**名词形态**（E.4 #14 因此漏网，乙域 RV-乙14 抓出）；
    #   且否定式清单只写了「无…」没写「没有…」，把判据自身那句也差点判成冲突。
    #   ⇒ **列模式时要按【这件事有多少种说法】列，不是按我先想到的那一种。**
    case "$f" in
      *.md|*.html)
        _xc=$(grep -nE '跨企业[^。；]{0,26}(白名单|capability)' "$f" 2>/dev/null \
              | grep -vE '(无|没有)[^。；]{0,8}capability 路径|不存在|恒为 0|恒 0|禁止|误读|无此路径|无白名单|冲突」|勿再按|不构成矛盾' || true)
        if [ -n "$_xc" ]; then
          echo "✗ $f：出现【跨企业 ＋ capability/白名单通路】的现行表述 —— 与 §22.1c 直接冲突"
          printf '%s\n' "$_xc" | cut -c1-130
          echo "  🔴 跨企业在 §22.0/§15.5 下无任何 capability 路径；描述通路的条款其对象只能是【跨人】。"
          return 1
        fi ;;
    esac

  done
  # 🔴 UNKNOWN 先于 FAIL 判：授权根不可得时，闸没有资格给结论。
  #   ⚠️ 首版把这段写在 FAIL 之后 —— 于是「例外存在但批准验不了」被判成了 FAIL，
  #     **把「我不知道」讲成了「它不合格」**，正是 GATE-CONTRACT 一之二第 1/2 条的分界。
  if [ "$_SUP_UNK" -gt 0 ]; then
    echo "✗ UNKNOWN（exit 2）：$_SUP_UNK 处受控例外的【批准】无从核验 —— 授权根不可得"
    echo "  ⇒ 见 specs/gates/approvals/TRUST-ROOT.md：当前无 ACTIVE 后端（门方待裁）。"
    return 2
  fi
  if [ "$rc" -ne 0 ]; then
    echo "  🔴 剥离【不是删】：痕迹里若含现行规范内容（能力边界声明 · 口径收窄的结果表等），"
    echo "     内容留正文、沿革移入 $HIST。逐条判断，不得机械替换。"
    echo "  ⚠️ 确有正当理由须留在正文者：🔴 **行内 style-ok 标记对本闸已不再构成豁免**（乙域全闸契约），"
    echo "     须到 $EXLIST 登记受控例外（六列 ＋ 门方 approval_ref）。"
    return 1
  fi
  # 沿革件必须存在且非空 —— 否则「剥离到沿革件」只是把它删了。
  if [ ! -s "$HIST" ]; then
    echo "✗ 沿革记录件 $HIST 不存在或为空 —— 剥离必须有去处,不得只删不存"; return 2
  fi
  echo "  ✓ 体例合规（正文 $_total 处痕迹指纹；沿革件在 $HIST）"
  echo "     受检范围（预期集合）：$(printf '%s' "$DOCS" | wc -w | tr -d ' ') 件；口径＝排除 【RV 标注段（标注豁免，自动）"
  echo "     未知集合：$_SUP_UNK"
  return 0
}

selftest() {
  T=$(mktemp -d) || return 2
  n=0; fail=0
  ok(){ n=$((n+1)); echo "  ✓ $1"; }; bad(){ echo "  ✗ 反测未过：$1"; fail=1; }
  mk(){ rm -rf "$T/d"; mkdir -p "$T/d"; printf '# 正文\n\n应当如何如何。\n' > "$T/d/a.md"; printf '# 沿革\n\n有内容。\n' > "$T/d/h.md"; }
  run(){ DOCS="$T/d/a.md" HIST="$T/d/h.md" check >/dev/null 2>&1; }

  mk; run; [ $? -eq 0 ] && ok "干净正文放行（不误报）" || bad "干净正文被误判"

  for s in '甲域 sol 指出本行有问题' '乙域 terra 判 NO' '丙域 kimi 复核实例' '本席核实成立'; do
    mk; printf '# 正文\n\n%s\n' "$s" >> "$T/d/a.md"; run
    [ $? -eq 1 ] && ok "拦住：评审方指纹「$s」" || bad "未拦住「$s」"
  done

  mk; printf '# 正文\n\nv0.5 更正：本行原写…\n' >> "$T/d/a.md"; run
  [ $? -eq 1 ] && ok "拦住：版本戳更正（v0.5 更正）" || bad "版本戳更正未被拦"
  mk; printf '# 正文\n\nv3.2 补，SA-08 …\n' >> "$T/d/a.md"; run
  [ $? -eq 1 ] && ok "拦住：版本戳补写（v3.2 补）" || bad "版本戳补写未被拦"

  # 🔴 二轮扩面反测：不点名的过程叙述同样是痕迹
  for s2 in '本轮已定为受控仓储 API' '本次为初版' '本行此前一直写着未评审' '不是被门方抓出的'; do
    mk; printf '# 正文\n\n%s\n' "$s2" >> "$T/d/a.md"; run
    [ $? -eq 1 ] && ok "拦住：不点名的过程叙述「$s2」" || bad "未拦住「$s2」"
  done

  mk; printf '# 正文\n\n被甲域实算打脸。\n' >> "$T/d/a.md"; run
  [ $? -eq 1 ] && ok "拦住：沿革叙述（打脸）" || bad "沿革叙述未被拦"
  mk; printf '# 正文\n\n已是第三次犯同一错。\n' >> "$T/d/a.md"; run
  [ $? -eq 1 ] && ok "拦住：第 N 次自述" || bad "第 N 次自述未被拦"

  # 🔴 不误伤:规范里「复核」「评审」是正当术语,收进模式就会逼人绕过闸
  mk; printf '# 正文\n\n不可回滚迁移的复核要求见 §5.2。架构评审为 L3。\n' >> "$T/d/a.md"; run
  [ $? -eq 0 ] && ok "不误伤：规范术语「复核 / 评审」不算痕迹" || bad "误伤了规范术语"

  # 例外须带理由
  # 🔴 **本条随乙域全闸契约反转**（2026-08-26）：
  #   原写「不误伤：带 style-ok 理由的例外放行 ⇒ ok」——
  #   **那把绕过面锁定成了期望语义**：闸不看理由是否属实，只看有没有那几个字，
  #   而反测又把这个行为断言为正确 ⇒ **再加多少条反测都测不出来，除非改契约本身。**
  #   ⚠️ 这与已撤销断言闸在 RV-乙18 时反转的是**同一条**，而本闸当时没跟着改 ——
  #     同一个 `style-ok` 标记被两个闸各自解释，改了一个不等于改了这个标记。
  mk; printf '# 正文\n\n本席保留此句 style-ok: 该句为能力边界声明,属规范内容\n' >> "$T/d/a.md"
  EXLIST="$T/none.txt" BASELINE="$T/none.txt" run
  [ $? -eq 1 ] && ok "🔴 拦住：仅有行内 style-ok【不再】豁免（乙域全闸契约反转此断言）" \
                || bad "自由文本 style-ok 仍能绕过本闸 —— 通则只落到一个闸就不是通则"

  # 经受控例外的合法对象 ⇒ 过（terra 要求的成对反测的另一半）
  _REV=zzc-chatgpt56terrazhangzongchu-2642
  . specs/gates/lib/approval-fixture.sh
  _OT=$EXC_TRUSTROOT; _OG=${EXC_GITDIR:-.}
  EXC_TRUSTROOT="$T/trust.md"; EXC_GITDIR="$T/appro"
  _lp=$(grep -n '本席保留此句' "$T/d/a.md" | cut -d: -f1)
  _fpp=$(sed -n "${_lp}p" "$T/d/a.md" | tr -d '[:space:]' | sha256sum | cut -c1-16)
  _objp=$(sed -n "${_lp}p" "$T/d/a.md" | grep -oE "$PAT_ALL" | head -1)
  _CMT=$(exc_fixture_make "$T" "$_REV" PS-TEST-01 "$_objp" "$T/d/a.md" "$_fpp" normative-not-process)
  [ -n "$_CMT" ] || bad "🔴 批准夹具没造出来 —— 下列反测全部无效"
  printf 'PS-TEST-01\t%s\t%s/d/a.md\t%s\tnormative-not-process\t%s@%s#AP-01\n' "$_objp" "$T" "$_fpp" "$_REV" "$_CMT" > "$T/psex.txt"
  EXLIST="$T/psex.txt" run
  [ $? -eq 0 ] && ok "经受控例外的合法对象放行（受保护 ref 可达 ＋ 不可变 commit ＋ 四元组匹配）" || bad "合规受控例外被误拦"

  # 🔴 授权根无 ACTIVE 后端（＝仓内当前实况）⇒ exit 2
  exc_fixture_trust "$T" "$_REV" ''
  EXLIST="$T/psex.txt" run
  [ $? -eq 2 ] && ok "🔴 授权根不可得 ⇒ UNKNOWN exit 2（不折算为 PASS）" || bad "授权根不可得时闸给了结论"
  exc_fixture_trust "$T" "$_REV" reviewer-owned-protected-ref

  # 🔴 commit 不可达 ⇒ UNKNOWN
  printf 'PS-TEST-01\t%s\t%s/d/a.md\t%s\tnormative-not-process\t%s@0123456789abcdef0123456789abcdef01234567#AP-01\n' "$_objp" "$T" "$_fpp" "$_REV" > "$T/psex2.txt"
  EXLIST="$T/psex2.txt" run
  [ $? -eq 2 ] && ok "🔴 commit 在受保护 ref 上不可达 ⇒ 验证失败 ＝ UNKNOWN(2)" || bad "不可达 commit 未判 UNKNOWN"

  EXC_TRUSTROOT=$_OT; EXC_GITDIR=$_OG

  # 🔴 剥离必须有去处:沿革件空 ⇒ 判闸坏,防「只删不存」
  mk; : > "$T/d/h.md"; run
  [ $? -eq 2 ] && ok "拦住：沿革件为空（只删不存）判闸坏" || bad "沿革件为空却放行"

  # 依赖缺失是闸坏,不是免检
  mk; rm -f "$T/d/a.md"; run
  [ $? -eq 2 ] && ok "拦住：正文件缺失判闸坏（非免检）" || bad "正文件缺失被当免检"

  # ── RV 标注反测 ──
  # 🔴 反测的构造须与判据同源：闸已改扫 RV_SCOPE，反测若仍只传 DOCS，
  #   RV_SCOPE 会回退到默认值（含真仓全部闸脚本），判据全乱。本闸已因此失守三条。
  # 🔴 必须同时覆盖 LEDGER：不覆盖的话 `--final` 会拿【真仓 78 条台账】去对临时夹具，
  #   每条 ID 都追不到 ⇒ **恒 rc=1**。于是所有「期望 rc=1」的反测都在为**错误的理由**通过，
  #   而它们看起来一直是绿的。是新加的一条「不误伤」反测（期望 rc=0）把它照出来的。
  #   ⚠️ 同源教训：**期望失败的反测最容易空转** —— 它不区分「因为我测的那条而失败」
  #     和「因为别的原因而失败」。凡新写期望 rc≠0 的反测，配一条期望 rc=0 的对照。
  : > "$T/lgempty.txt"; printf '# 空台账（反测用）\n' > "$T/lgempty.txt"
  rv(){ DOCS="$T/d/a.md" RV_SCOPE="$T/d/a.md" HIST="$T/d/h.md" \
        LEDGER="${LEDGER:-$T/lgempty.txt}" rvcheck "${1:-}" >/dev/null 2>&1; }

  mk; printf '\n本部回应 sol 的总则：转交须有设计合同。\n' >> "$T/d/a.md"
  DOCS="$T/d/a.md" HIST="$T/d/h.md" check >/dev/null 2>&1
  [ $? -eq 1 ] && ok "拦住：正文指名评审人（归属句式「sol 的」）" || bad "正文指名评审人被放行"

  mk; printf '\n按业主本条，设计层可公开全员。\n' >> "$T/d/a.md"
  DOCS="$T/d/a.md" HIST="$T/d/h.md" check >/dev/null 2>&1
  [ $? -eq 1 ] && ok "拦住：把某一轮的会话语境写进契约（业主本条/本轮，甲域 RV-甲48）" || bad "会话语境被放行"

  # ⚠️ 不误伤：裁定【内容】本身不带轮次语境
  mk; printf '\n🔴 设计层可公开全员（业主裁定）。\n' >> "$T/d/a.md"
  DOCS="$T/d/a.md" HIST="$T/d/h.md" check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：「业主裁定」不带轮次语境，是现行归属声明" || bad "业主裁定被误判"

  mk; printf '\n📌 <b>执笔方建议（待裁）：这样办。</b>\n' >> "$T/d/a.md"
  DOCS="$T/d/a.md" HIST="$T/d/h.md" check >/dev/null 2>&1
  [ $? -eq 1 ] && ok "拦住：执笔方自述句式（甲域 RV-甲45，40 行连过四轮）" || bad "执笔方自述被放行"

  # ⚠️ 不误伤：关于【执笔方这个角色】的规定是现行判据，不是自述
  mk; printf '\n🔴 执笔方不得宣布自己的 spec 生效；执笔方不得代填 checklist。\n' >> "$T/d/a.md"
  DOCS="$T/d/a.md" HIST="$T/d/h.md" check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：关于执笔方的【规定句式】（不得/须）不算自述" || bad "规范条款被误判为自述"

  # 🔴 真名不得裸匹配：sol 是 solid/console/resolve 的子串
  mk; printf '\n<div style="border:1px solid var(--x)">console.log(resolveMember)</div>\n' >> "$T/d/a.md"
  DOCS="$T/d/a.md" HIST="$T/d/h.md" check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：CSS solid / console / resolve 不算指名评审人" || bad "真名裸匹配误伤了 solid/console"

  # 🔴 两个机制不得互相打架:RV 标注按用途必然含评审域名,体例闸不得因此判它是痕迹。
  mk; printf '\n【RV-丙01|丙|阻断|未决】丙域指出某处有问题\n' >> "$T/d/a.md"   # rv-fixture
  DOCS="$T/d/a.md" HIST="$T/d/h.md" check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤:RV 标注含评审域名不算过程痕迹(两闸不打架)" \
    || bad "体例闸把 RV 标注当成痕迹 —— 两个机制互相打架"

  mk; printf '\n【RV-甲01|甲|阻断|未决】某条意见\n' >> "$T/d/a.md"; rv   # rv-fixture
  [ $? -eq 0 ] && ok "轮内：合法 RV 标注放行（不阻断评审进行）" || bad "轮内合法标注被拦"

  mk; printf '\n【RV-甲01|甲|阻断|未决】某条意见\n' >> "$T/d/a.md"; rv --final   # rv-fixture
  [ $? -eq 1 ] && ok "收官：仍有【未决】标注必拦" || bad "收官放行了未决标注"

  # ⚠️ 不用「丁」做非法域：它已被排除列表当作反测夹具滤掉 ——
  #   **反测与排除规则会打架**，而打架时失守的是反测（它变成恒过）。
  # 🔴 评审方引述缺陷文本不得被误判（甲域 RV-甲42 正文即引着「时点=R2 轮3前」）
  # 🔴 甲域最终 verdict 的那条：多行 RV 的续行不得残留正文
  mk; printf '\n§0 头块「本行此前一直写着」； <!-- style-ok: 本行为丙域 RV 标注,引述是标注的用途 -->\n' >> "$T/d/a.md"   # rv-fixture
  rv --final
  [ $? -eq 1 ] && ok "收官：拦住多行 RV 的续行残留（甲域最终 verdict）" || bad "续行残留被放行"

  # ⚠️ 不误伤：普通 style-ok 例外（不自称 RV 标注）不算续行
  mk; printf '\n某条正文。 <!-- style-ok: 本句为规范用语，非过程叙述 -->\n' >> "$T/d/a.md"
  rv --final
  [ $? -eq 0 ] && ok "不误伤：普通 style-ok 例外不算 RV 续行" || bad "普通 style-ok 被误判为续行"

  mk; printf '\n【RV-甲54|甲|阻断|采纳】闸只查一个绝对日期，故「时点=R2 轮3 前」这类仍全绿　　⟹ **【采纳】** 已修\n' >> "$T/d/a.md"; rv   # rv-fixture
  [ $? -eq 0 ] && ok "不误伤：标注中【引述】的相对时点不算真时点（先剥引文再判）" || bad "引述的相对时点被误判"

  mk; printf '\n【RV-甲50|甲|阻断|采纳】某条　　⟹ **【挂账】** 责任人=X；时点＝2026-09-01\n' >> "$T/d/a.md"; rv   # rv-fixture
  [ $? -eq 1 ] && ok "拦住：处置文本冒充状态列（第4列采纳 vs 文内挂账，甲域 RV-甲42）" || bad "状态互相矛盾被放行"

  mk; printf '\n【RV-甲51|甲|阻断|挂账】某条　　⟹ **【挂账】** 责任人=X；时点＝2026-09-01；时点=R2 轮3 前\n' >> "$T/d/a.md"; rv   # rv-fixture
  [ $? -eq 1 ] && ok "拦住：同一 RV 出现多个时点" || bad "多时点被放行"

  mk; printf '\n【RV-甲52|甲|阻断|挂账】某条　　⟹ **【挂账】** 责任人=X；时点=R2 轮3 前\n' >> "$T/d/a.md"; rv   # rv-fixture
  [ $? -eq 1 ] && ok "拦住：相对时点（会到期）" || bad "相对时点被放行"

  # ⚠️ 不误伤：第 4 列与处置内记号一致时须放行
  mk; printf '\n【RV-甲53|甲|阻断|挂账】某条　　⟹ **【挂账】** 责任人=X；时点＝2026-09-01\n' >> "$T/d/a.md"; rv   # rv-fixture
  [ $? -eq 0 ] && ok "不误伤：处置内记号与第 4 列一致时放行" || bad "一致的状态被误拦"

  # 🔴 跨企业 capability 冲突须拦（甲域 delta ②，执笔方机械替换造出 7 处）
  mk; printf '\n- 跨企业 → **默认拒**，白名单放行（须满足 §22.1c）\n' >> "$T/d/a.md"
  DOCS="$T/d/a.md" HIST="$T/d/h.md" check >/dev/null 2>&1
  [ $? -eq 1 ] && ok "拦住：跨企业 ＋ 白名单放行（与 §22.1c 直接冲突）" || bad "跨企业 capability 通路被放行"

  # 🔴 名词形态也须拦（乙域 RV-乙14：E.4 #14「跨企业白名单条目」漏网）
  mk; printf '\n| 14 | 跨企业白名单条目的**必要性与最小化**是否成立 |\n' >> "$T/d/a.md"
  DOCS="$T/d/a.md" HIST="$T/d/h.md" check >/dev/null 2>&1
  [ $? -eq 1 ] && ok "拦住：跨企业【白名单条目】名词形态（动词形态模式抓不到）" || bad "名词形态漏网"

  # ⚠️ 不误伤：指称这类冲突本身的句子（第五次同形，见判据注释）
  mk; printf '\n⇒ 复核者见此三行请勿再按「跨企业 capability 冲突」处理。\n' >> "$T/d/a.md"
  DOCS="$T/d/a.md" HIST="$T/d/h.md" check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：指称「跨企业 capability 冲突」这一类别的句子" || bad "谈论该冲突的句子被判成冲突"

  # ⚠️ 不误伤：否定式是判据本身，不是冲突
  mk; printf '\n`跨企业` 没有任何 capability 路径，故不存在「跨企业白名单」这种东西。\n' >> "$T/d/a.md"
  DOCS="$T/d/a.md" HIST="$T/d/h.md" check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：否定式表述（无路径/不存在）不算冲突" || bad "判据自身被误判为冲突"

  # 🔴 代码件里标注拼进命令行须拦（甲域 RV-甲47 的真实形态）
  mk; printf 'echo hi > x.md　　【RV-甲60|甲|阻断|采纳】说明文字\n' > "$T/d/g.sh"   # rv-fixture
  DOCS="$T/d/a.md" RV_SCOPE="$T/d/g.sh" HIST="$T/d/h.md" rvcheck >/dev/null 2>&1
  [ $? -eq 1 ] && ok "拦住：代码件里 RV 标注拼在非注释行（甲域 RV-甲47）" || bad "代码行内标注被放行"

  mk; printf '# 【RV-甲61|甲|阻断|采纳】说明文字\necho hi\n' > "$T/d/g.sh"   # rv-fixture
  DOCS="$T/d/a.md" RV_SCOPE="$T/d/g.sh" HIST="$T/d/h.md" rvcheck >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：代码件里标注独占注释行时放行" || bad "注释行标注被误拦"

  mk; printf '\n【RV|甲|阻断|采纳】没有稳定 ID\n' >> "$T/d/a.md"; rv
  [ $? -eq 1 ] && ok "拦住：RV 标注缺稳定 ID（入不了台账）" || bad "无 ID 标注被放行"

  mk; printf '\n【RV-甲99|甲|阻断|采纳】一\n【RV-甲99|甲|阻断|采纳】二\n' >> "$T/d/a.md"; rv   # rv-fixture
  [ $? -eq 1 ] && ok "拦住：RV 稳定 ID 重号（台账以 ID 为键）" || bad "重号 ID 被放行"

  mk; printf '| # | 项 | owner |\n|---|---|---|\n| 1 | x【RV|甲|阻断|采纳】并进单元格 | SP1 |\n' >> "$T/d/a.md"; rv
  [ $? -eq 1 ] && ok "拦住：RV 标注落在表格行内（| 与列分隔符对撞）" || bad "表内 RV 标注被放行"

  mk; printf '\n【RV-戊01|戊|严重|待定】字段乱写\n' >> "$T/d/a.md"; rv
  [ $? -eq 2 ] && ok "拦住：RV 字段不在受控枚举内（判闸坏，非通过）" || bad "非法字段被放行"

  mk; printf '\n【RV-乙01|乙|非阻断|驳回】就是不改\n' >> "$T/d/a.md"; rv --final   # rv-fixture
  [ $? -eq 1 ] && ok "收官：【驳回】未带理由必拦（驳回须可复核）" || bad "无理由的驳回被放行"

  mk; printf '\n【RV-乙01|乙|非阻断|驳回】理由：与 §5.2 冲突\n' >> "$T/d/a.md"; rv --final   # rv-fixture
  [ $? -eq 1 ] && ok "收官：标注纵已定性，仍须移出正文（体例）" || bad "收官后正文残留标注被放行"

  # 🔴 甲域 RV-甲14 的反测：光查「正文零标注」拦不住【直接删掉一条而不写沿革】。
  L="$T/lg.txt"; printf 'RV-甲01\t甲\t阻断\t采纳\tx.md\t某条\n' > "$L"
  mk; DOCS="$T/d/a.md" RV_SCOPE="$T/d/a.md" HIST="$T/d/h.md" LEDGER="$L" rvcheck --final >/dev/null 2>&1
  [ $? -eq 1 ] && ok "🔴 拦住：台账里的 RV-甲01 在正文与沿革件中都追不到（标注消失）" \
    || bad "标注被直接删掉却放行了 —— 「收官≠清空」仍只是句话"

  mk; printf '沿革：RV-甲01 已采纳，处置见下。\n' >> "$T/d/h.md"
  DOCS="$T/d/a.md" RV_SCOPE="$T/d/a.md" HIST="$T/d/h.md" LEDGER="$L" rvcheck --final >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：标注已落沿革件时放行" || bad "落了沿革仍被拦"

  mk; : > "$T/lg0.txt"
  DOCS="$T/d/a.md" RV_SCOPE="$T/d/a.md" HIST="$T/d/h.md" LEDGER="$T/lg0.txt" rvcheck --final >/dev/null 2>&1
  [ $? -eq 2 ] && ok "拦住：台账为空判闸坏（无可比对象不得当免检）" || bad "空台账被当免检"

  mk; LEDGER="$L" rv --final
  [ $? -eq 1 ] && ok "收官：台账未追平时不得放行" || bad "台账未追平却放行"

  # 🔴 钉住上面那条注释说的 bug:check() 若再用 n 当局部计数,本项即失守。
  #    判据是「跑完这么多项之后,通过计数必须 >= 已跑项数」——覆盖会把它压成个位数。
  # 🔴 门槛不写死具体项数：加反测时改一处漏一处，门槛与实际产出两处不同源
  #   —— 那正是本仓「计数型表述不写死数量」在闸自身上的形态（本闸已因此失守一次）。
  #   判据改为「远大于 check() 可能返回的命中数（个位数）」，只用于探测【被覆盖】。
  [ "$n" -ge 15 ] && ok "闸自身：通过计数未被 check()/rvcheck() 的局部变量覆盖" \
    || bad "通过计数被覆盖（现为 $n；被覆盖时会压成个位数）—— 反测数字不可信,闸就没有牙"

  rm -rf "$T"
  [ $fail -eq 0 ] && echo "--- 反测 $n 项全过（闸有牙）" || echo "--- 反测未过（闸没牙，不得依赖它的 ✓）"
  return $fail
}

# ── RV 标注检查 ──
# 🔴 甲域 sol R2 轮3：`--rv/--final` 的 DOCS 仅含三件正文，**完全看不见本脚本头注** ——
#   于是**闸自身仍有未决标注时，它照样宣称收官**。这是「闸看不见自己」的一种，
#   与本仓已归纳的「闸没起作用但打印 ✓」同族。
#   ⇒ RV 扫描面 = 三件正文 ＋ **全部闸脚本**（标注可能标在任何被评审的件上）。
# ⚠️ 标注住哪就扫哪：RV-甲41 住在 specs/evidence/collect.sh，RV 也会落在工具件上。
#   🔴 夹具排除改用显式标记 `rv-fixture` —— 此前按「行内含 printf」猜，
#     那是**按名字判不按效果判**：夹具之所以是夹具，在于它被标了，不在于它含哪个词。
RV_SCOPE=${RV_SCOPE:-"$DOCS $(ls scripts/*.sh specs/evidence/*.sh specs/tools/*.sh specs/tools/*.py 2>/dev/null | tr '\n' ' ')"}
RV_RE='【RV[^|]*\|[^|]*\|[^|]*\|[^】]*】'

rvcheck() { # rvcheck [--final]
  _final=${1:-}
  _bad=0; _open=0; _tot=0
  for f in $RV_SCOPE; do
    [ -f "$f" ] || { echo "✗ 闸依赖缺失：$f 不存在"; return 2; }
    # ⚠️ 闸脚本里有【模板】与【反测夹具】形态的 RV 串，它们不是真标注：
    #   模板：`【RV|<域>|…】`（含尖括号占位）· 正则：含 `[^|]` 等元字符
    #   夹具：反测里构造的 `【RV|丁|严重|待定】` 等（在 printf/ok/bad 行内）
    #   ⇒ 排除之。🔴 但**不排除头注里的真标注** —— 那正是本次要看见的。
    # ⚠️ 先取【整行】再筛，最后才抽 RV 串 —— `grep -oE` 只输出匹配片段，
    #   在片段上做上下文过滤（如排除 printf 行）**过滤器看不见它要过滤的东西**。本闸首版即如此。
    # ⚠️ 两步：先按【整行】排除模板/正则/反测夹具（上下文过滤只能在整行上做），
    #   再从**筛剩的行**里抽出全部 RV 串。
    #   🔴 首版在 `grep -oE` 的输出（只有片段）上做上下文过滤 —— 过滤器看不见它要过滤的东西。
    _keep=$(grep -nE "$RV_RE" "$f" 2>/dev/null \
            | grep -v '<域>' | grep -v '\[\^' | grep -v '(甲|乙|丙|执笔)' \
            | grep -v '|丁|' | grep -v 'printf' | grep -v 'rv-fixture' | grep -v 'RV_RE=')
    _hits=$(printf '%s\n' "$_keep" | grep -oE "$RV_RE")
    [ -z "$_hits" ] && continue
    _c=$(printf '%s\n' "$_hits" | wc -l | tr -d ' '); _tot=$((_tot+_c))
    # 字段合法性：域/严重度/状态三项必须在受控枚举内，否则判闸坏（无主的标注不得混入）
    _ill=$(printf '%s\n' "$_hits" | grep -vE '【RV[^|]*\|(甲|乙|丙|执笔)\|(阻断|非阻断)\|(未决|采纳|驳回|挂账)】' || true)

    # 🔴 RV 标注自身以 | 分隔，落进 markdown 表格行即与列分隔符对撞：
    #   表被劈成两块（下半缺分隔行）、标注被切碎（域字段带上残字）、
    #   且它会占掉某一列的位置 —— 附录 E 的 owner_sp 就这样被顶掉过一次。
    #   三种后果各由不同的闸报出，谁都不指向真因，故在此直接判死。
    # 🔴 处置列不得冒充状态列（甲域 sol R2 轮4 · RV-甲42）：
    #   RV-甲01 第 4 列写「采纳」，处置文本里却还嵌着「【挂账】…时点=R2 轮3 前」；
    #   RV-甲06 已有绝对日期，同一行仍留着已到期的「时点=R2 轮3 前」。
    #   原闸只看第 4 列、只查「有没有出现一个绝对日期」⇒ 两条都全绿。
    #   ⚠️ **状态只能由第 4 列表达**；处置文本里再出现状态记号即两处可互相矛盾，
    #     而读的人不知道该信哪一个。
    # 🔴 实现由【逐行 shell 循环】改为【单次解析】（甲域 sol R2 轮5 · RV-甲46）：
    #   原实现每行起 printf|grep|sed 四个进程，七千行 ⇒ 两万余次 fork，
    #   真实规模上 --rv 跑 47–60s 以上，门方 timeout 60 直接未完成。
    #   ⚠️ **判据没变，变的是实现** —— selftest 夹具只有几行，覆盖不到规模，
    #     故时限反测放在 specs/tools/rv-state-check.py 里（合成 2 万行断言 <5s）。
    python3 specs/tools/rv-state-check.py "$f" || return 1

    # 🔴 标注须带稳定 ID：台账以 ID 为键，无 ID 的标注【进不了账】，
    #   于是「任一标注不得消失」对它恒不成立 —— 少了也没人知道。
    _noid=$(grep -nE '【RV\|(甲|乙|丙|执笔)\|' "$f" 2>/dev/null | grep -v 'printf' | grep -v 'rv-fixture' || true)
    if [ -n "$_noid" ]; then
      echo "✗ $f：RV 标注缺稳定 ID（须写作【RV-<域><序号>|…】）—— 无 ID 则入不了台账，防丢判据对它恒不成立"
      printf '%s\n' "$_noid" | cut -c1-120
      return 1
    fi

    # 🔴 代码件里的 RV 标注必须独占【注释行】（甲域 sol R2 轮6 · RV-甲47）：
    #   执笔方的批量落处置脚本已四次把标注/处置文本写进**代码行**：
    #   两次续行、一次 printf 夹具、一次带反引号拼进 shell 命令 —— 最后那次
    #   触发命令替换，实跑连出 Permission denied / No such file / command not found，
    #   而该闸的 selftest **仍报 8/8 全过**。
    #   ⚠️ 根因不在某一次手滑，在于【标注可以合法地出现在代码件里】而没有位置约束。
    #   ⇒ .sh / .py 内的 RV 标注只允许出现在注释行（# 开头，允许前导空白）。
    case "$f" in
      *.sh|*.py)
        # ⚠️ 排除闸自身的【报错文案】：`echo "…【RV-<域><序号>|…】…"` 是提示信息，
        #   不是标注。判据若不排除它，闸会判自己 —— 与「判到了指出问题的那句话」同形，
        #   本仓第四次（前三次：⑫ 判引述时点、⑬ 判标注内旧断言、⑳ 判说明 path-ok 的那句）。
        # ⚠️ 只判【格式完整】的标注，不判「出现了 RV- 三个字」：
        #   正则源码（`'【RV-[^|]+\|'`）与报错文案（`【RV-<域><序号>|…】`）都不是标注，
        #   逐条加排除是打补丁，收窄判据才是修法 —— 否则闸会一直判到自己身上。
        _codeline=$(grep -nE '【RV-[^|]+\|(甲|乙|丙|执笔)\|(阻断|非阻断)\|(未决|采纳|驳回|挂账)】' "$f" 2>/dev/null \
                    | grep -v 'rv-fixture' | grep -vE '^[0-9]+:[[:space:]]*#' || true)
        if [ -n "$_codeline" ]; then
          echo "✗ $f：RV 标注出现在【非注释行】—— 代码件里的标注只允许独占注释行"
          printf '%s\n' "$_codeline" | cut -c1-120
          echo "  🔴 拼进命令行的标注会被 shell 当代码执行（反引号触发命令替换），"
          echo "     而该件的 selftest 可能照样报全过 —— 甲域 RV-甲47 即此。"
          return 1
        fi ;;
    esac

    # 🔴 稳定 ID 须唯一：台账以 ID 为键，重号即两条标注共用一行，
    #   「任一标注不得消失」这条判据会在重号处静默失效（少的那条查无对证）。
    # ⚠️ 必须先按【行】滤掉夹具再抽片段：先 -o 抽片段的话，
    #   grep -v 'printf' 只看得见片段本身，看不见它所在的那行 —— 本闸已因此失守两次。
    # ⚠️ 同上：只抽【格式完整】的标注 ID，正则源码与报错文案不算标注。
    _dup=$(grep -E '【RV-[^|]+\|(甲|乙|丙|执笔)\|(阻断|非阻断)\|(未决|采纳|驳回|挂账)】' "$f" 2>/dev/null \
           | grep -v 'rv-fixture' \
           | grep -ohE '【RV-[^|]+\|' | sort | uniq -d || true)
    if [ -n "$_dup" ]; then
      echo "✗ $f：RV 稳定 ID 重号 —— 台账以 ID 为键，重号处的防丢判据会静默失效"
      printf '%s\n' "$_dup"
      return 1
    fi

    _intbl=$(grep -nE '^\|.*【RV[^|]*\|' "$f" 2>/dev/null | grep -v 'printf' | grep -v 'rv-fixture' || true)
    if [ -n "$_intbl" ]; then
      echo "✗ $f：RV 标注落在表格行内 —— 标注的 | 与表格列分隔符对撞，须移出表块另起一行"
      printf '%s\n' "$_intbl" | cut -c1-120
      return 1
    fi
    if [ -n "$_ill" ]; then
      echo "✗ $f：RV 标注字段不合法（域/严重度/状态须为受控枚举）"
      printf '%s\n' "$_ill" | head -5 | sed 's/^/    /'
      _bad=2
    fi
    _u=$(printf '%s\n' "$_hits" | grep -cE '\|未决】' || true)
    _open=$((_open+_u))
    echo "  · $f：RV 标注 $_c 条（未决 $_u）"
  done
  [ "$_bad" -eq 2 ] && return 2
  if [ "$_final" = "--final" ]; then
    if [ "$_open" -gt 0 ]; then
      echo "✗ 收官检查：仍有 $_open 条【未决】标注 —— 收官须逐条落到 采纳 / 驳回(带理由) / 挂账(带责任人+时点)"
      echo "  🔴 标注不得就这么消失，否则「收官」是假装闭合"
      return 1
    fi
    for f in $RV_SCOPE; do
      _r=$(grep -E '【RV[^|]*\|[^|]*\|[^|]*\|驳回】' "$f" 2>/dev/null \
           | grep -v 'printf' | grep -v 'rv-fixture' | grep -v '<域>' \
           | grep -oE '【RV[^|]*\|[^|]*\|[^|]*\|驳回】[^【]{0,20}' | grep -vE '驳回】\s*理由' || true)
      if [ -n "$_r" ]; then
        echo "✗ $f：有【驳回】标注未带理由 —— 驳回必须可复核"; printf '%s\n' "$_r" | head -3 | sed 's/^/    /'; return 1
      fi
    done
    [ "$_tot" -gt 0 ] && { echo "✗ 收官后正文仍有 $_tot 条 RV 标注 —— 须全部移入 $HIST"; return 1; }
    # 🔴 甲域 RV-甲14：光查「正文零标注」拦不住【直接删掉一条而不写沿革】。
    #   须以台账为可比对象，逐条确认它去了哪里。
    # 🔴 甲域 sol：`--final` 只校验驳回理由，**不校验挂账须含责任人＋时点** ——
    #   现有挂账多数无时点，且「R2 轮3前」这类时点**已到期**，仍会通过。
    #   ⇒ 挂账须同时含【责任人】与【时点】，且时点须为具体日期（YYYY-MM-DD）而非「某轮之前」。
    for f in $RV_SCOPE; do
      _h=$(grep -E '【RV[^|]*\|[^|]*\|[^|]*\|挂账】' "$f" 2>/dev/null | grep -v 'printf' | grep -v 'rv-fixture' | grep -v '<域>' \
           | grep -oE '【RV[^|]*\|[^|]*\|[^|]*\|挂账】[^【]{0,400}' | grep -vE '责任人' || true)
      if [ -n "$_h" ]; then
        echo "✗ $f：有【挂账】标注未写责任人"; printf '%s\n' "$_h" | head -3 | cut -c1-90 | sed 's/^/    /'; return 1
      fi
      _h2=$(grep -E '【RV[^|]*\|[^|]*\|[^|]*\|挂账】' "$f" 2>/dev/null | grep -v 'printf' | grep -v 'rv-fixture' | grep -v '<域>' \
            | grep -oE '【RV[^|]*\|[^|]*\|[^|]*\|挂账】[^【]{0,400}' | grep -vE '20[0-9]{2}-[0-9]{2}-[0-9]{2}' || true)
      if [ -n "$_h2" ]; then
        echo "✗ $f：有【挂账】标注未写【具体日期】时点（须 YYYY-MM-DD，「某轮之前」这类会到期后仍通过）"
        printf '%s\n' "$_h2" | head -3 | cut -c1-90 | sed 's/^/    /'; return 1
      fi
    done

    _lg=${LEDGER:-specs/gates/rv-ledger.txt}
    if [ ! -s "$_lg" ]; then
      echo "✗ RV 台账 $_lg 不存在或为空 —— 无可比对象即无法判「标注是否消失」，闸坏了"; return 2
    fi
    _lost=0
    while IFS="$(printf '\t')" read -r id rest; do
      case "$id" in ''|'#'*) continue;; esac
      if ! grep -qF "$id" $DOCS "$HIST" 2>/dev/null; then
        [ "$_lost" -eq 0 ] && echo "✗ 收官：有标注【消失】—— 既不在正文也不在沿革件："
        echo "    $id"; _lost=$((_lost+1))
      fi
    done < "$_lg"
    if [ "$_lost" -gt 0 ]; then
      echo "  🔴 收官 ≠ 清空：$_lost 条标注被删而未落沿革。每条须落到 采纳／驳回(带理由)／挂账(带责任人) 之一。"
      return 1
    fi
    # 🔴 甲域 sol 最终 verdict（锚 53f4033）：收官脚本只摘走多行 RV 的【首行】，
  #   RV-丙11 的续行 5 行仍留在 SpecDoc §0 前，而 --final 与 ⑯ **均误报通过** ——
  #   ⑯ 比的是台账摘要（首行前 18 字），首行搬走了它就满足；
  #   --final 数的是「【RV…】」标记，续行没有该标记，于是两道闸都看不见它。
  #   ⚠️ **两道闸的判据都只覆盖标注的第一行** —— 一条标注不是一行，是一段。
  #   ⇒ 按门方留在续行上的【自述标记】判：`style-ok: 本行为…RV 标注`
  #   ⚠️ 作用域＝三件正文（$DOCS），不含闸脚本：续行残留是【正文】的事，
  #     扫脚本只会判到本判据自己的注释与正则源码 —— 本仓同形第五次。
  #     是评审方自己声明「本行是 RV 标注」，收官后正文出现它即为续行残留。
  _resid=$(grep -nE 'style-ok:[^|]*本行为[^|]*RV 标注|请执笔判|处理建议同' $DOCS 2>/dev/null \
           | grep -v 'rv-fixture' || true)
  if [ -n "$_resid" ]; then
    echo "✗ 收官后正文仍有【多行 RV 的续行】残留 —— 一条标注是一段，不是一行"
    printf '%s\n' "$_resid" | cut -c1-140
    echo "  🔴 ⑯ 只比首行摘要、--final 只数【RV…】标记，两者都看不见续行。"
    echo "  ⇒ 续行须【逐字】随其首行一并移入沿革件。"
    return 1
  fi
  echo "  ✓ 收官：正文零 RV 标注，且台账内每条 ID 均可在正文或沿革件中追到"
    echo "  ⚠️ 本闸判「ID 还在不在」，判不了「沿革里写得对不对」—— 后者归门方。"
  fi
  return 0
}

case "${1:-}" in
  --selftest) selftest; exit $? ;;
  --rv)       rvcheck; exit $? ;;
  --final)    check || exit $?; rvcheck --final; exit $? ;;
esac
check; exit $?
