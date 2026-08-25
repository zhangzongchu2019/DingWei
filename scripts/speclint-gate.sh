#!/bin/sh
# 提交闸 ①–⑩ —— 正文 lint / 语料 selftest / 变异体 / 原型 bump / 状态源同步+锚可达 / 架构引用核验 / 仓储包边界 / specs 索引 / 状态一致性 / 租户隔离前置。任一不过即失败。
#
# 立此闸的直接原因：2026-08-24，本席在 speclint 退出码为 1 的状态下推了 a165521。
# 当时 lint 与 commit 写在同一批命令里、没有用 && 串，照跑不误。
# 纪律靠记性，机制靠这个脚本 —— 这也是 §29「不靠纪律靠机制」对本仓自身的应用。
#
# 用法：scripts/speclint-gate.sh        （仓根执行）
# 装成本机钩子：ln -sf ../../scripts/speclint-gate.sh .git/hooks/pre-commit
set -u
D=docs
T=$D/tools
DOC=$D/dingwei-platform-specdoc.md
COR=$T/speclint-corpus.md
LINT=$T/speclint.py

# 🔴 T-1（丙域 kimi 终审）：工具缺席【不是通过】。
#    原写 exit 0 —— speclint.py 一旦不在，三道门直接报「通过」并放行提交。
#    这正是本轮刚归纳的第六种形态「没测却报通过」，而它就在本席自己的闸里。
#    ⇒ 闸的依赖缺失是【闸坏了】，不是【本次不用检】。
[ -f "$LINT" ] || { echo "✗ 闸依赖缺失：$LINT 不存在 —— 这是闸坏了，不是本次免检"; exit 1; }
[ -f "$COR" ]  || { echo "✗ 闸依赖缺失：$COR 不存在 —— 无语料即无法证明规则有牙"; exit 1; }

# ⓪ 自检：钩子必须【就是本脚本】，不能是它的一份旧拷贝。
#    立此判据的直接原因：本脚本写好后，.git/hooks/pre-commit 仍是更早的独立拷贝（754 字节），
#    于是新增的第 ④⑤ 两项【根本没执行】，而每次提交都照常打印「✓ 三道门全过」——
#    这是「定义了却没装上」的第三次，也是最阴的一次：闸报了通过，只是它不是你以为的那道闸。
HOOK=.git/hooks/pre-commit
if [ -e "$HOOK" ] && [ ! -L "$HOOK" ]; then
  echo "✗ .git/hooks/pre-commit 是拷贝不是软链 —— 它可能已落后于 scripts/speclint-gate.sh"
  echo "  修：make speclint-hook"
  exit 1
fi
command -v python3 >/dev/null 2>&1 || { echo "✗ 缺 python3"; exit 1; }

FAIL=0
echo "① 正文 lint";       python3 "$LINT" "$DOC"            || FAIL=1
echo "② 语料 selftest";   python3 "$LINT" --selftest "$COR" || FAIL=1
echo "③ 变异体";          python3 "$LINT" --mutate   "$COR" || FAIL=1

if [ "$FAIL" -ne 0 ]; then
  echo "✗ ①–③ 未过。修完再提交；确需绕过用 git commit --no-verify，"
  echo "  且须在提交信息里写明绕过理由 —— 堵死绕行不如让绕行留痕。"
  exit 1
fi
# ④ 原型改了就必须 bump 件内自述版本。
#    立此判据的直接原因：delivery-check.sh 本是为「件内版本 vs 文件名版本不符」而写，
#    可它跑在【投递时】—— 那时错已进仓。同一个错隔一个 commit 就复发了。
#    ⇒ 闸要装在错能发生的地方，不是错被发现的地方。
PROTO=docs/dingwei-prototype.html
if git diff --cached --name-only 2>/dev/null | grep -qx "$PROTO"; then
  NEW=$(grep -o "原型 v[0-9]*\.[0-9]*" "$PROTO" | head -1)
  OLD=$(git show HEAD:"$PROTO" 2>/dev/null | grep -o "原型 v[0-9]*\.[0-9]*" | head -1)
  if [ -z "$OLD" ]; then
    # HEAD 里取不到旧值（新增文件 / 刚改过路径）——此时【无法比对】，须明说，不得打印 ✓。
    # 立此分支的直接原因：改名后 git show HEAD:<新路径> 取不到值，判据被跳过却仍打印「✓」，
    # 与「检测到了但没接进失败条件」同族 —— 闸不能在自己没测的时候报通过。
    echo "  ④ 原型版本 —— ⚠️ HEAD 无该路径旧值（新增或刚改名），本次【跳过比对】，非通过"
  elif [ "$NEW" = "$OLD" ]; then
    echo "✗ 原型内容已改，但件内自述版本仍为 $NEW —— 请 bump 后再提交"
    echo "  （评审方打开页面看到的是标题里的版本；不 bump 等于告诉他这是旧件）"
    exit 1
  else
    echo "  ④ 原型版本 $OLD → $NEW ✓"
  fi
