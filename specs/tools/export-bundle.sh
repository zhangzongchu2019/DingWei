#!/bin/sh
# 导出锚定快照包 —— 件清单【由 git 从整棵子树导出】，作用域与枚举都不手写。
#
# 🔴 立此脚本的直接原因（丙域 kimi 终审 m 类）：
#   MANIFEST 此前由手写的 files=[...] 生成，却自述「全部闸与工具的源码」，
#   实际漏了 specs/evidence/collect.sh；自查后发现漏的不止一件。
#   ⇒ **凡说「全部」，清单就不能手写。**
#
# 🔴 v2（2026-08-25，SP7a 开工当天自查）：上一版只把【枚举】机械化了，**作用域仍是手写的**——
#   INCLUDE 是一串手写路径，git ls-tree 只在这串之内枚举。
#   后果：新立的 scripts/repo-boundary-gate.sh 与 specs/gates/* 一件都进不了快照包，
#   而 MANIFEST 照旧自称「完整评审件清单」。
#   ⇒ **同一个缺陷复发，只是升了一层**：治了「手写件名」，没治「手写范围」。
#   本版改为：作用域＝ROOTS 整棵 + 具名正文件，少带一件必须在
#   specs/tools/export-exclude.txt 里显式写明并给理由。**默默不带 与 写明不带，是两件事。**
#
# 用法: specs/tools/export-bundle.sh <锚> <目标目录> [排除清单]
#       specs/tools/export-bundle.sh --selftest
set -eu

DEF_EXCL=specs/tools/export-exclude.txt

# 作用域：这三棵子树整棵进，加上具名的正文件。
# 新增任何闸 / 工具 / 语料 / 基线，落在 ROOTS 里就自动进清单，不需要有人记得来改这里。
ROOTS='docs/tools specs scripts'
DOCS='docs/dingwei-platform-specdoc.md docs/dingwei-architecture-design.md
docs/dingwei-prototype.html docs/task-table-collision-analysis.md docs/DELIVERY-STATUS.md
docs/reference/not-adopted-pg-rls.md docs/SESSION-HANDOFF.md'

# 读排除清单 -> 每行「路径 理由」；校验存在性与理由非空
excl_paths() { sed 's/#.*//' "$1" | awk 'NF>0 {print $1}'; }
# ⚠️ 不把校验放进管道里的 while —— 管道每段跑在子 shell，里面的 exit 1 只退子 shell，
#    外层照常往下走，于是「校验」会变成一个永远不失败的摆设。这正是「闸没起作用」的一种形态。
#    改用重定向喂 while（本 shell 执行），失败置标志位，循环后统一 return。
excl_check() { # excl_check 锚 清单文件
  _r=$1; _f=$2; _bad=0
  sed 's/#.*//' "$_f" | awk 'NF>0' > "$TMP/.excl.$$"
  while IFS= read -r line; do
    _p=$(printf '%s' "$line" | awk '{print $1}')
    _why=$(printf '%s' "$line" | cut -s -d' ' -f2-)
    if [ -z "$_why" ]; then
      echo "✗ 排除项没有理由：$_p —— 没有理由的排除等于默默不带" >&2; _bad=1
    elif ! git cat-file -e "$_r:$_p" 2>/dev/null; then
      echo "✗ 排除项在锚 $_r 上不存在：$_p —— 清单已漂移，导出失败" >&2; _bad=1
    fi
  done < "$TMP/.excl.$$"
  rm -f "$TMP/.excl.$$"
  return $_bad
}

build_list() { # build_list 锚 排除清单 -> 逐行路径
  _r=$1; _f=$2
  git ls-tree -r --name-only "$_r" -- $ROOTS $DOCS | sort > "$TMP/all"
  excl_paths "$_f" | sort > "$TMP/exc"
  comm -23 "$TMP/all" "$TMP/exc"
}

TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT

