#!/bin/sh
# 提交闸 ①–㉘ —— 正文 lint / 语料 selftest / 变异体 / 原型 bump / 状态源同步+锚可达 / 架构引用核验 / 仓储包边界 / specs 索引 / 状态一致性 / 租户隔离前置。任一不过即失败。
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
# 若本次暂存了状态源，则必须与生成器的【整件】输出逐字节一致。
# 干净检出不做此比较：本件的元信息按设计滞后一个提交；但一旦本次声称在更新它，就不得手改内容列绕过生成器。
if git diff --cached --name-only 2>/dev/null | grep -qx docs/DELIVERY-STATUS.md; then
  STATUS_EXPECTED=$(mktemp)
  scripts/gen-status.sh > "$STATUS_EXPECTED" || { rm -f "$STATUS_EXPECTED"; echo "✗ 状态源整件生成失败"; exit 1; }
  if ! cmp -s "$STATUS_EXPECTED" docs/DELIVERY-STATUS.md; then
    rm -f "$STATUS_EXPECTED"
    echo "✗ DELIVERY-STATUS.md 已暂存但不是 gen-status.sh 的逐字节产物；禁止手改凑列"
    exit 1
  fi
  rm -f "$STATUS_EXPECTED"
  echo "  ⑤b DELIVERY-STATUS.md 暂存件与生成器整件逐字节一致 ✓"
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
if [ -f docs/dingwei-platform-architecture.md ]; then
  scripts/arch-refcheck.sh >/dev/null 2>&1 || { scripts/arch-refcheck.sh; echo "✗ 架构引用核验未过"; exit 1; }
  # 闸自己也要过反测 —— 否则「核验通过」可能只是它测不出来（甲域最终复核抓出的那种）
  scripts/arch-refcheck.sh --selftest >/dev/null 2>&1 || { scripts/arch-refcheck.sh --selftest; echo "✗ 架构引用核验的反测未过"; exit 1; }
  echo "  ⑥ 架构引用核验 ✓（含四项反测）"
fi
# ⑥a 现行原型设计门：原型不再只作他包的冻结输入，须以目标件身份核导航、状态源与失败/空态。
if [ ! -x specs/prototype/check.sh ]; then
  echo "✗ 闸依赖缺失：specs/prototype/check.sh 不可执行"; exit 1
fi
specs/prototype/check.sh || exit 1
specs/prototype/check.sh --selftest >/dev/null 2>&1 || {
  specs/prototype/check.sh --selftest; echo "✗ 现行原型设计门的反测未过"; exit 1; }
echo "  ⑥a 现行原型设计门的反测 ✓"
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
# ⑯ RV 标注完整性(丙域 kimi R2 轮3):标注是 reviewer-owned 的证据,不得被清理工具改写
if [ ! -x scripts/rv-integrity-gate.sh ]; then
  echo "✗ 闸依赖缺失：scripts/rv-integrity-gate.sh 不可执行"; exit 1
fi
scripts/rv-integrity-gate.sh || exit 1
scripts/rv-integrity-gate.sh --selftest >/dev/null 2>&1 || {
  scripts/rv-integrity-gate.sh --selftest; echo "✗ RV 完整性闸的反测未过"; exit 1; }
echo "  ⑯ RV 完整性闸的反测 ✓"

# ⑯a 防复发件：丙域要的是「检测 ＋ 作用域排除」两条，⑯ 只实现了检测。
#    批量替换须走 specs/tools/safe-replace.py（遮蔽 RV 段）；此处只保证它【装着且有牙】——
#    工具在仓里不等于有人用它，但反测不过的工具连被用的资格都没有。
if [ ! -x specs/tools/safe-replace.py ]; then
  echo "✗ 闸依赖缺失：specs/tools/safe-replace.py 不可执行（RV 段免疫替换件）"; exit 1
fi
python3 specs/tools/safe-replace.py --selftest >/dev/null 2>&1 || {
  python3 specs/tools/safe-replace.py --selftest; echo "✗ RV 段免疫替换件的反测未过"; exit 1; }
