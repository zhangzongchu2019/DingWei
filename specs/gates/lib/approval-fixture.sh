#!/bin/sh
# 受控例外的【批准夹具】—— 四个闸的反测共用。
#
# 🔴 为什么单独成件：丙机制的夹具要造一个**独立 git 仓 ＋ 受保护 ref ＋ 不可变 commit**，
#   四个闸各抄一份必然漂移（本轮已实测过这一族：同一规则的多个使用点，改一个不等于改完）。
#   ⇒ 一份实现，四闸共用。
#
# ⚠️ 本件只供 --selftest 使用；真实判定路径不读它。
set -u

# exc_fixture_make <工作目录> <门方会话> <claim_id> <被抑制对象> <件> <行指纹> <理由码>
#   造出：<工作目录>/appro（门方独占仓，分支 review/approvals-test）
#         <工作目录>/trust.md（含 ACTIVE ＋ REF 两行）
#   打印：批准记录所在的 **40 位不可变 commit**
exc_fixture_make() {
  _fw=$1; _frev=$2; _fcid=$3; _fobj=$4; _ffile=$5; _ffp=$6; _fcode=$7
  _fr="$_fw/appro"
  rm -rf "$_fr"; mkdir -p "$_fr/approvals" || return 1
  git -C "$_fr" init -q -b review/approvals-test >/dev/null 2>&1 || return 1
  printf '%s\n' "$_frev" > "$_fr/reviewers.txt"
  # 记录列：<记录ID> <claim_id> <被抑制对象> <件> <行指纹> <理由码> <批准人> <批准时刻UTC>
  printf '# OWNER: %s\n' "$_frev" > "$_fr/approvals/rec.approvals"
  printf 'AP-01\t%s\t%s\t%s\t%s\t%s\t%s\t2026-08-26T00:00:00Z\n' \
    "$_fcid" "$_fobj" "$_ffile" "$_ffp" "$_fcode" "$_frev" >> "$_fr/approvals/rec.approvals"
  git -C "$_fr" add -A >/dev/null 2>&1 || return 1
  git -C "$_fr" -c user.name=fx -c user.email=fx@example.invalid \
      commit -q -m "approval fixture" >/dev/null 2>&1 || return 1
  _fc=$(git -C "$_fr" rev-parse HEAD 2>/dev/null) || return 1
  exc_fixture_trust "$_fw" "$_frev" reviewer-owned-protected-ref
  printf '%s' "$_fc"
}

# exc_fixture_trust <工作目录> <门方会话> [后端名；留空＝无 ACTIVE 后端]
exc_fixture_trust() {
  : > "$2.tmp"; rm -f "$2.tmp"
  : > "$1/trust.md"
  [ -n "${3:-}" ] && printf 'ACTIVE: %s\n' "$3" >> "$1/trust.md"
  printf 'REF: %s review/approvals-test approvals/rec.approvals\n' "$2" >> "$1/trust.md"
  return 0
}
