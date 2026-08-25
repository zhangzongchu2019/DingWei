#!/bin/sh
# 证据包时效闸 —— 证据必须产自被评审的那个锚。
#
# 立此闸的直接原因（甲域 sol，R2 轮1）：
#   R2 评审包里的 `specs/evidence/EVIDENCE.md` 头写「分支头 d0322c6」，
#   而本轮冻结锚是 review-r2 —— 两锚之间改了 detector、Option 收口测试、conformance suite 等评审对象。
#
# 🔴 sol 的区分照录，它比「过期了」准得多：
#   「快照 MANIFEST 只能证明**过期 EVIDENCE 文件本身没被篡改**，
#     **不能证明其内容是冻结锚产出**。」
#   ⇒ 指纹一致 ≠ 内容当期。两个都要，缺一不可。
#
# ⚠️ 能力边界：本闸判「头块所载锚是否为当前 HEAD 的祖先且非落后」，
#   判不了「内容是否真由该锚跑出来」—— 那要重跑 collect.sh 比对，成本高，归门方抽验。
set -u

EV=${EV:-specs/evidence/EVIDENCE.md}

check() {
  [ -f "$EV" ] || { echo "✗ 闸依赖缺失：$EV 不存在 —— 闸坏了，不是免检"; return 2; }
  _a=$(grep -m1 -oE '分支头[^`]*`([0-9a-f]{7,40})`' "$EV" | grep -oE '[0-9a-f]{7,40}')
  [ -n "$_a" ] || { echo "✗ $EV 头块无「分支头」锚 —— 无法判定时效，闸坏了"; return 2; }
  _h=$(git rev-parse HEAD 2>/dev/null) || { echo "✗ 非 git 工作树，闸坏了"; return 2; }
  # 🔴 判据不能是「锚 == HEAD」：本闸在 pre-commit 跑，此刻 HEAD 还是上一个提交，
  #   而证据包是【本次提交中】重生成的 ⇒ 恒不相等，形成死循环。本闸首版即如此，当场卡住自己。
  #   ⇒ 改判「证据锚不落后于**最后一次改动评审对象**的提交」——评审对象＝三件正文 + 闸与工具。
  # 🔴 乙域 terra R2 轮3：本闸在【冻结锚】上实测拒绝 —— 且那是结构性的，不是偶发。
  #   证据包在 pre-commit 生成，那一刻 HEAD 尚是上一个提交 ⇒ 它记的锚
  #   **永远比它所在的那个提交少一个**。而「打锚那次提交」本身通常就改了评审对象，
  #   于是判据「不落后于评审对象末次改动」在**任何冻结锚上恒为 FAIL**。
  #   ⚠️ 执笔方只在 pre-commit 上下文验过它，**没在评审上下文验过** —— 又一次「验了一部分报成全部」。
  #   ⇒ 判据改为：证据锚须为 HEAD **或 HEAD 的直接父**（即「本次提交中生成」这一形态），
  #     且自证据锚起、除【生成证据包的那一个提交】外，评审对象无其他改动。
  _last=$(git log -1 --format=%H -- docs/dingwei-platform-specdoc.md docs/dingwei-architecture-design.md \
            docs/dingwei-prototype.html scripts specs/tools 2>/dev/null)
  # ⚠️ 不用 `rev-list --count`：**合并历史上它会把并入的分支一并计数** —— 实测 merge 后
  #   「证据锚 → 其子提交」的距离变成 2，判据当场失效。改用【第一父链】判相邻，
  #   它才对应「证据在 pre-commit 生成，锚 = 该提交的父」这一真实形态。
  # 🔴 乙域 terra R2 轮4（RV-乙08）：提交距离 ≤1 是**代理判据**，
  #   而落后的那一个提交恰好改了三件正文与分析工具时，代理正好失效 ——
  #   「证据锚」与「被评审的内容」不是同一份东西，闸却放行。
  #   ⇒ 判据换成【内容指纹】：证据包自报它分析的每个文件的 sha256，逐个与现值比。
  #     指纹对得上，就与提交拓扑无关地证明了「这份证据分析的就是这份字节」。
  #   ⚠️ terra 给的两条修法本席都没有直接取，理由写在这里以便复核：
  #     其一「重跑 collect --write 后再冻结」是**循环的** —— collect 在 pre-commit 盖父提交，
  #       提交完成后证据又落后一个，永远追不上自己；
  #     其二「未触及评审对象的 diff 闭包豁免」只在落后那一提交没动评审对象时成立，
  #       而冻结提交通常就是改正文的那一次。
  #     指纹判据同时满足这两条想要的东西，且不依赖提交拓扑。
  _fp=$(grep -c '^REVIEWED ' "$EV" 2>/dev/null || true)
  if [ "${_fp:-0}" -eq 0 ]; then
    echo "✗ $EV 无「评审对象内容指纹」段（REVIEWED 行）—— 无可比对象，闸坏了"
    echo "  ⇒ 重跑 specs/evidence/collect.sh --write（该段由乙域 RV-乙08 引入）"
    return 2
  fi
  _bad=0
  while read -r _kw _want _file; do
    [ "$_kw" = "REVIEWED" ] || continue
    if [ ! -f "$_file" ]; then
      echo "✗ 证据包声称分析过 $_file，而该文件现已不存在"; _bad=$((_bad+1)); continue
    fi
    _got=$(sha256sum "$_file" | cut -d' ' -f1)
    # 🔴 乙域 terra R2 轮5（RV-乙10）：只比【工作区】是不够的 ——
    #   执笔方跑成 `git add -A && collect --write && 提交`：
    #   collect 写的新证据**没进暂存区**，闸看工作区（新的）判通过，
    #   而提交进去的是暂存区里那份**旧证据** ⇒ 冻结锚上再次失效。
    #   ⚠️ 闸测的东西必须是【将要被提交的东西】，不是【我手边这份】。
    #   ⇒ 工作区与暂存区两侧都要对上；任一侧不符即拒。
    # ⚠️ 不能写成 `git show ":$f" | sha256sum`：git 失败时管道里是**空输入**，
    #   sha256sum 照样输出空串的哈希（e3b0c442…），于是「取不到」被当成「取到了一个不同的值」。
    #   ⇒ 先用 rev-parse 判该路径在暂存区里到底有没有。
    # ⚠️ 也不能用 `git rev-parse ":$f"` 判存在：**失败时它把参数原样打回 stdout**，
    #   于是「取不到」又变成一个非空值。用 cat-file -e，它只以退出码回答。
    if git cat-file -e ":$_file" 2>/dev/null; then
      _idx=$(git cat-file blob ":$_file" 2>/dev/null | sha256sum | cut -d' ' -f1)
    else
      _idx=""
    fi
    if [ -n "$_idx" ] && [ "$_idx" != "$_want" ] && [ "$_got" = "$_want" ]; then
      [ "$_bad" -eq 0 ] && echo "✗ 证据包非当期产出：以下文件【暂存区】与证据不符（工作区却是对的）"
      echo "    $_file"
      echo "      证据记 $(echo "$_want"|cut -c1-16)… · 暂存区 $(echo "$_idx"|cut -c1-16)…"
      echo "      ⇒ 典型成因：先 git add 后 collect --write，新证据没进暂存区。"
      _bad=$((_bad+1)); continue
    fi
    if [ "$_got" != "$_want" ]; then
      [ "$_bad" -eq 0 ] && echo "✗ 证据包非当期产出：以下文件在证据生成后又被改过"
      echo "    $_file"
      echo "      证据记 $(echo "$_want"|cut -c1-16)… · 现值 $(echo "$_got"|cut -c1-16)…"
      _bad=$((_bad+1))
    fi
  done < "$EV"
  if [ "$_bad" -gt 0 ]; then
    echo "  🔴 **提交距离 ≤1 判不出这一类** —— 改动就落在那一个提交里（乙域 RV-乙08）。"
    echo "  ⇒ 重跑 specs/evidence/collect.sh --write 后再提交。"
    return 1
  fi
  echo "  ✓ 证据包为当期产出（证据锚 $(echo "$_a"|cut -c1-12)；$_fp 个评审对象指纹逐个比对一致）"
  echo "  ⚠️ 指纹证明【分析的是这些字节】，不证明【分析得对】—— 后者归门方抽验。"
  return 0
}