echo "  ⑯a RV 段免疫替换件的反测 ✓（批量清理一律走该件，不得直接 sed）"

# ⑰ 跨制品一致性(analyze)——🔴 立此项的直接原因(丙域 kimi R2 轮4·RV-丙17):
#    analyze.sh 此前【不在提交闸内】,于是它的 ④b 可以整轮 FAIL,
#    而每次提交照常打印「①–⑯a 全过」。执笔方甚至把该误报「修」到了另一个闸上,
#    因为提交闸从来没告诉过他是哪个闸在响。
#    ⚠️ 闸外的检查不是检查,是一个需要有人记得去跑的建议。
if [ ! -f specs/tools/analyze.sh ]; then
  echo "✗ 闸依赖缺失：specs/tools/analyze.sh 不存在"; exit 1
fi
sh specs/tools/analyze.sh >/dev/null 2>&1 || {
  sh specs/tools/analyze.sh; echo "✗ ⑰ 跨制品一致性检查未过（analyze）"; exit 1; }
python3 specs/tools/foreign-version-check.py --selftest >/dev/null 2>&1 || {
  python3 specs/tools/foreign-version-check.py --selftest
  echo "✗ ④b 他件旧版本号检查的反测未过"; exit 1; }
echo "  ⑰ 跨制品一致性 ✓（含 ④b 反测；语义层不在其内，见 analyze 头注）"

# ⑲ 冲突标记恒零（甲域 sol R2 轮4 · RV-甲40）：
#    原型导出函数里提交了未解决的合并冲突，而 ①–⑰ 与 analyze 全绿 ——
#    标记恰好落在 JS 块注释内，语法不坏，于是连「还能不能跑」都没暴露它。
#    🔴 「全闸通过」在那里的含义是【没有一道闸看这个】。
if [ ! -x scripts/conflict-marker-gate.sh ]; then
  echo "✗ 闸依赖缺失：scripts/conflict-marker-gate.sh 不可执行"; exit 1
fi
scripts/conflict-marker-gate.sh || exit 1
scripts/conflict-marker-gate.sh --selftest >/dev/null 2>&1 || {
  scripts/conflict-marker-gate.sh --selftest; echo "✗ 冲突标记闸的反测未过"; exit 1; }
echo "  ⑲ 冲突标记闸的反测 ✓"

# ⑳ 路径锚解析（甲域 sol R2 轮5 · RV-甲44）：
#    #36b 的 reviewer owner 锚被写成一个仓内不存在的路径，人工判据的落点
#    因此指向空文件，而 ①–⑲ 全绿。⚠️ 交代责任落点的引用最需要能被打开。
if [ ! -x scripts/path-anchor-gate.sh ]; then
  echo "✗ 闸依赖缺失：scripts/path-anchor-gate.sh 不可执行"; exit 1
fi
scripts/path-anchor-gate.sh || exit 1
scripts/path-anchor-gate.sh --selftest >/dev/null 2>&1 || {
  scripts/path-anchor-gate.sh --selftest; echo "✗ 路径锚闸的反测未过"; exit 1; }
echo "  ⑳ 路径锚闸的反测 ✓"

# ㉑ 旧模型名棘轮（甲域 sol delta ②）：
#    §21.1c 写着「迁移期不得新增 TenantCtx」，而执笔方称该规矩由 ⑦ 执行 —— **那句是假的**：
#    ⑦ 按效果只统计 PendingTenantCtx 造出的上下文债，不看一般的 TenantCtx 出现。
#    ⚠️ 规矩没有执行者就等于没写。本项即那个执行者。
if [ ! -x scripts/legacy-name-gate.sh ]; then
  echo "✗ 闸依赖缺失：scripts/legacy-name-gate.sh 不可执行"; exit 1
fi
scripts/legacy-name-gate.sh || exit 1
scripts/legacy-name-gate.sh --selftest >/dev/null 2>&1 || {
  scripts/legacy-name-gate.sh --selftest; echo "✗ 旧模型名棘轮的反测未过"; exit 1; }
