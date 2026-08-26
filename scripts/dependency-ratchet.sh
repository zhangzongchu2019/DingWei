#!/bin/sh
# 依赖清单棘轮（§4.7.1a 默认约束「依赖不增」的对应物）—— 外部依赖数只许降不许升。
#
# 🔴 立此闸的直接原因：§4.7.1a 把「依赖不增」立为**不声明即生效**的默认约束。
#   一条不声明即生效的约束，若没有事后可机器验证的对应物，
#   就退化成「记得少加依赖」——那是纪律，不是机制（§22.2c 同一条判法）。
#
# ⚠️ 能力边界：
#   · 本闸判【外部依赖的【数量】是否回升】，按 go.mod 的 require 块实算；
#   · 🔴 **判不了「这个依赖该不该加」** —— 那是设计判断，归门方；
#   · 🔴 **也判不了「换了个等价依赖」** —— 数不变而依赖变了，本闸看不出来；
#     故基线文件同时记**依赖名集合的 sha256**，集合变了即使数不变也提示（提示，不判门）。
#   · 突破默认须在 spec §4.7.1 ⑤ 显式声明并按
#     `Violation | Why Needed | Simpler Alternative Rejected Because` 论证，
#     然后**同步下调/上调基线**——基线改动本身是评审面，不得静默。
set -u

# 🔴 自解析绝对路径 —— 反测须能从任意 cwd 被调用（曾依赖 $OLDPWD，从提交闸调用时失效）
SELF=$(cd "$(dirname "$0")" && pwd)/$(basename "$0")

GOMOD=${GOMOD:-go.mod}
BASE=${BASE:-specs/gates/dependency-baseline.txt}

# 直接依赖名集合（require 块内、不含 // indirect）
direct_deps() {
  [ -f "$GOMOD" ] || return 2
  awk '/^require \(/{f=1;next} f&&/^\)/{f=0} f&&!/\/\/ indirect/{print $1}' "$GOMOD" |
    grep -v '^$' | sort -u
}

check() {
  _d=$(direct_deps) || { echo "✗ 闸依赖缺失：$GOMOD 不存在 —— 走不到就不能报数"; return 2; }
  _n=$(printf '%s\n' "$_d" | grep -c . || echo 0)
  _h=$(printf '%s\n' "$_d" | sha256sum | cut -d' ' -f1)
  [ -f "$BASE" ] || { echo "✗ 基线缺失：$BASE —— 无基线不能判棘轮，不得报通过"; return 2; }
  _bn=$(awk -F= '/^direct_count=/{print $2}' "$BASE")
  _bh=$(awk -F= '/^direct_sha256=/{print $2}' "$BASE")
  case "$_bn" in ''|*[!0-9]*) echo "✗ 基线值非法：direct_count=$_bn"; return 2 ;; esac

  echo "  直接依赖：$_n（基线 $_bn）"
  if [ "$_n" -gt "$_bn" ]; then
    echo "✗ 依赖数回升 $_bn → $_n —— 违反默认约束「依赖不增」（§4.7.1a）"
    printf '%s\n' "$_d" | sed 's/^/      /'
    echo "  ⇒ 要么去掉新增依赖，要么在 spec §4.7.1 ⑤ 显式声明突破并按"
    echo "     Violation | Why Needed | Simpler Alternative Rejected Because 论证，再同步上调基线。"
    return 1
  fi
  if [ "$_n" -lt "$_bn" ]; then
    echo "  ✓ 依赖数下降 $_bn → $_n —— 请同步下调基线（棘轮只认更严的值）"
  fi
  if [ "$_h" != "$_bh" ]; then
    echo "  ⚠️ 依赖【集合】已变（数未回升）：基线 ${_bh%%????????????????????????????????????????????????}… ≠ 现值 $(echo "$_h" | cut -c1-12)…"
    echo "     🔴 本闸判数不判换 —— 换成等价依赖它看不出来，须人审确认。"
  fi
  echo "  ✓ 依赖清单棘轮未回升"
  echo "  ⚠️ 本闸判【数是否回升】，🔴 判不了【该不该加】，也判不了【等价替换】—— 归门方。"
  return 0
}

