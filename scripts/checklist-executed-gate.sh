#!/bin/sh
# checklist 执行落账闸 —— 「已按 checklist 执行」不得只是一句话。
#
# 立此闸的直接原因（甲域 sol，R1 全量评审）：
#   SpecDoc §0.4 第 2 项写「✅ 已闭…三域门方产出**并执行**，重走轮 verdict 全 PASS」，
#   而三份 reviewer checklist 的 **26 ＋ 18 ＋ 12 ＝ 56 个 checkbox 全为 `[ ]`**（实算），
#   安全件头块自述「**已签发，未执行**」。
#
# 🔴 §0.4 那一行紧接着还写了一句：
#     「闭合的是『按 checklist 取得了新门结果』，不是『有了 checklist 文件』—— 产出 ≠ 执行」
#   **它把要防的事说得一字不差，然后它防的那件事就发生了。**
#   三个状态里：(a) 有文件 ✅ (b) 按 checklist 逐项执行并落账 ❌ 0/56 (c) 拿到 verdict ✅（经消息）
#   闭合是拿 (c) 当了 (b)。而 **(c) 不蕴含 (b)** —— 不执行 checklist 也能产出 verdict。
#
# ⇒ **写下警告不等于让警告生效。警告没有执行者，就只是一句话。** 本闸即那个执行者。
#
# ⚠️ 能力边界，写明不假装：
#   本闸判的是「勾了没有、证据栏空不空」，**判不了「勾得对不对」** —— 那是门方的判断。
#   🔴 **不得因本闸通过而宣称「checklist 已正确执行」**，只能说「已落账」。
set -u

CKDIR=${CKDIR:-specs/platform}
SPECDOC=${SPECDOC:-docs/dingwei-platform-specdoc.md}
# 声称「已执行」的表述 —— 出现它就要求落账
CLAIM_RE=${CLAIM_RE:-'产出并执行|已执行|按其取得新的架构评审 verdict|逐项执行'}

_count() { [ -z "$1" ] && echo 0 || printf '%s\n' "$1" | wc -l | tr -d ' '; }