# ---- 反测：证明「新件会自动进清单」「排除必须写明」都不是嘴上说的 ----
selftest() {
  fail=0; n=0
  ok()  { n=$((n+1)); echo "  ✓ $1"; }
  bad() { echo "  ✗ 反测未过：$1"; fail=1; }
  R=HEAD

  # ① 作用域是整棵导出：ROOTS 下未被排除的件必须【全部】在清单里，未分类件恒为 0
  build_list "$R" "$DEF_EXCL" > "$TMP/list"
  git ls-tree -r --name-only "$R" -- $ROOTS | sort > "$TMP/roots"
  excl_paths "$DEF_EXCL" | sort > "$TMP/exc2"
  sort -u "$TMP/list" "$TMP/exc2" > "$TMP/known"   # /bin/sh 无进程替换，落临时文件
  unclassified=$(comm -23 "$TMP/roots" "$TMP/known" | wc -l | tr -d ' ')
  [ "$unclassified" -eq 0 ] && ok "ROOTS 下未分类件恒为 0（要么进清单，要么在排除清单里写明）" \
    || bad "有 $unclassified 件既不在清单也不在排除清单里"

  # ② 本轮新立的闸必须已被自动收入 —— 这正是 v1 漏掉的那些
  for f in scripts/repo-boundary-gate.sh specs/gates/boundary-baseline.txt specs/gates/repo-packages.txt; do
    grep -qx "$f" "$TMP/list" && ok "自动收入：$f" || bad "$f 未被自动收入清单"
  done

  # ③ 排除清单缺理由 -> 必须导出失败，而不是少带一件
  printf 'scripts/gen-status.sh\n' > "$TMP/noreason"
  if excl_check "$R" "$TMP/noreason" 2>/dev/null; then bad "缺理由的排除被放行"; else ok "拦住：排除项没写理由"; fi

  # ④ 排除清单指向不存在的路径 -> 判漂移、导出失败
  printf 'scripts/nosuch.sh 随便编的理由\n' > "$TMP/drift"
  if excl_check "$R" "$TMP/drift" 2>/dev/null; then bad "漂移的排除项被放行"; else ok "拦住：排除项在锚上不存在"; fi

  # ⑤ 排除确实生效：把一个在清单里的件排掉，件数须恰好少 1 且该件消失
  before=$(wc -l < "$TMP/list" | tr -d ' ')
  { cat "$DEF_EXCL"; printf 'scripts/gen-status.sh 反测用,验排除确实生效\n'; } > "$TMP/excl5"
  build_list "$R" "$TMP/excl5" > "$TMP/list5"
  after=$(wc -l < "$TMP/list5" | tr -d ' ')
  if [ $((before-after)) -eq 1 ] && ! grep -qx scripts/gen-status.sh "$TMP/list5"; then
    ok "排除确实生效（件数 $before → $after，该件已不在清单）"
  else bad "排除未生效（$before → $after）"; fi

  [ $fail -eq 0 ] && echo "--- 反测 $n 项全过" || echo "--- 反测未过"
  return $fail
}

case "${1:-}" in --selftest) selftest; exit $? ;; esac

R=$1; D=$2; EXCL=${3:-$DEF_EXCL}
[ -f "$EXCL" ] || { echo "✗ 排除清单不存在：$EXCL —— 无清单则「全部」无从判定"; exit 1; }
excl_check "$R" "$EXCL"
mkdir -p "$D"
LIST=$(build_list "$R" "$EXCL")
N=$(printf '%s\n' "$LIST" | wc -l | tr -d ' ')
NX=$(excl_paths "$EXCL" | wc -l | tr -d ' ')

{
  echo "# 完整评审件清单（锚定快照包）"
  echo
  echo "**统一锚**：\`$R\`　分支 \`design/addendum-decisions\`　**件数 $N**"
  echo
  echo "> 🔴 **作用域与枚举都不手写**：件清单由 \`git ls-tree\` 从 \`$ROOTS\` 整棵导出（加具名正文件），"
  echo "> 少带一件必须在 \`$EXCL\` 里显式写明并给出理由。**默默不带 与 写明不带，是两件事。**"
  echo "> 此前 v1 只把枚举机械化、作用域仍是手写的，新立的闸一件都进不来（2026-08-25 自查）。"
  echo
  echo "> ⚠️ **明确排除 $NX 件（有据，非默默不带）**："
  echo
  echo "| 排除路径 | 理由 |"
  echo "|---|---|"
  sed 's/#.*//' "$EXCL" | awk 'NF>0 {p=$1; $1=""; sub(/^ /,""); printf "| `%s` | %s |\n", p, $0}'
  echo
  echo "| 文件 | 仓内路径 | 行 | 字节 | sha256 |"
  echo "|---|---|---|---|---|"
  for f in $LIST; do
    b=$(basename "$f")
    git show "$R:$f" > "$D/$b"
    printf '| `%s` | `%s` | %s | %s | `%s` |\n' "$b" "$f" \
      "$(wc -l < "$D/$b")" "$(wc -c < "$D/$b")" "$(sha256sum "$D/$b" | cut -d' ' -f1)"
  done
} > "$D/MANIFEST.md"
echo "导出 $N 件（显式排除 $NX 件）→ $D"