fi
# ⑤ DELIVERY-STATUS.md 须与 HEAD 同步（它是单一状态源，过期即失去意义）
if [ ! -x scripts/gen-status.sh ]; then
  echo "✗ 闸依赖缺失：scripts/gen-status.sh 不可执行 —— 状态源无法刷新，不得放行"; exit 1
fi
# 🔴 只比【内容列】，不比整份文件（复核方实测 + coordinator 归口记账）：
#    本文件在 pre-commit 生成,那时本次提交尚不存在,故顶部「生成自 commit」、尾部 git log 列表、
#    「本次提交中」标注三者天然与检出后不同 ⇒ 原来「整份 diff」的写法会让
#    **任何人在干净检出上跑一次闸,这个文件就变脏**,复核方以为自己动过。
#    假信号是单一事实源最贵的成本:它让人不敢信这份文件,也让真变化淹没在噪声里。
#    ⇒ 内容没变就【不碰这个文件】;内容一变,易变的元信息随之一并更新。
if true; then
  NEWC=$(scripts/gen-status.sh --stable 2>/dev/null || true)
  # 从仓内现文件里抽同样的内容列（件|版本|行 / 字节|sha256），锚列不参与比较
  OLDC=$(sed -n 's/^| `\([^`]*\)` | \*\*\([^*]*\)\*\* | .* | \([0-9]* \/ [0-9]*\) | `\([0-9a-f]*\)` |$/\1|\2|\3|\4/p' \
           docs/DELIVERY-STATUS.md 2>/dev/null || true)
  if [ -z "$NEWC" ]; then
    # 取不到就是【无法比对】,须明说,不得打印 ✓（与 ④ 同一条理由）
    echo "  ⑤ ⚠️ 状态源内容列生成为空,本次【跳过比对】,非通过"
  elif [ "$NEWC" != "$OLDC" ]; then
    scripts/gen-status.sh > docs/DELIVERY-STATUS.md
    git add docs/DELIVERY-STATUS.md 2>/dev/null || true
    echo "  ⑤ DELIVERY-STATUS.md 内容列已变,已重生成并入暂存区"
  else
    echo "  ⑤ DELIVERY-STATUS.md 内容列一致 ✓（元信息滞后一格属设计内,不据此重写）"
  fi
fi
# ⑤a 状态源钉的锚必须【在分支历史里】。
#    立此判据的直接原因：2026-08-25，本席为修一句提交信里的编号口径对已过闸的提交做了 --amend。
#    amend 时 HEAD 仍是【被替换的那个提交】，pre-commit 把它写进了状态源，随即它被 amend 抛弃 ——
#    于是 origin 上的单一事实源钉着一个复核方 clone 后根本不存在的坐标。
#    ⚠️ 为修一个小瑕疵，在【单一事实源】里制造了一个幽灵坐标 —— 代价远大于所修。
STATUS=docs/DELIVERY-STATUS.md
anchor_of() { sed -n 's/^生成自 commit `\([0-9a-f]\{40\}\)`.*/\1/p' | head -1; }
PREV_ANCHOR=$(git show HEAD:"$STATUS" 2>/dev/null | anchor_of)
if [ -z "$PREV_ANCHOR" ]; then
  # 取不到就是【无法比对】，须明说，不得打印 ✓（与 ④ 的同名分支同一条理由）
  echo "  ⑤a ⚠️ HEAD 的状态源取不到锚，本次【跳过比对】，非通过"
elif git merge-base --is-ancestor "$PREV_ANCHOR" HEAD 2>/dev/null; then
  echo "  ⑤a 状态源锚在分支历史内 ✓"
