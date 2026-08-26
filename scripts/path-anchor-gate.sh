#!/bin/sh
# 路径锚解析闸 —— 正文里反引号包起来的仓内路径必须解析得到。
#
# 🔴 立此闸的直接原因（甲域 sol，R2 轮5 · RV-甲44）：
#   E.3 #36b 的 reviewer owner 锚被执笔方写成 `specs/checklists/checklist.md`，
#   **仓内没有这个路径**（真实路径是 specs/platform/checklist.md）。
#   于是「人工判据的落点」指向一个空文件，而 ①–⑲ 全绿。
#   ⚠️ 这一处是执笔方**凭印象写路径**造成的 —— 同一形态还会出现在
#     「见 xxx.md」「归 scripts/yyy.sh」这类交代责任落点的句子里，
#     它们恰恰是最需要能被打开的引用。
#
# ⚠️ 判据边界：
#   · 只判**仓内相对路径**（docs/ specs/ scripts/ internal/ 开头），不判 URL、命令、包名；
#   · 目录也算解析得到（git ls-files 只列文件，故须单独判目录）；
#   · 含 * ? 的当成通配示例，不判；
#   · **RV 标注段整段跳过** —— 评审方引述错路径是常态（sol 的 RV-甲44 正文里就引着那个错路径），
#     判它等于把「指出问题的那句话」当成问题本身。
set -u

DOCS=${DOCS:-'docs/dingwei-platform-specdoc.md docs/dingwei-architecture-design.md docs/dingwei-prototype.html'}
EXLIST=${EXLIST:-specs/gates/path-anchor-exceptions.txt}
BASELINE=${BASELINE:-specs/gates/path-anchor-suppression-baseline.txt}

# 🔴 乙域 terra 全闸契约（2026-08-26）：
#   「`style-ok` / `status-ok` / `path-ok` 等抑制**均属特权例外**；每个例外须精确绑定
#     被豁免对象、理由、reviewer approval，**不能由执笔自由文本生效**。」
#   ⇒ 本闸原先 `grep -v 'path-ok:'` **整行跳过**，只校「理由非空」——
#     那是**长度**，不是批准；且整行跳过意味着同一行上**其它**路径锚也一起被抹掉。
#   ⇒ 改为与已撤销断言闸**同一套**受控例外（公共库 specs/gates/lib/exceptions.sh）。
_LIB=${EXC_LIB:-specs/gates/lib/exceptions.sh}
[ -f "$_LIB" ] || { echo "✗ 闸依赖缺失：$_LIB 不存在 —— 闸坏了，不是免检"; exit 2; }
. "$_LIB"

