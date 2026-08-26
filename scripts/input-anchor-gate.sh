#!/bin/sh
# 输入锚解析闸（§4.7.1 ⑥「输入来源」面）—— spec 声明的输入锚必须解析得到。
#
# 🔴 立此闸的直接原因：§4.7.1 ⑥ 立面之后，「范围不扩 / 共识不变」两条默认约束
#   （§4.7.1a）的判据全部压在输入锚上 —— 锚若指向一个不存在的条目，
#   那两条约束就落空，而正文看起来是合规的（六面都填了）。
#
# ⚠️ 能力边界（写死在输出里，不许读者自行脑补）：
#   · 本闸只判【锚本身是否解析得到】：`commit＋sha256` 可取到 · 仓内路径存在 · 条目 ID 形态合法；
#   · 🔴 **判不了「陈述追不追得到锚」** —— 那是语义判断，归门方（附录 E.4 #66）。
#     锚都解析得到 ≠ 陈述都有出处。**两层不可互相冒充。**
#   · 「本 spec 无上游输入，系首发」是合法取值；**留空不是**（§4.7.1 铁律）。
set -u

# 🔴 自解析绝对路径 —— 反测须能从任意 cwd 被调用（曾依赖 $OLDPWD，从提交闸调用时失效）
SELF=$(cd "$(dirname "$0")" && pwd)/$(basename "$0")

DOCS=${DOCS:-'docs/dingwei-platform-specdoc.md'}
SECT=${SECT:-'输入锚'}