echo "  ㉑ 旧模型名棘轮的反测 ✓"

# ㉒ 评审状态生成件（docs/REVIEW-STATE.md）须与仓内实况同步。
#    🔴 立项原因：评审转述与推锚已交叉**五次**，每次都是门方按较早的锚复核、
#    报回的缺口在更新的锚上已修。根子不是谁疏忽 —— **我推锚快于转述，而状态只活在消息里**。
#    ⇒ 状态落进仓：门方 checkout 任一锚即可读到该锚上各条 RV 的处置，不必与消息比对。
#    ⚠️ 本件说的是【执笔方处置到哪一步】，**不是【门方是否认可】**。
if [ ! -x scripts/gen-review-state.sh ]; then
  echo "✗ 闸依赖缺失：scripts/gen-review-state.sh 不可执行"; exit 1
fi
scripts/gen-review-state.sh > /tmp/.rvstate.$$ 2>/dev/null
if ! diff -q /tmp/.rvstate.$$ docs/REVIEW-STATE.md >/dev/null 2>&1; then
  echo "✗ docs/REVIEW-STATE.md 与实况不符 —— 重跑 scripts/gen-review-state.sh > docs/REVIEW-STATE.md"
  diff /tmp/.rvstate.$$ docs/REVIEW-STATE.md | head -6
  rm -f /tmp/.rvstate.$$; exit 1
fi
rm -f /tmp/.rvstate.$$
echo "  ㉒ 评审状态件与实况一致 ✓（门方可直接读 docs/REVIEW-STATE.md 判断本锚状态）"

# ㉓ 门方复现台账（归口方第九次交叉后立）：
#    执笔方报「已修」时给的一行判据，多次**证明的是上一轮修复还在**而非这一轮的洞已修
#    （归口原话：「只是证明还是 Go AST 版，没证明 Walk 错误/子串计数已修」）。
#    ⇒ 判据改由门方给：**他问什么，闸就跑什么**。
if [ ! -x scripts/reviewer-repro-gate.sh ]; then
  echo "✗ 闸依赖缺失：scripts/reviewer-repro-gate.sh 不可执行"; exit 1
fi
scripts/reviewer-repro-gate.sh || exit 1
scripts/reviewer-repro-gate.sh --selftest >/dev/null 2>&1 || {
  scripts/reviewer-repro-gate.sh --selftest; echo "✗ 门方复现闸的反测未过"; exit 1; }
echo "  ㉓ 门方复现台账的反测 ✓"

# ㉔ 标题/版本字段一致（交付前实取三件版本时发现架构件标题 v0.33 vs 字段 v0.34）：
#    闸 ① 的 hdr-state **只覆盖 SpecDoc**，架构件与原型不在其判据面内
#    ⇒ 该不一致一路带到交付前才被人工发现。属「**断言作用域小于实际需要**」。
if [ ! -x scripts/title-version-gate.sh ]; then
  echo "✗ 闸依赖缺失：scripts/title-version-gate.sh 不可执行"; exit 1
fi
scripts/title-version-gate.sh || exit 1
scripts/title-version-gate.sh --selftest >/dev/null 2>&1 || {
  scripts/title-version-gate.sh --selftest; echo "✗ 标题/版本一致闸的反测未过"; exit 1; }
echo "  ㉔ 标题/版本一致闸的反测 ✓"

# ㉕ 输入锚解析闸（§4.7.1 ⑥）：锚解析不到 ⇒「范围不扩 / 共识不变」两条默认约束落空，
#    而六面都填了、正文看着合规。🔴 本闸判【锚是否解析得到】，判不了【陈述追不追得到锚】（E.4 #66）。
scripts/input-anchor-gate.sh || exit 1
scripts/input-anchor-gate.sh --selftest >/dev/null 2>&1 || {
  scripts/input-anchor-gate.sh --selftest; echo "✗ 输入锚解析闸的反测未过"; exit 1; }
echo "  ㉕ 输入锚解析闸的反测 ✓"