check() {
  _bad=0; _n=0; _exn=0; _debt=0; _unkn=0; _unk=''; _inert=0; _seen=0
  for f in $DOCS; do
    [ -f "$f" ] || { echo "✗ 闸依赖缺失：$f 不存在"; return 2; }
    _seen=$((_seen+1))
    # 逐行取路径锚（先剥 RV 标注段）—— 🔴 **保留行号**，因为受控例外要逐处绑定。
    #   旧实现先 `grep -v path-ok:` 再抽路径，等于用一个自由文本标记把整行从视野里抹掉。
    _hits=$(sed 's/【RV[^】]*】[^\n]*//g' "$f" 2>/dev/null \
            | grep -noE '`(docs|specs|scripts|internal)/[A-Za-z0-9_.@/-]+`' | tr -d '`')
    for h in $_hits; do
      _ln=${h%%:*}; _p=${h#*:}
      case "$_p" in *'*'*|*'?'*) continue;; esac
      _n=$((_n+1))
      [ -e "$_p" ] && continue
      exc_decide "$EXLIST" "$_p" "$f" "$_ln"; _erc=$?
      [ "$_erc" -eq 0 ] && { _exn=$((_exn+1)); continue; }
      [ "$_erc" -eq 2 ] && { _unk="$_unk $f:$_ln($_p)"; _unkn=$((_unkn+1)); continue; }
      if exc_baseline_hit "$BASELINE" "$_p" "$f" "$_ln"; then _debt=$((_debt+1)); continue; fi
      [ "$_bad" -eq 0 ] && echo "✗ 正文引用的仓内路径解析不到（人工判据的落点指向空文件）："
      echo "    $f:$_ln  →  $_p"
      # 🔴 行内 path-ok 已失效 —— 说清楚，否则读的人会以为闸坏了
      if sed -n "${_ln}p" "$f" | grep -q 'path-ok:'; then
        echo "       ⚠️ 本行有行内 \`path-ok:\` 标记，但它对本闸**已不再构成豁免**（乙域全闸契约）："
        echo "          自由文本理由无人核 ⇒ 须到 $EXLIST 登记受控例外并取门方 approval_ref。"
        _inert=$((_inert+1))
      fi
      _bad=$((_bad+1))
    done
  done
  if [ "$_unkn" -gt 0 ]; then
    echo "✗ UNKNOWN（exit 2）：$_unkn 处受控例外的【批准】无从核验 —— 授权根不可得"
    for u in $_unk; do echo "    $u"; done
    echo "  ⇒ 见 specs/gates/approvals/TRUST-ROOT.md：当前无 ACTIVE 后端（门方待裁）。"
    return 2
  fi
  if [ "$_bad" -gt 0 ]; then
    echo "  🔴 **交代责任落点的引用最需要能被打开** —— 写错路径时闸不报，读的人才发现是空的。"
    echo "  ⇒ 用 git ls-files 核对后再写，不要凭印象；确属「断言该路径不存在」者，"
    echo "     到 $EXLIST 登记（六列：claim_id ＋ 路径 ＋ 件 ＋ 行指纹 ＋ 理由码 ＋ approval_ref）。"
    [ "$_inert" -gt 0 ] && echo "  ⚠️ 其中 $_inert 处仍在用行内 path-ok —— 那条通道已关闭。"
    return 1
  fi
  echo "  ✓ 正文仓内路径锚全部解析得到"
  # 覆盖见证（GATE-CONTRACT 一之二·第 4 条）
  echo "     受检范围（预期集合）：$_seen 件 / $_n 处路径锚（口径＝已剥 RV 标注段；含通配符者不判）"
  echo "     实检集合：$_n 处逐处实查（test -e）"
  echo "     未知集合：$_unkn"
  if [ "$_exn" -gt 0 ]; then
    echo "  ⚠️ 其中 $_exn 处经【受控例外】放行（$EXLIST，approval_ref 已解析核验）"
    echo "     🔴 闸只验了批准记录存在且四元组对得上；**这条豁免该不该给，仍归门方**。"
  fi
  if [ "$_debt" -gt 0 ]; then
    echo "  ⚠️ PASS WITH REGISTERED DEBT：另有 $_debt 处命中【冻结基线】放行（$BASELINE）"
    echo "     🔴 基线**不是批准**；基线数 $(exc_count "$BASELINE") / 当前 $_debt / 增减 $((_debt - $(exc_count "$BASELINE")))"
    echo "     基线按行指纹绑定、不可新增 —— 该行一改即掉出基线，须走门方批准。"
  fi
  echo "  ⚠️ 本闸判【路径存在】，判不了【引对了没有】—— 指向一个存在但不相干的文件它看不出来。"
  return 0
}

