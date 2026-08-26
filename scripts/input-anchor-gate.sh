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

# 🔴 结构化⑥面声明的识别（甲域 sol 复核：v1 以 grep 命中【词汇】代替【声明】）
#   v1 两个洞：① `输入锚：` 声明存在但为空 ⇒ rc=0；② 「本文讨论输入锚如何工作」纯提及被误当声明 ⇒ rc=0。
#   ⇒ **词出现 ≠ 面已声明**。声明须在【声明位】：行首（可带 ⑥/列表符/表格首格）＋ 术语 ＋ 冒号。
_decl_lines() {   # 输出 "行号:正文"
  grep -nE '^[[:space:]]*(\|[[:space:]]*)?(\*\*)?(⑥[[:space:]]*)?(输入锚|输入来源)(\*\*)?[[:space:]]*[:：]' "$1" \
    | grep -v 'input-anchor-ok:'
}

check() {
  _bad=0; _files=0
  : > /tmp/.iag.$$
  for f in $DOCS; do
    [ -f "$f" ] || { echo "✗ 闸依赖缺失：$f 不存在"; rm -f /tmp/.iag.$$; return 2; }
    _files=$((_files+1))
    _decls=$(_decl_lines "$f")
    # 🔴 空集不得当通过：一处⑥面【声明】都没有 ⇒ 整面缺失（纯提及不算声明）
    if [ -z "$_decls" ]; then
      echo "✗ $f 未见任何【⑥ 输入来源 声明】—— 整面缺失（§4.7.1 允许声明「无上游输入，系首发」，但不允许不提）" >> /tmp/.iag.$$
      echo '    ⚠️ 正文中【提及】该词不算声明；声明须在行首 / 表格首格，形如「输入锚：<锚>」' >> /tmp/.iag.$$
      continue
    fi
    printf '%s\n' "$_decls" | while IFS= read -r _d; do
      _ln=${_d%%:*}; _body=${_d#*:}
      # (a) 显式声明「无」——合法
      case "$_body" in *无上游输入*|*系首发*) continue ;; esac
      # (b) 否则须至少有一个候选锚
      _cands=$(printf '%s\n' "$_body" | grep -oE '`[^`]+`' | tr -d '`' | awk 'NF')
      _got=0
      for _a in $_cands; do
        case "$_a" in
          docs/*|specs/*|scripts/*|internal/*|cmd/*|tools/*|REQ-*|MSG-*|SP[0-9]*) : ;;
          *) [ ${#_a} -eq 40 ] || continue ;;
        esac
        _got=$((_got+1))
        resolve_anchor "$_a" || echo "✗ $f:$_ln 输入锚解析不到：$_a" >> /tmp/.iag.$$
      done
      # 🔴 声明存在但为空 ⇒ 拒（甲域 sol 洞①）
      [ "$_got" -eq 0 ] && echo "✗ $f:$_ln ⑥面【声明为空】—— 既无候选锚，也未显式声明「无上游输入，系首发」" >> /tmp/.iag.$$
    done
  done
  _bad=$(grep -c '^✗' /tmp/.iag.$$ 2>/dev/null | head -1)
  case "${_bad:-}" in ''|*[!0-9]*) echo "✗ 计数异常（_bad=$_bad）—— 算不出就不能报通过"; rm -f /tmp/.iag.$$; return 2 ;; esac
  sed 's/^/  /' /tmp/.iag.$$; rm -f /tmp/.iag.$$
  [ "$_files" -eq 0 ] && { echo "✗ 受检件为 0 —— 走不到就不能报数"; return 2; }
  if [ "$_bad" -gt 0 ]; then
    echo "  ⇒ ⑥面缺失 / 声明为空 / 锚解析不到 ⇒ §4.7.1a「范围不扩·共识不变」两条默认约束落空。"
    return 1
  fi
  echo "  ✓ ⑥面声明齐备，且输入锚全部解析得到"
  echo "     受检范围：$_files 件（$DOCS）；🔴 判据＝【结构化声明位】，**正文中提及该词不算声明**"
  echo "     空集处置：无声明 ⇒ FAIL；声明为空 ⇒ FAIL；均不折叠为 PASS"
  echo "  ⚠️ 本闸判【声明是否齐备 ＋ 锚是否解析得到】，🔴 判不了【陈述追不追得到锚】—— 归门方（E.4 #66）。"
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

  # ===== v3 反测：甲域 sol 复核抓出的两个洞（2026-08-26）=====
  # 🔴 根因是 v2「以 grep 命中【词汇】代替【结构化声明】」——按名字判而非按效果判。

  # ⑩ 声明存在但为空 ⇒ 必拒
  printf '# spec\n\n输入锚：\n' > "$T/docs/e1.md"
  ( cd "$T" && DOCS=docs/e1.md sh "$SELF" >/dev/null 2>&1 )
  [ $? -eq 1 ] && ok "🔴 声明【存在但为空】⇒ 拒（sol 洞①：v2 在此 rc=0）" || bad "空声明被放行"

  # ⑪ 纯提及不算声明 ⇒ 视为整面缺失，必拒
  printf '# spec\n\n本文讨论输入锚如何工作，但未声明本件来源。\n' > "$T/docs/e2.md"
  ( cd "$T" && DOCS=docs/e2.md sh "$SELF" >/dev/null 2>&1 )
  [ $? -eq 1 ] && ok "🔴 正文【纯提及】该词不算声明 ⇒ 判整面缺失（sol 洞②：v2 在此 rc=0）" \
                || bad "纯提及被误当声明"

  # ⑫ 变异验证：把「声明位」判据改坏（退回词汇命中），上面两条须【如期失守】
  #    —— 不做这一步，就分不清「判据有效」与「样本恰好不触发」。
  #   🔴 **变异验证的两次自纠，都值得记：**
  #   ① 首版用 sed 注入把后续行一并注掉 ⇒ 变异体本身是坏的。**坏的变异体证明不了任何事。**
  #   ② 改对之后仍「未失守」—— 实测发现：把声明位退回词汇命中，纯提及样本【仍被拒】，
  #      因为「声明为空」那条判据**独立兜住了它**。
  #      ⇒ **本席原先的变异假设是错的**：那条反测测的不是「声明位」这一维。
  #      **变异体不失守，未必是判据没用；也可能是你以为它在测 A，其实是 B 接住的。**
  #   ⇒ 改为对「声明位」真正正交的变异：把【纯提及行】做成【带合法锚的提及】，
  #      此时只有「声明位」判据能拒它 —— 词汇命中版必然放行。
  #   ③ 第三次自纠：改对之后【仍】不失守 —— 因为变异体在夹具目录里跑，
  #      那个锚路径不存在，被「锚解析不到」这条**又一次**先接住了。
  #      🔴 **同一个坑连踩三次，共同形状是：想测 A，却被 B / C 接住而不自知。**
  #      ⇒ 正交化必须逐条排除【所有其它能拒它的判据】：此处让锚在夹具内**真实存在**。
  : > "$T/docs/real-anchor.md"
  printf '# spec\n\n本文讨论输入锚，例如 `docs/real-anchor.md` 这种写法。\n' > "$T/docs/e3.md"
  ( cd "$T" && DOCS=docs/e3.md sh "$SELF" >/dev/null 2>&1 )
  [ $? -eq 1 ] && ok "🔴 带合法锚的【纯提及】仍判整面缺失 —— 只有声明位判据能拒它" \
                || bad "带锚的提及被当成声明 —— 声明位判据没起作用"

  awk '/^_decl_lines\(\) \{/{print; print "  grep -n \"输入锚\" \"$1\"; return 0"; skip=1; next}
       skip && /^\}/{skip=0; print; next} skip{next} {print}' "$SELF" > "$T/mut.sh"
  sh -n "$T/mut.sh" 2>/dev/null || { bad "变异体语法坏 —— 坏的变异体证明不了任何事"; }
  ( cd "$T" && DOCS=docs/e3.md sh "$T/mut.sh" >/dev/null 2>&1 )
  [ $? -eq 0 ] && ok "🔴 变异验证：声明位判据退回词汇命中后，该样本【如期失守】⇒ 现判据真在起作用" \
                || bad "变异体未失守 —— 变异未构造成功，或该反测测的不是声明位这一维"

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