selftest() {
  T=$(mktemp -d) || { echo "✗ 夹具目录建不出 ⇒ selftest FAIL"; return 2; }
  n=0; fail=0
  ok(){ n=$((n+1)); echo "  ✓ $1"; }
  bad(){ echo "  ✗ 反测未过：$1"; fail=1; }
  G="$SELF"
  # 🔴 夹具须逐行写 —— printf '%s' 不解释参数里的 \n，曾把整个 require 块挤成一行，
  #    导致「持平放行」假通过（基线与现值都从同一个坏夹具算出，自然相等）。
  mk(){ { printf 'module x\n\nrequire (\n'; for _d in $1; do printf '\t%s v1\n' "$_d"; done; printf ')\n'; } > "$T/go.mod"
        grep -qc . "$T/go.mod" || { echo "✗ 夹具构造失败 ⇒ selftest FAIL"; exit 2; }
        _c=$(awk '/^require \(/{f=1;next} f&&/^\)/{f=0} f&&!/\/\/ indirect/{print $1}' "$T/go.mod" | grep -c .)
        [ "$_c" -eq "$(echo $1 | wc -w)" ] || { echo "✗ 夹具自检失败：应有 $(echo $1|wc -w) 个依赖，实解析 $_c ⇒ selftest FAIL"; exit 2; } }
  mkbase(){ d=$(awk '/^require \(/{f=1;next} f&&/^\)/{f=0} f&&!/\/\/ indirect/{print $1}' "$T/go.mod" | grep -v '^$' | sort -u)
            printf 'direct_count=%s\ndirect_sha256=%s\n' "$(printf '%s\n' "$d"|grep -c .)" "$(printf '%s\n' "$d"|sha256sum|cut -d' ' -f1)" > "$T/base.txt"; }

  mk "a b"; mkbase
  GOMOD="$T/go.mod" BASE="$T/base.txt" sh "$G" >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：与基线持平时放行" || bad "持平被拦"

  mk "a b c"
  GOMOD="$T/go.mod" BASE="$T/base.txt" sh "$G" >/dev/null 2>&1
  [ $? -eq 1 ] && ok "拦住：依赖数回升（2 → 3）" || bad "回升被放行"

  mk "a"
  GOMOD="$T/go.mod" BASE="$T/base.txt" sh "$G" >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：依赖数下降时放行（并提示下调基线）" || bad "下降被拦"

  # 🔴 数不变而集合变：不判门，但必须【看得见】——否则等于静默放过
  mk "a z"
  _out=$(GOMOD="$T/go.mod" BASE="$T/base.txt" sh "$G" 2>&1)
  printf '%s' "$_out" | grep -q '依赖【集合】已变' \
    && ok "🔴 数不变而集合变：不判门，但明确提示（判数不判换，须人审）" || bad "等价替换被静默放过"

  # 🔴 无基线不得报通过
  GOMOD="$T/go.mod" BASE="$T/nobase.txt" sh "$G" >/dev/null 2>&1
  [ $? -eq 2 ] && ok "🔴 基线缺失 ⇒ exit 2，不报通过（无基线不能判棘轮）" || bad "无基线却报通过"

  # 🔴 go.mod 缺失不得报通过
  GOMOD="$T/nogomod" BASE="$T/base.txt" sh "$G" >/dev/null 2>&1
  [ $? -eq 2 ] && ok "🔴 受检件缺失 ⇒ exit 2（走不到就不能报数）" || bad "件缺失却报通过"

  rm -rf "$T"
  [ $fail -eq 0 ] && { echo "  依赖棘轮反测 $n/$n ✓"; return 0; }
  return 1
}

case "${1:-}" in --selftest) selftest;; *) check;; esac