selftest() {
  T=$(mktemp -d) || return 2
  n=0; fail=0
  ok(){ n=$((n+1)); echo "  ✓ $1"; }; bad(){ echo "  ✗ 反测未过：$1"; fail=1; }
  _h=$(git rev-parse HEAD)
  _f1="$T/obj1.md"; _f2="$T/obj2.md"
  printf 'AAA\n' > "$_f1"; printf 'BBB\n' > "$_f2"
  _mk(){ # 造一份证据包：$1=锚，其余为「被分析对象」
    _anchor=$1; shift
    printf '生成时刻：X\n分支头：`%s`\n' "$_anchor" > "$T/e.md"
    for _x in "$@"; do printf 'REVIEWED %s  %s\n' "$(sha256sum "$_x"|cut -d" " -f1)" "$_x" >> "$T/e.md"; done
  }

  _mk "$_h" "$_f1" "$_f2"
  EV="$T/e.md" check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：全部指纹一致时放行" || bad "当期证据包被误拦"

  # 🔴 RV-乙08 的形态本身：证据生成后评审对象又被改了
  _mk "$_h" "$_f1" "$_f2"; printf 'CHANGED\n' > "$_f2"
  EV="$T/e.md" check >/dev/null 2>&1
  [ $? -eq 1 ] && ok "🔴 拦住：证据生成后评审对象被改（正是乙域 RV-乙08）" || bad "被改过的评审对象放行了"
  printf 'BBB\n' > "$_f2"

  # 🔴 判据须与提交拓扑无关：锚取仓库首个提交（落后极多），指纹一致仍须放行。
  #   旧判据在这里恒 FAIL —— 那正是「提交距离」作为代理判据的失效点。
  _mk "$(git rev-list --max-parents=0 HEAD | head -1)" "$_f1" "$_f2"
  EV="$T/e.md" check >/dev/null 2>&1
  [ $? -eq 0 ] && ok "不误伤：锚落后极多但指纹一致仍放行（判据不再依赖提交拓扑）" \
    || bad "指纹一致却因拓扑被拦 —— 代理判据的残留"

  # 🔴 反向：锚等于 HEAD 但指纹不符，仍须拦 —— 防「锚对了就算数」
  _mk "$_h" "$_f1" "$_f2"; printf 'X\n' > "$_f1"
  EV="$T/e.md" check >/dev/null 2>&1
  [ $? -eq 1 ] && ok "拦住：锚等于 HEAD 但指纹不符（锚对不能替代内容对）" || bad "锚对就放行了"
  printf 'AAA\n' > "$_f1"

  # 🔴 RV-乙10 的形态：工作区对、暂存区旧 —— 闸只看工作区就会放行
  #   （构造：在真仓里取一个 REVIEWED 件，令其暂存区内容与工作区不同）
  _probe=specs/gates/rv-ledger.txt
  if [ -f "$_probe" ] && git rev-parse --git-dir >/dev/null 2>&1; then
    _sv=$(mktemp); cp "$_probe" "$_sv"
    printf '\n# rv-fixture 反测临时行\n' >> "$_probe"; git add "$_probe" 2>/dev/null
    cp "$_sv" "$_probe"                       # 工作区还原，暂存区留着改动
    _mk "$_h" "$_probe"                        # 证据按【工作区】现值记指纹
    EV="$T/e.md" check >/dev/null 2>&1
    [ $? -eq 1 ] && ok "🔴 拦住：工作区对而暂存区旧（先 add 后 collect 的形态，乙域 RV-乙10）" \
      || bad "暂存区与证据不符却放行 —— 闸测的不是将要被提交的东西"
    git add "$_probe" 2>/dev/null; rm -f "$_sv"
  fi

  _mk "$_h" "$_f1"; printf 'REVIEWED %s  %s\n' "$(sha256sum "$_f1"|cut -d" " -f1)" "$T/gone.md" >> "$T/e.md"
  EV="$T/e.md" check >/dev/null 2>&1
  [ $? -eq 1 ] && ok "拦住：证据声称分析过的文件已不存在" || bad "文件消失被放行"

  # 🔴 无指纹段＝无可比对象，须判【闸坏】而不是【通过】
  printf '生成时刻：X\n分支头：`%s`\n' "$_h" > "$T/e.md"
  EV="$T/e.md" check >/dev/null 2>&1
  [ $? -eq 2 ] && ok "拦住：证据包无 REVIEWED 指纹段判闸坏（非免检）" || bad "无指纹段被当通过"

  printf '生成时刻：X\n没有锚\nREVIEWED x  y\n' > "$T/e.md"
  EV="$T/e.md" check >/dev/null 2>&1
  [ $? -eq 2 ] && ok "拦住：头块无锚判闸坏（非免检）" || bad "无锚被当免检"

  EV="$T/nosuch.md" check >/dev/null 2>&1
  [ $? -eq 2 ] && ok "拦住：证据件缺失判闸坏（非免检）" || bad "件缺失被当免检"

  rm -rf "$T"
  [ $fail -eq 0 ] && echo "--- 反测 $n 项全过（闸有牙）" || echo "--- 反测未过（闸没牙）"
  return $fail
}

case "${1:-}" in --selftest) selftest; exit $? ;; esac
check; exit $?