# 解析一个锚串，返回 0=解析得到 1=解析不到
resolve_anchor() {
  _a=$1
  case "$_a" in
    # 40 位 commit
    *[!0-9a-f]*) : ;;
    *) if [ ${#_a} -eq 40 ]; then git cat-file -e "$_a^{commit}" 2>/dev/null && return 0 || return 1; fi ;;
  esac
  # 仓内路径
  case "$_a" in
    docs/*|specs/*|scripts/*|internal/*|cmd/*|tools/*)
      { git ls-files --error-unmatch "$_a" >/dev/null 2>&1 || [ -e "$_a" ]; } && return 0 || return 1 ;;
  esac
  # 条目 ID：REQ-xxx / MSG-xxx / SP<n>
  case "$_a" in
    REQ-*|MSG-*|SP[0-9]*) return 0 ;;
  esac
  return 1
}

check() {
  _bad=0; _n=0; _files=0
  for f in $DOCS; do
    [ -f "$f" ] || { echo "✗ 闸依赖缺失：$f 不存在"; return 2; }
    _files=$((_files+1))
    # 只在标了「输入锚」的行上抽反引号内容（不全文扫，避免误伤）
    _lines=$(grep -n "$SECT" "$f" 2>/dev/null | grep -v 'input-anchor-ok:' || true)
    # 🔴 空集不得当通过（specs/gates/GATE-CONTRACT.md）：
    #   受检件里【一处 ⑥ 面标识都没有】，不是「锚都解析得到」，是**整面缺失**。
    #   §4.7.1 铁律：允许声明「无」，**不允许不提** ⇒ 此情形须 FAIL，不得 continue。
    #   ⚠️ 本行原写 `[ -z "$_lines" ] && continue` —— 一份连第六面都没有的 spec 一路绿到底。
    if [ -z "$_lines" ]; then
      echo "✗ $f 未见任何「$SECT」面标识 —— §4.7.1 ⑥ 整面缺失（允许声明「无上游输入，系首发」，但不允许不提）"
      continue
    fi
    echo "$_lines" | while IFS= read -r _l; do
      _ln=${_l%%:*}
      _body=${_l#*:}
      # 「无上游输入」是合法声明
      case "$_body" in *无上游输入*|*系首发*) continue ;; esac
      printf '%s\n' "$_body" | grep -oE '`[^`]+`' | tr -d '`' | while IFS= read -r _a; do
        [ -z "$_a" ] && continue
        # 只判像锚的：40hex / 仓内路径 / 条目 ID
        case "$_a" in
          docs/*|specs/*|scripts/*|internal/*|cmd/*|tools/*|REQ-*|MSG-*|SP[0-9]*) : ;;
          *) if [ ${#_a} -ne 40 ]; then continue; fi ;;
        esac
        if ! resolve_anchor "$_a"; then
          echo "✗ $f:$_ln 输入锚解析不到：$_a"
        fi
      done
    done
  done > /tmp/.iag.$$ 2>/dev/null
  # 🔴 不用 `grep -c ... || echo 0` —— 空文件时 grep 既输出「0」又退出 1，
  #    `||` 再补一个「0」⇒ 变量成了两行，`[ -gt ]` 报 Illegal number。
  #    **那次它仍打印了 ✓** —— 报错与 ✓ 同时出现，正是本仓反复记的形态。
  _bad=$(grep '^✗' /tmp/.iag.$$ 2>/dev/null | wc -l | tr -d ' \n')
  # 空集判 FAIL 的那行也写在同一个临时件里，故一并计入 _bad
  case "${_bad:-}" in ''|*[!0-9]*) echo "✗ 计数异常（_bad=$_bad）—— 算不出就不能报通过"; rm -f /tmp/.iag.$$; return 2 ;; esac
  cat /tmp/.iag.$$ 2>/dev/null | sed 's/^/  /'
  rm -f /tmp/.iag.$$
  [ "$_files" -eq 0 ] && { echo "✗ 受检件为 0 —— 走不到就不能报数"; return 2; }
  if [ "${_bad:-0}" -gt 0 ]; then
    echo "  ⇒ 锚解析不到 ⇒ §4.7.1a「范围不扩 / 共识不变」两条默认约束落空。修锚或改声明。"
    return 1
  fi
  echo "  ✓ 输入锚全部解析得到"
  echo "     受检范围：$_files 件（$DOCS）；命中 ⑥ 面标识的行已逐行解析"   # GATE-CONTRACT 三-1：范围句
  echo "     空集处置：受检件无任何 ⑥ 面标识 ⇒ 判 FAIL，不折叠为 PASS"    # GATE-CONTRACT 三-3
  echo "  ⚠️ 本闸判【锚是否解析得到】，🔴 判不了【陈述追不追得到锚】—— 后者是语义判断，归门方（E.4 #66）。"
  echo "     锚都解析得到 ≠ 陈述都有出处。"
  return 0
}

selftest() {
  T=$(mktemp -d) || { echo "✗ 夹具目录建不出 ⇒ selftest FAIL"; return 2; }
  n=0; fail=0
  ok(){ n=$((n+1)); echo "  ✓ $1"; }
  bad(){ echo "  ✗ 反测未过：$1"; fail=1; }
  mkdir -p "$T/docs" || { echo "✗ 夹具构造失败 ⇒ selftest FAIL"; return 2; }

  # ① 不误伤：锚解析得到时放行
  printf '输入锚：`docs/x.md`\n' > "$T/docs/a.md"; mkdir -p "$T/docs"; : > "$T/docs/x.md"
  ( cd "$T" && git init -q . 2>/dev/null; DOCS=docs/a.md sh "$SELF" >/dev/null 2>&1 )
  [ $? -eq 0 ] && ok "不误伤：锚解析得到时放行" || bad "解析得到却被拦"

  # ② 拦住：锚指向不存在的仓内路径
  printf '输入锚：`docs/nosuch-ghost.md`\n' > "$T/docs/b.md"
  ( cd "$T" && DOCS=docs/b.md sh "$SELF" >/dev/null 2>&1 )
  [ $? -eq 1 ] && ok "拦住：锚指向仓内不存在的路径" || bad "悬空锚被放行"

  # ③ 拦住：40hex 但不是本仓的 commit
  printf '输入锚：`%s`\n' "0000000000000000000000000000000000000000" > "$T/docs/c.md"
  ( cd "$T" && DOCS=docs/c.md sh "$SELF" >/dev/null 2>&1 )
  [ $? -eq 1 ] && ok "拦住：40hex 但仓内取不到该 commit" || bad "假 commit 被放行"

  # ④ 不误伤：「无上游输入，系首发」是合法声明
  printf '输入锚：本 spec 无上游输入，系首发\n' > "$T/docs/d.md"
  ( cd "$T" && DOCS=docs/d.md sh "$SELF" >/dev/null 2>&1 )
  [ $? -eq 0 ] && ok "不误伤：「无上游输入，系首发」放行（§4.7.1 铁律：允许声明无）" || bad "合法声明被拦"

  # ⑤ 🔴 走不到就不能报数：受检件不存在须 exit 2，不得报「全过」
  ( cd "$T" && DOCS=docs/nofile.md sh "$SELF" >/dev/null 2>&1 )
  [ $? -eq 2 ] && ok "🔴 受检件缺失 ⇒ exit 2（走不到就不能报数），不报全过" || bad "件缺失却报通过"

  # ⑥ 🔴 通过时【stderr 必须干净】—— 首版曾在报 ✓ 的同时打印 shell 报错
  #    （`[: Illegal number`）。**报错与 ✓ 同时出现 ⇒ 读者只看见 ✓。**
  _err=$( ( cd "$T" && DOCS=docs/a.md sh "$SELF" >/dev/null ) 2>&1 )
  [ -z "$_err" ] && ok "🔴 通过时 stderr 干净（报错不得与 ✓ 同时出现）" \
                 || bad "通过时有 stderr 输出：$_err"

  # ⑧ 🔴 空集不得当通过（GATE-CONTRACT §二）——本闸曾在此漏判：
  #    一份连第六面都没有的 spec，命中集为空 ⇒ 原实现 continue ⇒ rc=0 一路绿到底。
  #    甲域 sol 报、coordinator 独立实跑复现。
  printf '# spec\n\n本件没有任何第六面标识\n' > "$T/docs/z.md"
  ( cd "$T" && DOCS=docs/z.md sh "$SELF" >/dev/null 2>&1 )
  [ $? -eq 1 ] && ok "🔴 空集判 FAIL：受检件无任何 ⑥ 面标识 ⇒ 拦（不是「全部解析得到」）" \
                || bad "整面缺失被判通过 —— 空集又被当成已检查且合格"

  # ⑨ 但「显式声明无上游输入」仍须放行（允许声明无，不允许不提）
  printf '# spec\n\n输入锚：本 spec 无上游输入，系首发\n' > "$T/docs/y.md"
  ( cd "$T" && DOCS=docs/y.md sh "$SELF" >/dev/null 2>&1 )
  [ $? -eq 0 ] && ok "不误伤：显式声明「无上游输入」放行（与空集区分开）" || bad "合法声明被当空集拦"

  # ⑦ 真有失败项时：计数正确 ＋ 明确打印该锚
  printf '输入锚：`docs/ghost-a.md` 与 `docs/ghost-b.md`\n' > "$T/docs/e.md"
  _out=$( ( cd "$T" && DOCS=docs/e.md sh "$SELF" ) 2>&1 ); _rc=$?
  _cnt=$(printf '%s' "$_out" | grep -c '输入锚解析不到')
  { [ $_rc -eq 1 ] && [ "$_cnt" -eq 2 ]; } \
    && ok "两个悬空锚：均被点名且 return 1（计数正确）" \
    || bad "多锚失败计数不对（rc=$_rc cnt=$_cnt）"

  rm -rf "$T"
  [ $fail -eq 0 ] && { echo "  输入锚闸反测 $n/$n ✓"; return 0; }
  return 1
}

case "${1:-}" in --selftest) selftest;; *) check;; esac