else
  echo "🔴 ⑤a HEAD 的状态源钉着一个【不在分支历史里】的锚：$PREV_ANCHOR"
  echo "     成因几乎总是对已过闸的提交做了 --amend。此后请勿 amend 已过闸的提交；"
  echo "     要改提交信，另起一个提交说明，或 --no-verify 后手工重生成状态源。"
  # 有牙的部分：本次提交必须【确实在修】它，否则拒绝放行。
  STAGED_ANCHOR=$(git show :"$STATUS" 2>/dev/null | anchor_of)
  if [ "$STAGED_ANCHOR" = "$(git rev-parse HEAD)" ]; then
    echo "     ✓ 本次提交已把状态源修回当前 HEAD，放行这一次。"
  else
    echo "✗ 本次提交并未修复该锚（暂存区记的是 ${STAGED_ANCHOR:-空}），拒绝放行"; exit 1
  fi
fi
# ⑥ 架构设计的引用核验（来源类型化解析）
if [ ! -x scripts/arch-refcheck.sh ]; then
  echo "✗ 闸依赖缺失：scripts/arch-refcheck.sh 不可执行"; exit 1
fi
if [ -f docs/dingwei-architecture-design.md ]; then
  scripts/arch-refcheck.sh >/dev/null 2>&1 || { scripts/arch-refcheck.sh; echo "✗ 架构引用核验未过"; exit 1; }
  # 闸自己也要过反测 —— 否则「核验通过」可能只是它测不出来（甲域最终复核抓出的那种）
  scripts/arch-refcheck.sh --selftest >/dev/null 2>&1 || { scripts/arch-refcheck.sh --selftest; echo "✗ 架构引用核验的反测未过"; exit 1; }
  echo "  ⑥ 架构引用核验 ✓（含四项反测）"
fi
# ⑦ 仓储包边界（SP7a 甲组·收口判据的执行处）
#    立此项的直接原因：判据「仓储包外 sql.Open 恒零」此前只写在架构设计与证据脚本里，
#    没有任何闸执行它 —— specs/evidence/collect.sh 只记录数字、整篇无非零退出，本闸也没调过它。
#    ⇒ 判据不接进失败条件，等于没有判据。
if [ ! -x scripts/repo-boundary-gate.sh ]; then
  echo "✗ 闸依赖缺失：scripts/repo-boundary-gate.sh 不可执行 —— 收口判据无人执行，不得放行"; exit 1
fi
scripts/repo-boundary-gate.sh || exit 1
# 闸自己也要过反测 —— 否则它的 ✓ 可能只是它测不出来。
scripts/repo-boundary-gate.sh --selftest >/dev/null 2>&1 || {
  scripts/repo-boundary-gate.sh --selftest; echo "✗ 仓储包边界闸的反测未过"; exit 1; }
echo "  ⑦ 仓储包边界闸的反测 ✓"
# ⑧ specs/ 索引与实际一致（README 的结构表不许再靠人手维护）
if [ ! -x scripts/specs-index-check.sh ]; then
  echo "✗ 闸依赖缺失：scripts/specs-index-check.sh 不可执行"; exit 1
fi
scripts/specs-index-check.sh || exit 1
scripts/specs-index-check.sh --selftest >/dev/null 2>&1 || {
  scripts/specs-index-check.sh --selftest; echo "✗ specs 索引闸的反测未过"; exit 1; }
echo "  ⑧ specs 索引闸的反测 ✓"
# ⑨ 状态一致性:supporting spec 不得留与权威状态源矛盾的现状断言(甲域 sol 抓出)
if [ ! -x scripts/status-consistency-check.sh ]; then
  echo "✗ 闸依赖缺失：scripts/status-consistency-check.sh 不可执行"; exit 1
fi
scripts/status-consistency-check.sh || exit 1
scripts/status-consistency-check.sh --selftest >/dev/null 2>&1 || {
  scripts/status-consistency-check.sh --selftest; echo "✗ 状态一致性闸的反测未过"; exit 1; }
echo "  ⑨ 状态一致性闸的反测 ✓"
# ⑩ 乙组隔离判据的前置依赖(乙域 terra 裁定):领域表一旦出现租户列,四项负测须同时就位
if [ ! -x scripts/tenant-isolation-gate.sh ]; then
  echo "✗ 闸依赖缺失：scripts/tenant-isolation-gate.sh 不可执行"; exit 1
