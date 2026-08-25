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

check() {
  _bad=0; _n=0
  for f in $DOCS; do
    [ -f "$f" ] || { echo "✗ 闸依赖缺失：$f 不存在"; return 2; }
    # 剥 RV 标注段后再抽路径
    # ⚠️ 有一类行的【内容本身】就是「该路径不存在」（架构 A§ 的实测取证表）——
    #   闸判对了形态、判错了意图。按仓内既有惯例给行内例外 `path-ok:<理由>`，
    #   理由非空且可被审；例外**可见**，不是静默豁免。
    _paths=$(grep -v 'path-ok:' "$f" 2>/dev/null | sed 's/【RV[^】]*】[^\n]*//g' \
             | grep -oE '`(docs|specs|scripts|internal)/[A-Za-z0-9_.@/-]+`' \
             | tr -d '`' | sort -u)
    for p in $_paths; do
      case "$p" in *'*'*|*'?'*) continue;; esac
      _n=$((_n+1))
      if [ ! -e "$p" ]; then
        [ "$_bad" -eq 0 ] && echo "✗ 正文引用的仓内路径解析不到（人工判据的落点指向空文件）："
        echo "    $f  →  $p"
        _bad=$((_bad+1))
      fi
    done
  done
  if [ "$_bad" -gt 0 ]; then
    echo "  🔴 **交代责任落点的引用最需要能被打开** —— 写错路径时闸不报，读的人才发现是空的。"
    echo "  ⇒ 用 git ls-files 核对后再写，不要凭印象。"
    return 1
  fi
  _ex=0
  for f in $DOCS; do
    [ -f "$f" ] || continue
    # ⚠️ 例外校验同样要先剥 RV 标注段：处置文本里会**提到** path-ok 这个词
    #   （RV-甲44 的处置就写着「按仓内惯例给行内 `path-ok:` 例外」），
    #   不剥就会把「说明例外机制的那句话」判成一个空理由的例外。
    #   —— 与「判到了指出问题的那句话」同形，本轮第三次。
    _stripped=$(sed 's/【RV[^】]*】[^\n]*//g' "$f" 2>/dev/null)
    _e=$(printf '%s\n' "$_stripped" | grep -c 'path-ok:' 2>/dev/null || true); _ex=$((_ex + ${_e:-0}))
    if printf '%s\n' "$_stripped" | grep -qE 'path-ok:[[:space:]]*(\||$)'; then
      echo "✗ $f：有 path-ok 例外未写理由 —— 例外必须可被审"; return 1
    fi
  done
  echo "  ✓ 正文仓内路径锚全部解析得到（$_n 个引用；另有 $_ex 行标 path-ok 例外，理由非空已校验）"
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

  # 【RV-甲47|甲|阻断|采纳】本反测源码已被 RV 处置文本直接拼到 shell 命令行：第 91 行在重定向目标后跟着“⟹【采纳】…”及多个反引号，实跑会尝试执行 specs/platform/checklist.md、scripts/path-anchor-gate.sh、path-ok:，连续打印 Permission denied / No such file / command not found，却最终仍报“8 项全过”并 rc=0。即反测夹具自身执行失败而通过计数未感知。须把 RV 标注与处置放在纯注释行、恢复原夹具写入命令，并让 selftest 捕获任何非预期 stderr/子命令失败；另加一条“夹具构造命令失败 ⇒ selftest FAIL”反测。　　⟹ **【采纳】** 🔴 **成立，而且是最坏的一类：闸自己的反测在报假通过。** 真因：我的批量落处置脚本把处置文本**拼进了 shell 命令行**，其中的反引号触发命令替换，实跑连出 Permission denied / No such file / command not found，而 selftest 仍打印 8/8 全过 —— **通过计数只统计了断言，没统计断言所依赖的前置动作**。已按甲域三条全做：① 标注与处置移到纯注释行、恢复原夹具写入命令（stderr 实测清零）；② 夹具构造走 `mkfx`，自己判成败，并收集非预期 stderr（不干净即 FAIL）；③ 新增反测「夹具构造失败 ⇒ selftest FAIL」，并做变异体验证（把夹具目标改成不存在的目录，反测如期失守）。🔴 **更根本的一层我另立了判据**：根因不在某一次手滑，而在于【RV 标注可以合法出现在代码件里却没有位置约束】——我的脚本已四次写进代码行（两次续行、一次 printf 夹具、这次拼进命令）。⑫ 现增判：`.sh`／`.py` 内的 RV 标注只允许独占注释行，带正反两向反测。
  # 🔴 评审方在标注里【引述】错路径，不得被判
  # 【RV-甲44|甲|阻断|采纳】owner 锚写成 specs/checklists/checklist.md，仓内不存在（处置见 SpecDoc 内同 ID 标注；此处只留注释，不得拼进命令行）
  mkfx docs/a.md '【RV-甲44|甲|阻断|采纳】owner 锚写成 `specs/checklists/checklist.md`，仓内不存在\n' || return 1   # rv-fixture
  DOCS=docs/a.md check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：RV 标注段内引述的错路径不判（否则等于判指出问题的那句话）" || bad "标注内引述被误判"

  mkfx docs/a.md '| 🔴 `internal/view` 不存在 | 实测 | path-ok: 本行断言该路径不存在，是取证结果 |\n' || return 1
  DOCS=docs/a.md check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：path-ok 例外行（内容本身即「该路径不存在」）" || bad "带理由的例外被拦"

  mkfx docs/a.md '| 🔴 `internal/view` 不存在 | 实测 | path-ok: |\n' || return 1
  DOCS=docs/a.md check >/dev/null 2>&1
  [ $? -eq 1 ] && ok "拦住：path-ok 例外未写理由（例外必须可被审）" || bad "空理由的例外被放行"

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