check() {
  [ -d "$CKDIR" ] || { echo "✗ 闸依赖缺失：$CKDIR 不存在 —— 闸坏了，不是免检"; return 2; }
  [ -f "$SPECDOC" ] || { echo "✗ 闸依赖缺失：$SPECDOC 不存在"; return 2; }

  _claim=$(grep -nE "$CLAIM_RE" "$SPECDOC" 2>/dev/null | grep -i checklist)
  _nclaim=$(_count "$_claim")

  _tot=0; _done=0; _files=0
  for f in "$CKDIR"/checklist*.md; do
    [ -f "$f" ] || continue
    case "$(basename "$f")" in *INDEX*) continue;; esac
    _files=$((_files+1))
    # 🔴 grep -c 无命中时【既输出 0 又返回 1】,写 `|| echo 0` 会补出第二个 0 ⇒ "0\n0",
    #   下一行算术就炸。本闸第一版即栽此。用 : 吞掉退出码,不要再加兜底输出。
    _t=$(grep -cE '^[[:space:]]*[-|][[:space:]]*\[' "$f" 2>/dev/null) || :
    _d=$(grep -cE '^[[:space:]]*[-|][[:space:]]*\[[xX!]\]' "$f" 2>/dev/null) || :
    _t=${_t:-0}; _d=${_d:-0}
    _tot=$((_tot+_t)); _done=$((_done+_d))
    echo "  · $(basename "$f")：$_t 项 · 已勾 $_d"
  done

  if [ "$_files" -eq 0 ]; then
    echo "✗ $CKDIR 下无 checklist 件 —— 闸坏了，不是免检"; return 2
  fi

  echo "  合计：$_tot 项 · 已勾 $_done · 未勾 $((_tot-_done))"

  if [ "$_nclaim" -eq 0 ]; then
    echo "  ✓ 正文未声称「已执行」，本闸不要求落账（现状：$_done/$_tot）"
    return 0
  fi

  echo "  ⓘ 正文有 $_nclaim 处声称 checklist 已执行："
  printf '%s\n' "$_claim" | head -3 | cut -c1-120 | sed 's/^/      /'

  if [ "$_done" -eq 0 ] && [ "$_tot" -gt 0 ]; then
    _exc=$(sed 's/#.*//' "${EXC:-specs/gates/checklist-execution-exceptions.txt}" 2>/dev/null | awk 'NF>0' | wc -l | tr -d ' ')
    if [ "${_exc:-0}" -gt 0 ]; then
      echo "⚠️ **未落账，但已登记为例外（$_exc 条）** —— 🔴 **这不是 PASS。**"
      echo "   声称「已执行」而 $_tot 个 checkbox 一个都没勾（$_done/$_tot）；"
      echo "   登记只是把债务变成可见的、有主的东西，**不是修复**。例外见 ${EXC:-specs/gates/checklist-execution-exceptions.txt}："
      sed 's/#.*//' "${EXC:-specs/gates/checklist-execution-exceptions.txt}" 2>/dev/null | awk 'NF>0' | sed 's/^/     /'
      return 0
    fi
    echo "✗ 正文声称「已执行」，而 $_tot 个 checkbox **一个都没勾**（$_done/$_tot）。"
    echo "  🔴 **消息侧的 verdict 不能取代 checkbox gate**（§4.1：只有 reviewer 能打 [x]，IMPLEMENT 读 checkbox 当 gate）。"
    echo "  ⇒ 三条出路：三域补执行并落勾与证据 ／ 撤回该声称 ／ 另立口径（须写明为何 verdict 可替代 checkbox，且该口径本身要过门）。"
    return 1
  fi

  # 已勾者须有证据 —— 无证据的 [x] 按 checklist 自定规则即为本域 NO
  _noev=0
  for f in "$CKDIR"/checklist*.md; do
    [ -f "$f" ] || continue
    case "$(basename "$f")" in *INDEX*) continue;; esac
    # 🔴 按【条目块】判，不按单行（丙域首次落勾时暴露）：
    #   门方的格式是 `- [x] 标题` 换行后再写「证据（时点，锚…）：…」——
    #   原实现只在 **checkbox 那一行**找证据关键词 ⇒ 把**证据齐全**的 8 项判成「证据栏为空」。
    #   ⚠️ 又一次「判据回答的问题不是要问的那个」：
    #     要问的是「这一项有没有证据」，判的却是「这一行里有没有证据词」。
    #   ⇒ 条目块 ＝ 该 checkbox 行起，至下一个 checkbox 行（或文件末）为止。
    _n=$(awk '
      /^[[:space:]]*[-|][[:space:]]*\[[xX]\]/ {
        if (open && !ev) bad++
        open=1; ev=0; blk=$0
      }
      open { if ($0 ~ /(sha256|锚|commit|命令|复现|证据|https?:\/\/|`[^`]+`)/) ev=1 }
      END { if (open && !ev) bad++; print bad+0 }
    ' "$f" 2>/dev/null)
    _noev=$((_noev+_n))
  done
  if [ "$_noev" -gt 0 ]; then
    echo "✗ 有 $_noev 项已勾但**证据栏为空** —— 按 checklist 自定规则，无证据的 [x] 即本域 NO。"
    return 1
  fi

  echo "  ✓ 声称与落账一致（已勾 $_done/$_tot，且已勾项均带证据）"
  echo "  ⚠️ 本闸只判「勾了没有、证据空不空」，**判不了勾得对不对** —— 那是门方的判断。"
  return 0
}

selftest() {
  T=$(mktemp -d) || return 2
  n=0; fail=0
  ok(){ n=$((n+1)); echo "  ✓ $1"; }; bad(){ echo "  ✗ 反测未过：$1"; fail=1; }

  # 🔴 证据写在【下一行】时不得判成「证据栏为空」（丙域首次落勾时暴露）
  mkdir -p "$T/ck1"; printf -- '- [x] **CAC-1 标题**\n  证据（2026-08-25，锚 abc1234）：实取输出如下。\n' > "$T/ck1/checklist.a.md"
  printf 'checklist 三域产出并执行完毕\n' > "$T/spec.md"; CKDIR="$T/ck1" SPECDOC="$T/spec.md" check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：证据在条目块的下一行（门方实际格式）" || bad "证据写下一行被判成空"

  # ⚠️ 真的没证据仍须拦
  mkdir -p "$T/ck2"; printf -- '- [x] **CAC-2 标题**\n  这里只有说明，没有任何。\n' > "$T/ck2/checklist.a.md"
  printf 'checklist 三域产出并执行完毕\n' > "$T/spec.md"; CKDIR="$T/ck2" SPECDOC="$T/spec.md" check >/dev/null 2>&1
  [ $? -eq 1 ] && ok "拦住：条目块内确无证据" || bad "无证据被放行"
  # 🔴 反测必须与真仓的例外清单隔离:否则真仓登记了例外,
  #   「该拦」的反测就会走进登记分支而假通过 —— 本闸第一版即栽此。
  mk(){ rm -rf "$T/p"; mkdir -p "$T/p"; printf '# spec\n\n正文\n' > "$T/s.md"; : > "$T/exc0.txt"; }
  run(){ CKDIR="$T/p" SPECDOC="$T/s.md" EXC="$T/exc0.txt" check >/dev/null 2>&1; }

  mk; printf '# ck\n\n- [ ] 项一\n- [ ] 项二\n' > "$T/p/checklist.md"
  run; [ $? -eq 0 ] && ok "未声称已执行时不要求落账（不误伤）" || bad "未声称却被拦"

  mk; printf '# ck\n\n- [ ] 项一\n- [ ] 项二\n' > "$T/p/checklist.md"
  printf '# spec\n\n三域门方 checklist 产出并执行，verdict 全 PASS\n' > "$T/s.md"
  run; [ $? -eq 1 ] && ok "🔴 拦住：声称「已执行」而 checkbox 一个没勾" || bad "空 checkbox + 声称已执行被放行"

  mk; printf '# ck\n\n- [x] 项一 证据 sha256 abc\n- [x] 项二 复现命令 `go test`\n' > "$T/p/checklist.md"
  printf '# spec\n\n三域门方 checklist 产出并执行\n' > "$T/s.md"
  run; [ $? -eq 0 ] && ok "不误伤：已勾且带证据时放行" || bad "已勾带证据仍被拦"

  mk; printf '# ck\n\n- [x] 项一\n- [x] 项二\n' > "$T/p/checklist.md"
  printf '# spec\n\n三域门方 checklist 产出并执行\n' > "$T/s.md"
  run; [ $? -eq 1 ] && ok "拦住：已勾但证据栏为空（无证据的 [x] 即本域 NO）" || bad "无证据的 [x] 被放行"

  mk; printf '# ck\n\n| [ ] | 项一 | 判据 |\n| [ ] | 项二 | 判据 |\n' > "$T/p/checklist.md"
  printf '# spec\n\nchecklist 产出并执行\n' > "$T/s.md"
  run; [ $? -eq 1 ] && ok "拦住：表格形态的 checkbox 同样计入（安全件即此形态）" || bad "表格形态被漏计"

  mk; printf '# ck\n\n- [ ] 项一\n' > "$T/p/checklist.md"
  printf '# spec\n\nchecklist 产出并执行\n' > "$T/s.md"
  printf '§X 某方 某理由\n' > "$T/exc.txt"
  CKDIR="$T/p" SPECDOC="$T/s.md" EXC="$T/exc.txt" check 2>&1 | grep -q '这不是 PASS' \
    && ok "登记例外后不阻断，但输出明说【这不是 PASS】" || bad "登记例外被印成了通过"
  CKDIR="$T/p" SPECDOC="$T/s.md" EXC="$T/exc.txt" check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "登记例外后 rc=0（不阻断无关提交）" || bad "登记例外仍阻断"
  : > "$T/exc.txt"
  CKDIR="$T/p" SPECDOC="$T/s.md" EXC="$T/exc.txt" check >/dev/null 2>&1
  [ $? -eq 1 ] && ok "拦住：例外清单为空则恢复阻断（不许空文件当免检）" || bad "空例外清单被当免检"

  mk; rm -f "$T/p"/*.md
  run; [ $? -eq 2 ] && ok "拦住：无 checklist 件判闸坏（非免检）" || bad "无件被当免检"

  CKDIR="$T/nosuch" SPECDOC="$T/s.md" check >/dev/null 2>&1
  [ $? -eq 2 ] && ok "拦住：目录缺失判闸坏（非免检）" || bad "目录缺失被当免检"

  rm -rf "$T"
  [ $fail -eq 0 ] && echo "--- 反测 $n 项全过（闸有牙）" || echo "--- 反测未过（闸没牙，不得依赖它的 ✓）"
  return $fail
}

case "${1:-}" in --selftest) selftest; exit $? ;; esac
check; exit $?
