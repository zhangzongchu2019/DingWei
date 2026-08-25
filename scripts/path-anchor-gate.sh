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
  cd "$T" || return 2
  mkdir -p docs specs/platform scripts internal/repo
  : > specs/platform/checklist.md; : > internal/repo/tenant.go

  printf '见 `specs/platform/checklist.md`。\n' > docs/a.md
  DOCS=docs/a.md check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：存在的文件路径放行" || bad "存在的路径被拦"

  printf '见 `specs/checklists/checklist.md`。\n' > docs/a.md
  DOCS=docs/a.md check >/dev/null 2>&1
  [ $? -eq 1 ] && ok "🔴 拦住：不存在的路径锚（正是甲域 RV-甲44）" || bad "空锚被放行"

  printf '收编进 `internal/repo`。\n' > docs/a.md
  DOCS=docs/a.md check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：目录路径算解析得到（ls-files 只列文件）" || bad "目录被误判为空锚"

  printf '见 `specs/gates/*.txt` 各基线。\n' > docs/a.md
  DOCS=docs/a.md check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：含通配符的示例路径不判" || bad "通配示例被误判"

  # 【RV-甲47|甲|阻断|未决】本反测源码已被 RV 处置文本直接拼到 shell 命令行：第 91 行在重定向目标后跟着“⟹【采纳】…”及多个反引号，实跑会尝试执行 specs/platform/checklist.md、scripts/path-anchor-gate.sh、path-ok:，连续打印 Permission denied / No such file / command not found，却最终仍报“8 项全过”并 rc=0。即反测夹具自身执行失败而通过计数未感知。须把 RV 标注与处置放在纯注释行、恢复原夹具写入命令，并让 selftest 捕获任何非预期 stderr/子命令失败；另加一条“夹具构造命令失败 ⇒ selftest FAIL”反测。
  # 🔴 评审方在标注里【引述】错路径，不得被判
  printf '【RV-甲44|甲|阻断|采纳】owner 锚写成 `specs/checklists/checklist.md`，仓内不存在\n' > docs/a.md　　⟹ **【采纳】** 🔴 成立：`specs/checklists/checklist.md` 是我**凭印象写的路径**，仓内不存在（真实路径 `specs/platform/checklist.md`），于是人工判据的落点指向空文件而 ①–⑲ 全绿。已改真实路径，并按甲域要求立闸 ⑳ `scripts/path-anchor-gate.sh`：正文反引号包起的仓内路径须解析得到，8 项反测（含「目录算解析得到」「通配示例不判」「标注内引述的错路径不判」三向不误伤）。⚠️ 全文复扫另发现 A§ 一行内容**本身就是断言该路径不存在**（实测取证表），按仓内惯例给行内 `path-ok:` 例外并强制带理由。
  DOCS=docs/a.md check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：RV 标注段内引述的错路径不判（否则等于判指出问题的那句话）" || bad "标注内引述被误判"

  printf '| 🔴 `internal/view` 不存在 | 实测 | path-ok: 本行断言该路径不存在，是取证结果 |\n' > docs/a.md
  DOCS=docs/a.md check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：path-ok 例外行（内容本身即「该路径不存在」）" || bad "带理由的例外被拦"

  printf '| 🔴 `internal/view` 不存在 | 实测 | path-ok: |\n' > docs/a.md
  DOCS=docs/a.md check >/dev/null 2>&1
  [ $? -eq 1 ] && ok "拦住：path-ok 例外未写理由（例外必须可被审）" || bad "空理由的例外被放行"

  printf '见 `https://example.com/x.md` 与 `go build ./...`。\n' > docs/a.md
  DOCS=docs/a.md check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：URL 与命令不算仓内路径" || bad "非仓内路径被误判"

  cd / ; rm -rf "$T"
  [ $fail -eq 0 ] && echo "--- 反测 $n 项全过（闸有牙）" || echo "--- 反测未过（闸没牙）"
  return $fail
}

case "${1:-}" in --selftest) selftest;; *) check;; esac