fi
scripts/tenant-isolation-gate.sh || exit 1
scripts/tenant-isolation-gate.sh --selftest >/dev/null 2>&1 || {
  scripts/tenant-isolation-gate.sh --selftest; echo "✗ 租户隔离前置闸的反测未过"; exit 1; }
echo "  ⑩ 租户隔离前置闸的反测 ✓"
# ⑪ checklist 执行落账(甲域 sol):宣称「已按 checklist 执行」时,机器要求 checkbox 已勾且证据非空
if [ ! -x scripts/checklist-executed-gate.sh ]; then
  echo "✗ 闸依赖缺失：scripts/checklist-executed-gate.sh 不可执行"; exit 1
fi
scripts/checklist-executed-gate.sh || exit 1
scripts/checklist-executed-gate.sh --selftest >/dev/null 2>&1 || {
  scripts/checklist-executed-gate.sh --selftest; echo "✗ checklist 落账闸的反测未过"; exit 1; }
echo "  ⑪ checklist 落账闸的反测 ✓"
# ⑫ 体例合规(R1 执笔方 G-1 / 丙域 F-7):正文不得含评审过程痕迹;RV 标注字段须合法
if [ ! -x scripts/prose-style-gate.sh ]; then
  echo "✗ 闸依赖缺失：scripts/prose-style-gate.sh 不可执行"; exit 1
fi
scripts/prose-style-gate.sh || exit 1
scripts/prose-style-gate.sh --rv || exit 1
scripts/prose-style-gate.sh --selftest >/dev/null 2>&1 || {
  scripts/prose-style-gate.sh --selftest; echo "✗ 体例合规闸的反测未过"; exit 1; }
echo "  ⑫ 体例合规闸的反测 ✓"
# ⑬ 已撤销断言(R2 轮1,三域各自独立标出同一处):裁定改了,引用点必须跟着改
if [ ! -x scripts/superseded-claims-gate.sh ]; then
  echo "✗ 闸依赖缺失：scripts/superseded-claims-gate.sh 不可执行"; exit 1
fi
scripts/superseded-claims-gate.sh || exit 1
scripts/superseded-claims-gate.sh --selftest >/dev/null 2>&1 || {
  scripts/superseded-claims-gate.sh --selftest; echo "✗ 已撤销断言闸的反测未过"; exit 1; }
echo "  ⑬ 已撤销断言闸的反测 ✓"
# ⑭ 证据包时效(甲域 sol):证据必须产自被评审的那个锚
if [ ! -x scripts/evidence-fresh-gate.sh ]; then
  echo "✗ 闸依赖缺失：scripts/evidence-fresh-gate.sh 不可执行"; exit 1
fi
scripts/evidence-fresh-gate.sh || exit 1
scripts/evidence-fresh-gate.sh --selftest >/dev/null 2>&1 || {
  scripts/evidence-fresh-gate.sh --selftest; echo "✗ 证据包时效闸的反测未过"; exit 1; }
echo "  ⑭ 证据包时效闸的反测 ✓"
# ⑮ 附录 E 认领(甲域 sol · 乙域 terra 同指):恒零判据须有数据承载
if [ ! -x scripts/appendix-e-owner-gate.sh ]; then
  echo "✗ 闸依赖缺失：scripts/appendix-e-owner-gate.sh 不可执行"; exit 1
fi
scripts/appendix-e-owner-gate.sh || exit 1
scripts/appendix-e-owner-gate.sh --selftest >/dev/null 2>&1 || {
  scripts/appendix-e-owner-gate.sh --selftest; echo "✗ 附录 E 认领闸的反测未过"; exit 1; }
echo "  ⑮ 附录 E 认领闸的反测 ✓"
_debt=$(sed 's/#.*//' specs/gates/status-stale-exceptions.txt 2>/dev/null | awk 'NF>0' | wc -l | tr -d ' ')
if [ "${_debt:-0}" -gt 0 ]; then
  echo "✓ 提交闸通过（①–⑮）⚠️ PASS WITH REGISTERED DEBT：状态例外 ${_debt} 条登记在 specs/gates/status-stale-exceptions.txt —— 登记是债务可见化，不是修复；例外的新增与批准属 owner/门方评审面（与 repo allowlist 同形：登记≠获批准）。"
else
  echo "✓ 提交闸全过（①–⑮）"
fi