# ㉖ 依赖清单棘轮（§4.7.1a「依赖不增」的对应物）：不声明即生效的约束若无可机器验证的对应物，
#    就退化成「记得少加依赖」——那是纪律不是机制。🔴 判数不判换，等价替换只提示不判门。
scripts/dependency-ratchet.sh || exit 1
scripts/dependency-ratchet.sh --selftest >/dev/null 2>&1 || {
  scripts/dependency-ratchet.sh --selftest; echo "✗ 依赖清单棘轮的反测未过"; exit 1; }
echo "  ㉖ 依赖清单棘轮的反测 ✓"

# ㉗ workflow 触发面覆盖闸：CI 的 paths 必须覆盖【闸的载体本身】。
#    立闸原因（ops 2026-08-26 实测）：触发面曾只有 docs/**，而闸脚本在 scripts/、基线在 specs/gates/
#    ⇒ 闸的载体不在自己的触发面内，改动它们时 CI 一声不响。
#    🔴 本闸过 ≠ 仓侧不可逆保护已落地 —— 触发面对了只是「红得出来」，不是「挡得住」。
scripts/workflow-coverage-gate.sh || exit 1
scripts/workflow-coverage-gate.sh --selftest >/dev/null 2>&1 || {
  scripts/workflow-coverage-gate.sh --selftest; echo "✗ 触发面覆盖闸的反测未过"; exit 1; }
echo "  ㉗ 触发面覆盖闸的反测 ✓"

# ㉘ §18.1 列集一致闸：列清单须机器可唯一提取，且【清单块 / 表格 / 原型自述】三处一致。
#    立闸原因（coordinator 实测）：同一节三种抽取法得 19/19/21，原型另述一个数
#    ⇒ 「以 §18.1 为准」无处落，下游各取一份。问题不是哪个数对，是判据与被判物不同源。
#    🔴 本闸判【三处是否一致】，判不了【列定义对不对】——那是设计判断，归门方。
scripts/post-columns-gate.sh || exit 1
scripts/post-columns-gate.sh --selftest >/dev/null 2>&1 || {
  scripts/post-columns-gate.sh --selftest; echo "✗ 列集一致闸的反测未过"; exit 1; }
echo "  ㉘ 列集一致闸的反测 ✓"

# ㉙ 覆盖见证元闸 —— 「每闸须给覆盖见证：预期集合 · 实检集合 · 未知集合」（乙域跨闸硬契约第 4 条），
#    fail-closed **棘轮**模式（乙域 2026-08-26：不接受当前 18 项债务，也不让全仓一次性停摆）。
#
# 🔴 接入范围由 coordinator 裁（2026-08-26）：
#    「元闸㉙ ＝ 接入侧分支 fix 的提交闸/CI 链路；**不接主评审面硬提交闸**，主闸接入等并锚后再定。」
#
# ⚠️ 实现方式写死在此，防止被读成更宽或更窄：
#    · **一律实跑、一律打印**，只有【是否阻断】随分支不同；
#    · 🔴 **不做成「别的分支上根本不跑」** —— 那会让它悄悄变成一个空集：
#      **一个不跑的闸和一个不存在的闸，在输出里长得一模一样。**
#      （本轮反复出现的那一族：没查 / 没触发 / 没登记，都会印成一片安静。）
#    · 非侧分支上打印「只报不拦」时**必须同时写明这不是通过**，否则读者会把它当成绿的。
_WGATE_BRANCH=${WGATE_ENFORCE_BRANCH:-fix/e-registry-and-18-1}
if [ ! -x scripts/gate-witness-gate.sh ]; then
  echo "✗ 闸依赖缺失：scripts/gate-witness-gate.sh 不可执行"; exit 1
fi
scripts/gate-witness-gate.sh --selftest >/dev/null 2>&1 || {
  scripts/gate-witness-gate.sh --selftest; echo "✗ ㉙ 覆盖见证元闸的反测未过"; exit 1; }
