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
# 【RV|甲|阻断|未决】`--final` 目前不能实现上文“任一标注不得消失”。它只检查【当前】三件中未决数、驳回理由和最终零标注；提交者若直接删除一条 RV 且不写沿革，脚本没有前一轮 RV ledger/稳定 ID 可比，仍会 rc=0。须为每条 RV 分配稳定 ID，并在独立 reviewer-owned 清单或基线 manifest 中记录；final 对拍“前轮全集 = 采纳 ∪ 驳回(理由) ∪ 挂账(owner+时点) = 沿革落账”，否则收官防丢只是文字纪律。
set -u

DOCS=${DOCS:-'docs/dingwei-platform-specdoc.md docs/dingwei-architecture-design.md docs/dingwei-prototype.html'}
# 沿革记录件是【专门存放】过程痕迹的地方,本闸不扫它。
HIST=${HIST:-'docs/dingwei-revision-history.md'}

# 痕迹指纹 —— 只取指向【特定评审轮次 / 版本 / 评审方】的表述。
#   刻意【不】收「复核」「评审」等词:它们在规范里是正当术语
#   （如「不可回滚迁移的复核要求」），收进来就是误伤,会逼人绕过闸。
PAT_ROLE='甲域|乙域|丙域|本席'
PAT_VER='v[0-9]+\.[0-9]+[[:space:]]*(更正|补|压缩|删去|收窄|改写|新增|撤回|再次)'
PAT_HIST='打脸|撤回为|原句已撤回|此前写|本段此前|原文写成|原表|已是第[二三四五六七八九]次|第[二三四五六七八九]次'
PAT_ALL="$PAT_ROLE|$PAT_VER|$PAT_HIST"

scan() { # scan <file> -> 命中行
  # 🔴 RV 标注行不计入痕迹：标注【本来就要】指名评审域与轮次,那是它的用途。
  #   两个机制若不互相认识就会打架 —— 本闸立起来后第一次真用 RV 就撞上了。
  #   ⚠️ 这不是给 RV 开后门:RV 有自己的判据(rvcheck:字段受控枚举、收官须清零)。
  #     排除的是【重复管辖】,不是【不管】。
  grep -nE "$PAT_ALL" "$1" 2>/dev/null | grep -v 'style-ok:' | grep -v '【RV|'
}

