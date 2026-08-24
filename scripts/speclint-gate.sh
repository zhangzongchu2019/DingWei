#!/bin/sh
# SpecDoc 三道门 —— 正文 lint / 语料 selftest / 变异体。任一不过即失败。
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
  echo "✗ 三道门未过。修完再提交；确需绕过用 git commit --no-verify，"
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
if true; then
  scripts/gen-status.sh > /tmp/.dw-status.$$ 2>/dev/null || true
  if ! diff -q /tmp/.dw-status.$$ docs/DELIVERY-STATUS.md >/dev/null 2>&1; then
    scripts/gen-status.sh > docs/DELIVERY-STATUS.md
    git add docs/DELIVERY-STATUS.md 2>/dev/null || true
    echo "  ⑤ DELIVERY-STATUS.md 已自动重生成并入暂存区"
  else
    echo "  ⑤ DELIVERY-STATUS.md 已是最新 ✓"
  fi
  rm -f /tmp/.dw-status.$$
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
echo "✓ 三道门全过"