_wout=$(scripts/gate-witness-gate.sh 2>&1); _wrc=$?
printf '%s\n' "$_wout"
_wdebt=$(printf '%s\n' "$_wout" | sed -n 's/.*WITNESS_DEBT_COUNT=\([0-9]*\).*/\1/p' | head -1)
case "$_wdebt" in ''|*[!0-9]*) _wdebt=0 ;; esac
echo "  ㉙ 覆盖见证元闸的反测 ✓"
_wbr=$(git rev-parse --abbrev-ref HEAD 2>/dev/null) || _wbr=''
[ -n "$_wbr" ] || _wbr='(取不到分支名)'
if [ "$_wbr" = "$_WGATE_BRANCH" ]; then
  echo "  ㉙ 接入态：**阻断**（当前分支 $_wbr ＝ 侧分支，coordinator 已裁本分支接入提交闸/CI）"
  [ "$_wrc" -ne 0 ] && { echo "✗ ㉙ 在侧分支阻断（rc=$_wrc）—— 按棘轮四条：缺见证增加 / 合规退化 / 新增闸无见证 / 缺 owner 或到期"; exit "$_wrc"; }
else
  echo "  ㉙ 接入态：**只报不拦**（当前分支 $_wbr ≠ 侧分支 $_WGATE_BRANCH；本次 rc=$_wrc）"
  echo "     🔴 **这不是「通过」** —— 主评审面的硬接入等并锚后由 coordinator 再定；"
  echo "     ⚠️ 取不到分支名时也走本支路（fail-open 仅限阻断决定，检查本身照跑照印）。"
  # 🔴 只报不拦时若 ㉙ 是红的，**末行的「全过」必须改口** ——
  #   否则就出现本轮反复治的那个形态：闸红着，而摘要行印着 ✓。
  #   ⚠️ 这个洞是我把「只报不拦」接上去之后【实跑非侧分支】才看见的：
  #     rc=1 的 ㉙ 后面照样跟着一行「提交闸全过」。**接入方式本身也要被反测。**
  [ "$_wrc" -ne 0 ] && _W_ADVISORY_RED=$_wrc
fi
_W_ADVISORY_RED=${_W_ADVISORY_RED:-0}
_wsuffix=''
[ "$_W_ADVISORY_RED" -ne 0 ] && _wsuffix="　⚠️ 🔴 但 ㉙ 本次为红（rc=$_W_ADVISORY_RED，只报不拦）—— **㉙ 不在「全过」之列**"
# 🔴 乙域收尾裁定：债务**不得被聚合为合规 PASS**。
#   ⇒ ㉙ 带未清债务时，末行必须写出来 —— 否则「全过」这三个字就把 15 项债务吞掉了。
#   ⚠️ 这正是本轮反复治的形态：**摘要行比它下面那些行说得宽**。
[ "${_wdebt:-0}" -gt 0 ] && _wsuffix="${_wsuffix}　⚠️ 🔴 ㉙ 另带 ${_wdebt} 项未清覆盖见证债务（棘轮未回升，但**不计入合规**）"

# ⑱ 处置兑现 triage —— 🔴 **本项不判门**，只把「标了采纳 vs 正文真改了」的可疑处印出来。
#    立项原因：R2 轮3–轮4 三域独立抓到四次「标采纳而正文未动／改在了别处」，
#    **没有一次是执笔方自查出来的**。
#    ⚠️ 误报率高（首跑 4 命中 3 误报），做成硬闸只会逼人绕过它 ——
#      与 ④b 头注同一条教训：宁可收窄，也不要一个没人信的闸。
#    ⚠️ 但它也不能只存在于仓里没人跑（见 ⑰ 的立项原因：闸外的检查不是检查）⇒ 每次提交必印。
python3 specs/tools/disposition-audit.py --selftest >/dev/null 2>&1 || {
  python3 specs/tools/disposition-audit.py --selftest; echo "✗ ⑱ 处置兑现 triage 的反测未过（它连响都不会响）"; exit 1; }
python3 specs/tools/disposition-audit.py || true
echo "✓ 提交闸全过（①–㉙ ＋ ⑱ triage）${_wsuffix}"