check() {
  # 🔴 局部计数一律加 _ 前缀:sh 无函数局部变量,叫 n 会覆盖 selftest 的通过计数,
  #    于是「反测 N 项全过」印出来的是最后一次命中数 —— 一个看着合理、其实是别的东西的数字。
  #    本闸自己先栽过这一次,注在此处防复发。
  rc=0; _total=0
  for f in $DOCS; do
    [ -f "$f" ] || { echo "✗ 闸依赖缺失：$f 不存在 —— 闸坏了，不是免检"; return 2; }
    hits=$(scan "$f")
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
  done
  if [ "$rc" -ne 0 ]; then
    echo "  🔴 剥离【不是删】：痕迹里若含现行规范内容（能力边界声明 · 口径收窄的结果表等），"
    echo "     内容留正文、沿革移入 $HIST。逐条判断，不得机械替换。"
    echo "  ⚠️ 确有正当理由须留在正文者，行内加 style-ok: <理由> 标记；理由为空视同未标。"
    return 1
  fi
  # 沿革件必须存在且非空 —— 否则「剥离到沿革件」只是把它删了。
  if [ ! -s "$HIST" ]; then
    echo "✗ 沿革记录件 $HIST 不存在或为空 —— 剥离必须有去处,不得只删不存"; return 2
  fi
  echo "  ✓ 体例合规（正文 $_total 处痕迹指纹；沿革件在 $HIST）"
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

  mk; printf '# 正文\n\n被甲域实算打脸。\n' >> "$T/d/a.md"; run
  [ $? -eq 1 ] && ok "拦住：沿革叙述（打脸）" || bad "沿革叙述未被拦"
  mk; printf '# 正文\n\n已是第三次犯同一错。\n' >> "$T/d/a.md"; run
  [ $? -eq 1 ] && ok "拦住：第 N 次自述" || bad "第 N 次自述未被拦"

  # 🔴 不误伤:规范里「复核」「评审」是正当术语,收进模式就会逼人绕过闸
  mk; printf '# 正文\n\n不可回滚迁移的复核要求见 §5.2。架构评审为 L3。\n' >> "$T/d/a.md"; run
  [ $? -eq 0 ] && ok "不误伤：规范术语「复核 / 评审」不算痕迹" || bad "误伤了规范术语"

  # 例外须带理由
  mk; printf '# 正文\n\n本席保留此句 style-ok: 该句为能力边界声明,属规范内容\n' >> "$T/d/a.md"; run
  [ $? -eq 0 ] && ok "不误伤：带 style-ok 理由的例外放行" || bad "带理由的例外被误拦"

  # 🔴 剥离必须有去处:沿革件空 ⇒ 判闸坏,防「只删不存」
  mk; : > "$T/d/h.md"; run
  [ $? -eq 2 ] && ok "拦住：沿革件为空（只删不存）判闸坏" || bad "沿革件为空却放行"

  # 依赖缺失是闸坏,不是免检
  mk; rm -f "$T/d/a.md"; run
  [ $? -eq 2 ] && ok "拦住：正文件缺失判闸坏（非免检）" || bad "正文件缺失被当免检"

  # ── RV 标注反测 ──
  rv(){ DOCS="$T/d/a.md" HIST="$T/d/h.md" rvcheck "${1:-}" >/dev/null 2>&1; }

  # 🔴 两个机制不得互相打架:RV 标注按用途必然含评审域名,体例闸不得因此判它是痕迹。
  mk; printf '\n【RV|丙|阻断|未决】丙域指出某处有问题\n' >> "$T/d/a.md"
  DOCS="$T/d/a.md" HIST="$T/d/h.md" check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤:RV 标注含评审域名不算过程痕迹(两闸不打架)" \
    || bad "体例闸把 RV 标注当成痕迹 —— 两个机制互相打架"

  mk; printf '\n【RV|甲|阻断|未决】某条意见\n' >> "$T/d/a.md"; rv
  [ $? -eq 0 ] && ok "轮内：合法 RV 标注放行（不阻断评审进行）" || bad "轮内合法标注被拦"

  mk; printf '\n【RV|甲|阻断|未决】某条意见\n' >> "$T/d/a.md"; rv --final
  [ $? -eq 1 ] && ok "收官：仍有【未决】标注必拦" || bad "收官放行了未决标注"

  mk; printf '\n【RV|丁|严重|待定】字段乱写\n' >> "$T/d/a.md"; rv
  [ $? -eq 2 ] && ok "拦住：RV 字段不在受控枚举内（判闸坏，非通过）" || bad "非法字段被放行"

  mk; printf '\n【RV|乙|非阻断|驳回】就是不改\n' >> "$T/d/a.md"; rv --final
  [ $? -eq 1 ] && ok "收官：【驳回】未带理由必拦（驳回须可复核）" || bad "无理由的驳回被放行"

  mk; printf '\n【RV|乙|非阻断|驳回】理由：与 §5.2 冲突\n' >> "$T/d/a.md"; rv --final
  [ $? -eq 1 ] && ok "收官：标注纵已定性，仍须移出正文（体例）" || bad "收官后正文残留标注被放行"

  mk; rv --final
  [ $? -eq 0 ] && ok "收官：正文零标注放行" || bad "干净正文收官被误拦"

  # 🔴 钉住上面那条注释说的 bug:check() 若再用 n 当局部计数,本项即失守。
  #    判据是「跑完这么多项之后,通过计数必须 >= 已跑项数」——覆盖会把它压成个位数。
  [ "$n" -ge 19 ] && ok "闸自身：通过计数未被 check()/rvcheck() 的局部变量覆盖" \
    || bad "通过计数被覆盖（现为 $n，应 >=19）—— 反测数字不可信,闸就没有牙"

  rm -rf "$T"
  [ $fail -eq 0 ] && echo "--- 反测 $n 项全过（闸有牙）" || echo "--- 反测未过（闸没牙，不得依赖它的 ✓）"
  return $fail
}

# ── RV 标注检查 ──
RV_RE='【RV\|[^|]*\|[^|]*\|[^】]*】'

rvcheck() { # rvcheck [--final]
  _final=${1:-}
  _bad=0; _open=0; _tot=0
  for f in $DOCS; do
    [ -f "$f" ] || { echo "✗ 闸依赖缺失：$f 不存在"; return 2; }
    _hits=$(grep -noE "$RV_RE" "$f" 2>/dev/null)
    [ -z "$_hits" ] && continue
    _c=$(printf '%s\n' "$_hits" | wc -l | tr -d ' '); _tot=$((_tot+_c))
    # 字段合法性：域/严重度/状态三项必须在受控枚举内，否则判闸坏（无主的标注不得混入）
    _ill=$(printf '%s\n' "$_hits" | grep -vE '【RV\|(甲|乙|丙|执笔)\|(阻断|非阻断)\|(未决|采纳|驳回|挂账)】' || true)
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
    for f in $DOCS; do
      _r=$(grep -oE '【RV\|[^|]*\|[^|]*\|驳回】[^【]{0,20}' "$f" 2>/dev/null | grep -vE '驳回】\s*理由' || true)
      if [ -n "$_r" ]; then
        echo "✗ $f：有【驳回】标注未带理由 —— 驳回必须可复核"; printf '%s\n' "$_r" | head -3 | sed 's/^/    /'; return 1
      fi
    done
    [ "$_tot" -gt 0 ] && { echo "✗ 收官后正文仍有 $_tot 条 RV 标注 —— 须全部移入 $HIST"; return 1; }
    echo "  ✓ 收官：正文零 RV 标注"
  fi
  return 0
}

case "${1:-}" in
  --selftest) selftest; exit $? ;;
  --rv)       rvcheck; exit $? ;;
  --final)    check || exit $?; rvcheck --final; exit $? ;;
esac
check; exit $?
