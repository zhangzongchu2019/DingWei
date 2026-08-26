#!/bin/sh
# workflow 触发面覆盖闸（提交闸 ㉗）—— CI 的 paths 必须覆盖【闸的载体本身】。
#
# 🔴 立此闸的直接原因（ops 2026-08-26 实测）：
#   .github/workflows/speclint.yml 的触发面曾是 paths:['docs/**']，
#   而闸脚本在 scripts/、基线在 specs/gates/ ——**闸的载体不在自己的触发面内**。
#   实例：38d4c46 只改 specs/**，完全未触发 CI，分支头一度停在闸 ㉒ 判 FAIL 的状态而无人告警。
#   ⇒ 上调 specs/gates/*-baseline.txt（**上调基线＝放宽判据**）或删掉闸脚本，CI 一声不响。
#
# ⚠️ 能力边界（照实写）：
#   · 本闸判【触发面是否覆盖必需前缀】与【push 与 pull_request 两处是否一致】；
#   · 🔴 **判不了 CI 是否真的跑了**，也判不了它是不是保护分支的必过状态检查
#     —— 后者是平台侧配置，不是 git 可见项，归仓库管理员。
#   · ⇒ **本闸过 ≠ 仓侧不可逆保护已落地**。触发面对了只是「红得出来」，不是「挡得住」。
set -u
SELF=$(cd "$(dirname "$0")" && pwd)/$(basename "$0")
WF=${WF:-.github/workflows/speclint.yml}
REQUIRED=${REQUIRED:-"docs/ scripts/ specs/ go.mod go.sum .github/"}

# 抽出某个 key 下的 paths 列表（简化解析：段内以 - '...' 开头的行）
paths_of() {
  awk -v key="$1" '
    $0 ~ "^  "key":" {inkey=1; next}
    inkey && /^  [a-z_]+:/ {inkey=0}
    inkey && /^    paths:/ {inp=1; next}
    inp && /^      - / {gsub(/^      - |'"'"'/,""); print; next}
    inp && !/^      - / {inp=0}
  ' "$WF"
}

check() {
  [ -f "$WF" ] || { echo "✗ 闸依赖缺失：$WF —— 走不到就不能报数"; return 2; }
  _p=$(paths_of push); _r=$(paths_of pull_request)
  [ -n "$_p" ] || { echo "✗ push.paths 解析不出 —— 解析不出就不能报通过（可能格式变了）"; return 2; }
  _bad=0
  # ① 两处一致
  if [ "$(printf '%s' "$_p" | sort)" != "$(printf '%s' "$_r" | sort)" ]; then
    echo "✗ push 与 pull_request 的 paths 不一致 —— 显式写两份就必须对撞"
    echo "    push: $(printf '%s' "$_p" | tr '\n' ' ')"
    echo "    pr  : $(printf '%s' "$_r" | tr '\n' ' ')"
    _bad=1
  fi
  # ② 覆盖必需前缀
  for _need in $REQUIRED; do
    if ! printf '%s\n' "$_p" | grep -q "^$_need"; then
      echo "✗ 触发面未覆盖：$_need"; _bad=1
    fi
  done
  [ "$_bad" -gt 0 ] && {
    echo "  ⇒ 闸的载体（脚本 / 基线 / 索引）若不在触发面内，改动它们时 CI 不响。"
    return 1; }
  echo "  ✓ 触发面覆盖必需前缀（$(printf '%s' "$REQUIRED")），且 push 与 pull_request 一致"
  echo "  ⚠️ 🔴 本闸过【不等于】仓侧不可逆保护已落地 —— 触发面对了只是「红得出来」，"
  echo "     是否为保护分支的必过状态检查属平台侧配置，非 git 可见项，归仓库管理员。"
  return 0
}

selftest() {
  T=$(mktemp -d) || { echo "✗ 夹具目录建不出 ⇒ selftest FAIL"; return 2; }
  n=0; fail=0
  ok(){ n=$((n+1)); echo "  ✓ $1"; }; bad(){ echo "  ✗ 反测未过：$1"; fail=1; }
  # 🔴 须 set -f：`docs/**` 等在 `for x in $1` 里会被当通配符**展开成真实文件**
  #    （首版夹具因此解析出 104 条而非 12 条）。关掉路径展开再构造。
  mkwf(){ set -f; { echo "on:"; echo "  push:"; echo "    paths:"; for x in $1; do echo "      - '$x'"; done
            echo "  pull_request:"; echo "    paths:"; for x in $2; do echo "      - '$x'"; done; } > "$T/wf.yml"
          grep -qc . "$T/wf.yml" || { echo "✗ 夹具构造失败 ⇒ selftest FAIL"; exit 2; }
          _c=$(WF="$T/wf.yml" awk '/^      - /{n++} END{print n+0}' "$T/wf.yml")
          [ "$_c" -eq "$(echo "$1 $2" | wc -w)" ] || { set +f; echo "✗ 夹具自检失败（解析 $_c 条）⇒ selftest FAIL"; exit 2; }
          set +f; }
  ALL="docs/** scripts/** specs/** go.mod go.sum .github/**"

  mkwf "$ALL" "$ALL"; WF="$T/wf.yml" sh "$SELF" >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：覆盖齐全且两处一致时放行" || bad "齐全被拦"

  mkwf "docs/**" "docs/**"; WF="$T/wf.yml" sh "$SELF" >/dev/null 2>&1
  [ $? -eq 1 ] && ok "🔴 拦住：只有 docs/**（正是本次实际发生的形态）" || bad "只 docs 被放行"

  mkwf "docs/** specs/** go.mod go.sum .github/**" "docs/** specs/** go.mod go.sum .github/**"
  _o=$(WF="$T/wf.yml" sh "$SELF" 2>&1); [ $? -eq 1 ] && printf '%s' "$_o" | grep -q 'scripts/' \
    && ok "拦住：漏 scripts/（闸脚本本体所在）且点名该前缀" || bad "漏 scripts/ 未被点名"

  mkwf "$ALL" "docs/**"; WF="$T/wf.yml" sh "$SELF" >/dev/null 2>&1
  [ $? -eq 1 ] && ok "🔴 拦住：push 与 pull_request 不一致（显式写两份必须对撞）" || bad "两处漂移被放行"

  WF="$T/nofile.yml" sh "$SELF" >/dev/null 2>&1
  [ $? -eq 2 ] && ok "🔴 件缺失 ⇒ exit 2（走不到就不能报数）" || bad "件缺失却报通过"

  _err=$( ( mkwf "$ALL" "$ALL"; WF="$T/wf.yml" sh "$SELF" >/dev/null ) 2>&1 )
  [ -z "$_err" ] && ok "🔴 通过时 stderr 干净（报错不得与 ✓ 同时出现）" || bad "通过时有 stderr：$_err"

  rm -rf "$T"
  [ $fail -eq 0 ] && { echo "  触发面覆盖闸反测 $n/$n ✓"; return 0; }
  return 1
}
case "${1:-}" in --selftest) selftest;; *) check;; esac
