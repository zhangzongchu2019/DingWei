#!/bin/sh
# 标题 / 版本字段一致闸 —— 三件各自的标题版本号须与其版本字段一致。
#
# 🔴 立此闸的直接原因：交付前实取三件版本时发现
#   **架构设计标题 v0.33、版本字段 v0.34** —— 我 bump 时只改了字段没改标题。
#   而闸 ① 的 `hdr-state` 判据**只覆盖 SpecDoc**（它是 speclint 对单件的规则），
#   架构件与原型**不在其判据面内** ⇒ 该不一致一路带到交付前才被人工发现。
#   ⚠️ 属「**断言作用域小于实际需要**」：判据本身对，但只装在一件上。
# ⚠️ 能力边界：判「两处数字是否相等」，判不了「该 bump 而没 bump」——
#   两处一起忘记改，本闸看不出来。那由闸 ④（原型内容改了必 bump）与人审承担。
set -u

check() {
  _bad=0
  for f in docs/dingwei-platform-specdoc.md docs/dingwei-architecture-design.md; do
    [ -f "$f" ] || { echo "✗ 闸依赖缺失：$f"; return 2; }
    _t=$(head -1 "$f" | grep -oE 'v[0-9]+\.[0-9]+' | head -1)
    _v=$(grep -m1 -oE '^\*\*版本\*\*: v[0-9]+\.[0-9]+' "$f" | grep -oE 'v[0-9]+\.[0-9]+')
    [ -n "$_t" ] && [ -n "$_v" ] || { echo "✗ $f：标题或版本字段取不到 —— 取不到就不能报通过"; return 2; }
    if [ "$_t" != "$_v" ]; then
      echo "✗ $f：标题 $_t 与版本字段 $_v 不一致"; _bad=$((_bad+1))
    fi
  done
  # 原型：标题 title 与页内自述锚须一致
  _p=docs/dingwei-prototype.html
  if [ -f "$_p" ]; then
    _a=$(grep -oE '原型 v[0-9]+\.[0-9]+' "$_p" | sort -u | wc -l | tr -d ' ')
    if [ "${_a:-0}" -gt 1 ]; then
      echo "✗ $_p：页内出现 $_a 个不同的「原型 vX.Y」自述"; grep -oE '原型 v[0-9]+\.[0-9]+' "$_p" | sort -u | sed 's/^/    /'
      _bad=$((_bad+1))
    fi
  fi
  [ "$_bad" -gt 0 ] && { echo "  ⇒ bump 时两处一起改；只改字段不改标题，读者按标题读到的就是旧版。"; return 1; }
  echo "  ✓ 三件标题与版本自述一致"
  echo "  ⚠️ 本闸判【两处是否相等】，判不了【该 bump 而没 bump】—— 两处一起忘改它看不出来。"
  return 0
}

selftest() {
  T=$(mktemp -d) || return 2
  n=0; fail=0
  ok(){ n=$((n+1)); echo "  ✓ $1"; }; bad(){ echo "  ✗ 反测未过：$1"; fail=1; }
  cd "$T" || return 2; mkdir -p docs
  _mk(){ printf '# X %s —— y\n\n**版本**: %s\n' "$1" "$2" > docs/dingwei-platform-specdoc.md
         printf '# Y %s —— y\n\n**版本**: %s\n' "$3" "$4" > docs/dingwei-architecture-design.md
         printf '<title>原型 %s</title>\n<b>原型 %s</b>\n' "$5" "$6" > docs/dingwei-prototype.html; }
  _mk v3.1 v3.1 v0.1 v0.1 v1.1 v1.1; check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：三件均一致时放行" || bad "一致被拦"
  _mk v3.1 v3.2 v0.1 v0.1 v1.1 v1.1; check >/dev/null 2>&1
  [ $? -eq 1 ] && ok "拦住：SpecDoc 标题与字段不一致" || bad "specdoc 不一致被放行"
  _mk v3.1 v3.1 v0.33 v0.34 v1.1 v1.1; check >/dev/null 2>&1
  [ $? -eq 1 ] && ok "🔴 拦住：架构件标题 v0.33 vs 字段 v0.34（本闸的立项实例）" || bad "架构件不一致被放行"
  _mk v3.1 v3.1 v0.1 v0.1 v1.1 v1.2; check >/dev/null 2>&1
  [ $? -eq 1 ] && ok "拦住：原型页内出现两个不同自述版本" || bad "原型多版本被放行"
  rm -f docs/dingwei-architecture-design.md; check >/dev/null 2>&1
  [ $? -eq 2 ] && ok "拦住：件缺失判闸坏（非免检）" || bad "件缺失被当免检"
  cd /; rm -rf "$T"
  [ $fail -eq 0 ] && echo "--- 反测 $n 项全过（闸有牙）" || echo "--- 反测未过（闸没牙）"
  return $fail
}
case "${1:-}" in --selftest) selftest;; *) check;; esac
