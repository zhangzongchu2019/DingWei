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
#
# 🔴 **ROOTS 为什么是这三棵 —— 丙域 kimi 2026-08-25 裁定，理由写在这里而不是留在消息里**：
#   本包服务的是「规范 / 设计 / 界面契约 / 闸」的评审。
#   **不扩 cmd/ 与 internal/**：实现代码的事实由架构设计 A§6.2 的测量命令与
#   specs/evidence/collect.sh 承载（丙域已在真仓逐条复现），把实现代码收进包里
#   只会把评审面稀释成代码仓。
#   ⚠️ 何时该扩：若将来 conformance suite 本身成为评审对象，再扩 ROOTS **并同期在此注明理由**。
#   ⇒ 这一条是把「人定边界」落成「声明式边界」：边界仍由人定，但**理由与脚本同在**，
#     下一个想改它的人能看到当初为什么这么定，而不必去翻消息记录。
# 🔴 扩面条件已达成并已扩（甲域 sol：脚本自定「若 conformance suite 成为评审对象，再扩 ROOTS」，
#   而 C.3a 硬前置 #2 已把 board conformance suite 定为换库的不可绕前置 —— 它已是被依赖的评审对象）。
#   ⚠️ 只扩到 conformance 件本身，不把 internal/ 整棵收进来：
#     实现代码的事实由架构设计 A§6.2 的测量命令与 specs/evidence/collect.sh 承载，
#     整棵进包只会把评审面稀释成代码仓。
ROOTS='docs/tools specs scripts'
CONFORMANCE=${CONFORMANCE:-'internal/mcs/board/conformance_test.go'}

# 🔴 basename 压平的同名冲突：不再静默覆盖（甲域 sol）。
#   原实现 b=$(basename "$f") 后写 $D/$b —— 两个不同子树的同名件，后者覆盖前者，
#   而 MANIFEST 照旧列两行 ⇒ 自检时必有一行 sha256 对不上。
#   ⚠️ sol 当时判【非阻断】，理由是「34 件恰好无 basename 冲突」；
#     SA 批新增 specs/tools/pending-debt/main.go 后，与 handle-escape/main.go 同名，
#     **「恰好」变成了「已发生」** —— 一条以「目前恰好成立」为由降级的 finding，
#     在下一次改动后就成了阻断。故本次改为【结构上不可能冲突】：目录层用 __ 连进文件名。
_flat() { printf '%s' "$1" | sed 's|/|__|g'; }
# DOCS 允许由环境覆盖 —— 仅为【可测性】,默认值不变。
# 没有这个钩子,「具名件缺失须报错」这条就只能靠读代码相信,无法用反测证明。
DOCS=${DOCS:-'docs/dingwei-platform-specdoc.md docs/dingwei-platform-architecture.md
docs/dingwei-prototype.html docs/v0.1/task-table-collision-analysis.md docs/DELIVERY-STATUS.md
docs/v0.1/reference/not-adopted-pg-rls.md docs/SESSION-HANDOFF.md
docs/dingwei-revision-history.md'}

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
  git ls-tree -r --name-only "$_r" -- $ROOTS $DOCS $CONFORMANCE | sort > "$TMP/all"
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

  # 🔴 同名冲突（甲域 sol）：两个不同子树的同名件不得静默覆盖。
  #   判据是【结构上不可能】而非【当前恰好没有】—— 后者会在下一次改动时失效，本仓已实证。
  _c=$(build_list HEAD "$DEF_EXCL" | while read -r f; do _flat "$f"; echo; done | sort | uniq -d | wc -l | tr -d ' ')
  [ "$_c" -eq 0 ] && ok "压平后无同名冲突（结构上不可能，非「恰好没有」）" \
    || bad "压平后仍有 $_c 组同名冲突"
  _a=$(_flat 'specs/tools/handle-escape/main.go'); _b=$(_flat 'specs/tools/pending-debt/main.go')
  [ "$_a" != "$_b" ] && ok "拦住：两处 main.go 压平后仍可区分" || bad "两处 main.go 压平后同名"

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

  # ⑥ DOCS 具名件缺失须报错，不得静默少导（丙域 kimi 观察 ①）
  # ⚠️ 必须写成 if !，不能写成「命令换行后再判 $?」—— 本脚本 set -e，
  #    一个【预期失败】的命令会直接终止整个反测,后面几项根本不跑,而结尾那句
  #    「N 项全过」也不会打印。上一版我就这么写的,反测静悄悄少跑两项。
  #    if 条件里的命令豁免于 set -e,这是唯一正确的写法。
  if ! DOCS="docs/nosuch-file-for-selftest.md" sh "$0" HEAD "$TMP/out6" >/dev/null 2>&1; then
    ok "拦住:具名正文件在锚上不存在(不静默少导)"
  else
    bad "具名件缺失被静默少导"
  fi
  # 反向:默认 DOCS 全部存在时必须正常导出,否则修法把正常路径堵死了。
  if sh "$0" HEAD "$TMP/out7" >/dev/null 2>&1; then
    ok "不误伤:默认 DOCS 齐全时正常导出"
  else
    bad "默认路径被堵死"
  fi

  [ $fail -eq 0 ] && echo "--- 反测 $n 项全过" || echo "--- 反测未过"
  return $fail
}

case "${1:-}" in --selftest) selftest; exit $? ;; esac

R=$1; D=$2; EXCL=${3:-$DEF_EXCL}
[ -f "$EXCL" ] || { echo "✗ 排除清单不存在：$EXCL —— 无清单则「全部」无从判定"; exit 1; }
excl_check "$R" "$EXCL"

# 🔴 DOCS 具名件的存在性校验（丙域 kimi 观察 ①）：
#   ROOTS 是整棵导出，少一件会被反测①（未分类件恒 0）兜住；但 DOCS 是【具名清单】——
#   某件若在该锚上不存在，git ls-tree 只会**静默少给一件**，MANIFEST 照样自称「完整评审件清单」。
#   与 v1 那条「手写清单声称全部」同类，只是范围更小。⇒ 具名即须校验存在。
_missing=''
for f in $DOCS; do
  git cat-file -e "$R:$f" 2>/dev/null || _missing="$_missing $f"
done
if [ -n "$_missing" ]; then
  echo "✗ 具名正文件在锚 $R 上不存在：$_missing"
  echo "  —— 具名清单少件必须报错，不得静默少导（否则 MANIFEST 会自称完整而实际缺件）。"
  echo "  若确要导出该件尚未入仓的历史锚，请先从 DOCS 中显式移除并说明，不要靠「反正它导不出来」。"
  exit 1
fi
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
    b=$(_flat "$f")
    git show "$R:$f" > "$D/$b"
    printf '| `%s` | `%s` | %s | %s | `%s` |\n' "$b" "$f" \
      "$(wc -l < "$D/$b")" "$(wc -c < "$D/$b")" "$(sha256sum "$D/$b" | cut -d' ' -f1)"
  done
} > "$D/MANIFEST.md"
echo "导出 $N 件（显式排除 $NX 件）→ $D"