selftest() {
  T=$(mktemp -d) || return 2
  n=0; fail=0
  ok(){ n=$((n+1)); echo "  ✓ $1"; }; bad(){ echo "  ✗ 反测未过：$1"; fail=1; }
  # 🔴 甲域 sol R2 轮6（RV-甲47）：本反测曾在**夹具构造命令连续失败**的情况下
  #   仍打印「8 项全过」并 rc=0 —— 处置文本被拼进 printf 行，反引号触发命令替换，
  #   实跑连出 Permission denied / No such file / command not found，而通过计数毫无感知。
  #   ⚠️ **夹具没造出来，后面测的就不是它该测的东西** —— 通过计数只统计了断言，
  #     没统计断言所依赖的前置动作。
  #   ⇒ mkfx：夹具构造必须自己判成败；任何非预期 stderr 也算失败。
  FXERR="$T/fx.err"; : > "$FXERR"
  mkfx(){ # 用法：mkfx <目标文件> <内容> [probe]
    #   probe 模式只回退出码，不打印、不置 fail —— 用于「证明 mkfx 真会失败」那一条，
    #   否则屏幕上会先出现一行 ✗ 再出现一行 ✓，读的人分不清哪个是真的。
    #   ⚠️ **看不清的输出本身就是假通过的温床**（本闸就是这么混过去的）。
    if ! printf '%s' "$2" > "$1" 2>>"$FXERR"; then
      [ "${3:-}" = probe ] || { echo "  ✗ 反测未过：夹具构造失败（$1）"; fail=1; }
      return 1
    fi
    if [ ! -s "$1" ]; then
      [ "${3:-}" = probe ] || { echo "  ✗ 反测未过：夹具构造出空文件（$1）"; fail=1; }
      return 1
    fi
    return 0
  }
  cd "$T" || return 2
  mkdir -p docs specs/platform scripts internal/repo
  : > specs/platform/checklist.md; : > internal/repo/tenant.go

  mkfx docs/a.md '见 `specs/platform/checklist.md`。\n' || return 1
  DOCS=docs/a.md check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：存在的文件路径放行" || bad "存在的路径被拦"

  mkfx docs/a.md '见 `specs/checklists/checklist.md`。\n' || return 1
  DOCS=docs/a.md check >/dev/null 2>&1
  [ $? -eq 1 ] && ok "🔴 拦住：不存在的路径锚（正是甲域 RV-甲44）" || bad "空锚被放行"

  mkfx docs/a.md '收编进 `internal/repo`。\n' || return 1
  DOCS=docs/a.md check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：目录路径算解析得到（ls-files 只列文件）" || bad "目录被误判为空锚"

  mkfx docs/a.md '见 `specs/gates/*.txt` 各基线。\n' || return 1
  DOCS=docs/a.md check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：含通配符的示例路径不判" || bad "通配示例被误判"

  # 🔴 评审方在标注里【引述】错路径，不得被判
  mkfx docs/a.md '【RV-甲44|甲|阻断|采纳】owner 锚写成 `specs/checklists/checklist.md`，仓内不存在\n' || return 1   # rv-fixture
  DOCS=docs/a.md check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：RV 标注段内引述的错路径不判（否则等于判指出问题的那句话）" || bad "标注内引述被误判"

  # ===== 受控例外组（乙域 terra 全闸契约 2026-08-26）=====
  # 🔴 **改契约必须回头改为旧契约写的反测**：原两条测的是「path-ok 理由非空即放行 / 空则拦」，
  #   而该通道已整体关闭 —— 留着它们就等于把绕过面重新锁定成期望语义。
  REV=zzc-chatgpt56terrazhangzongchu-2642
  _OA=$EXC_APPROVALS; _OR=$EXC_REVIEWERS; _OT=$EXC_TRUSTROOT
  mkdir -p "$T/appr"; EXC_APPROVALS="$T/appr"; EXC_REVIEWERS="$T/rev.txt"; EXC_TRUSTROOT="$T/trust.md"
  printf '%s\n' "$REV" > "$T/rev.txt"
  _fpx(){ sed -n "$2p" "$1" | tr -d '[:space:]' | sha256sum | cut -c1-16; }

  # ① 🔴 真实违规 ＋ 行内编造抑制 ⇒ 必拒（terra 点名要的那一半）
  mkfx docs/a.md '| 🔴 `internal/view` 不存在 | 实测 | path-ok: 本行断言该路径不存在，是取证结果 |\n' || return 1
  DOCS=docs/a.md EXLIST="$T/none.txt" BASELINE="$T/none.txt" check >/dev/null 2>&1
  [ $? -eq 1 ] && ok "🔴 拦住：行内 path-ok 自由文本【已不再豁免】（自由文本理由无人核）" \
                || bad "行内 path-ok 仍能整行跳过 —— 通则只落到一条通道里就不是通则"

  # ② 经受控例外的合法对象 ⇒ 过（terra 点名要的另一半）
  _fx=$(_fpx docs/a.md 1)
  printf 'ACTIVE: reviewer-owned-file\n' > "$T/trust.md"
  printf '# OWNER: %s\n' "$REV" > "$T/appr/$REV.approvals"
  printf 'AP-P1\tPA-TEST-01\tdocs/a.md\t%s\tevidence-of-absence\t%s\t2026-08-26T00:00:00Z\n' "$_fx" "$REV" >> "$T/appr/$REV.approvals"
  printf 'PA-TEST-01\tinternal/view\tdocs/a.md\t%s\tevidence-of-absence\t%s#AP-P1\n' "$_fx" "$REV" > "$T/ex.txt"
  DOCS=docs/a.md EXLIST="$T/ex.txt" BASELINE="$T/none.txt" check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "经受控例外的合法对象放行（键齐 ＋ 批准可解析 ＋ 授权根 ACTIVE）" || bad "合规受控例外被误拦"

  # ③ 🔴 授权根无 ACTIVE 后端（＝仓内当前实况）⇒ exit 2，不折算为 PASS
  : > "$T/trust.md"
  DOCS=docs/a.md EXLIST="$T/ex.txt" BASELINE="$T/none.txt" check >/dev/null 2>&1
  [ $? -eq 2 ] && ok "🔴 授权根不可得 ⇒ UNKNOWN exit 2（不是 PASS，也不是 FAIL）" || bad "授权根不可得时闸给了结论"
  printf 'ACTIVE: reviewer-owned-file\n' > "$T/trust.md"

  # ④ 🔴 码选错 ⇒ 必拒（码不是摆设：后置条件不成立就不放行）
  #   ⚠️ 写明本通道的一处能力边界，不假装：`evidence-of-absence` 在本闸上
  #   **天然成立** —— 闸只在路径确实不存在时才走到判码这一步。
  #   ⇒ 在本通道，真正的判断量全在 **approval**，码只挡「选了个不相干的码」。
  mkfx docs/a.md '| 🔴 `internal/view` 不存在 | 实测 | path-ok: 本行断言该路径不存在，是取证结果 |\n' || return 1
  _fx=$(_fpx docs/a.md 1)
  printf 'AP-P2\tPA-TEST-02\tdocs/a.md\t%s\thistorical-narrative\t%s\t2026-08-26T00:00:00Z\n' "$_fx" "$REV" >> "$T/appr/$REV.approvals"
  printf 'PA-TEST-02\tinternal/view\tdocs/a.md\t%s\thistorical-narrative\t%s#AP-P2\n' "$_fx" "$REV" > "$T/ex2.txt"
  DOCS=docs/a.md EXLIST="$T/ex2.txt" BASELINE="$T/none.txt" check >/dev/null 2>&1
  [ $? -eq 1 ] && ok "🔴 拦住：码与行形态不符（本行无时点表述却报 historical-narrative）" \
                || bad "码选错仍放行 —— 码没有后置条件"

  # ⑤ 🔴 冻结基线：命中则亮 REGISTERED DEBT；指纹漂移即掉出基线
  mkfx docs/a.md '| 🔴 `internal/view` 不存在 | 实测 | path-ok: 本行断言该路径不存在，是取证结果 |\n' || return 1
  _fx=$(_fpx docs/a.md 1)
  printf 'PA-BL-01\tinternal/view\tdocs/a.md\t%s\tevidence-of-absence\t反测用基线\n' "$_fx" > "$T/bl.txt"
  _o=$(DOCS=docs/a.md EXLIST="$T/none.txt" BASELINE="$T/bl.txt" check 2>&1); _r=$?
  case "$_o" in
    *"REGISTERED DEBT"*) [ $_r -eq 0 ] && ok "🔴 冻结基线放行时亮明 PASS WITH REGISTERED DEBT（基线≠批准）" || bad "基线亮明但退出码不对";;
    *) bad "命中基线时未亮明 REGISTERED DEBT";;
  esac
  printf 'PA-BL-01\tinternal/view\tdocs/a.md\t0000000000000000\tevidence-of-absence\t指纹已漂移\n' > "$T/bl.txt"
  DOCS=docs/a.md EXLIST="$T/none.txt" BASELINE="$T/bl.txt" check >/dev/null 2>&1
  [ $? -eq 1 ] && ok "🔴 基线按行指纹绑定：该行一改即掉出基线（基线不可新增）" || bad "基线在指纹漂移后仍放行"
  EXC_APPROVALS=$_OA; EXC_REVIEWERS=$_OR; EXC_TRUSTROOT=$_OT

  mkfx docs/a.md '见 `https://example.com/x.md` 与 `go build ./...`。\n' || return 1
  DOCS=docs/a.md check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：URL 与命令不算仓内路径" || bad "非仓内路径被误判"

  # 🔴 甲域 RV-甲47 要求的那条：夹具构造失败必须让 selftest FAIL，而不是照样报全过
  if mkfx "$T/nodir/x.md" 'x' probe 2>/dev/null; then
    bad "夹具构造失败未被感知（写入不存在的目录竟报成功）"
  else
    ok "🔴 夹具构造失败 ⇒ selftest FAIL（甲域 RV-甲47：曾在连续 shell error 下仍报 8/8）"
  fi
  : > "$FXERR"   # probe 是预期失败，其 stderr 不算环境不干净
  if [ -s "$FXERR" ]; then
    echo "  ✗ 反测未过：夹具构造期间出现非预期 stderr —— 反测环境本身不干净"
    head -5 "$FXERR" | sed 's/^/      /'
    fail=1
  fi

  cd / ; rm -rf "$T"
  [ $fail -eq 0 ] && echo "--- 反测 $n 项全过（闸有牙）" || echo "--- 反测未过（闸没牙）"
  return $fail
}

case "${1:-}" in --selftest) selftest;; *) check;; esac
