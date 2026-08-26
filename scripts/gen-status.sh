#!/bin/sh
# 生成 DELIVERY-STATUS.md —— 单一状态源,随时可拉,不靠消息传递。
#
# 立此件的直接原因（2026-08-24）：一天内交叉七次，且本席的澄清信本身也在交叉。
# 锚标注解决「收信方能否判断这条是否过时」，解决不了消息延迟。
# 根因是老问题的新形态：**状态存在消息里**。⇒ 让状态存在一个固定位置，谁都能随时拉。
set -eu
B=$(git rev-parse --abbrev-ref HEAD)
H=$(git rev-parse HEAD)
D=docs
say() { printf '%s\n' "$*"; }

# --stable：只输出【内容列】（件·版本·行/字节·sha256），用于提交闸做「是否需要重生成」的比较。
#
# 🔴 立此模式的直接原因（复核方实测、coordinator 归口记账）：
#   本文件在 pre-commit 生成，那时本次提交尚不存在，故顶部「生成自 commit」、尾部 git log 列表、
#   以及「本次提交中」标注三者【天然与检出后不同】。于是**任何人在干净检出上跑一次闸，
#   这个文件必然变脏** —— 复核方会以为自己动过。
#   ⚠️ 假信号是这类「单一事实源」最贵的成本：它让人不敢信这份文件，也让真变化淹没在噪声里。
#   ⇒ 闸只比内容列；内容没变就【不碰这个文件】，易变的元信息随内容变化时一并更新。
if [ "${1:-}" = "--stable" ]; then
  for f in "$D/dingwei-platform-specdoc.md" "$D/dingwei-prototype.html" "$D/dingwei-platform-architecture.md"; do
    [ -f "$f" ] || continue
    v=$(grep -m1 -oE '\*\*版本\*\*: v[0-9]+\.[0-9]+' "$f" 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+' || true)
    [ -n "$v" ] || v=$(sed -n 's/^| \*\*版本\*\* | \(v[0-9][0-9]*\.[0-9][0-9]*\) |$/\1/p' "$f" | head -1)
    [ -n "$v" ] || v=$(grep -m1 -oE '原型 v[0-9]+\.[0-9]+' "$f" | grep -oE 'v[0-9]+\.[0-9]+' || true)
    lb=$(wc -l -c < "$f" | tr -s ' ' | sed 's/^ //;s/ / \/ /')
    printf '%s|%s|%s|%s\n' "$(basename "$f")" "$v" "$lb" "$(sha256sum "$f" | cut -d' ' -f1)"
  done
  exit 0
fi

say "# DingWei 交付状态（机器生成，勿手改）"
say ""
say "生成自 commit \`$H\` · 分支 \`$B\`"
say ""
say "⚠️ 本文件在 **pre-commit 时刻**生成，此刻本次提交尚未存在 ——"
say "故「生成自 commit」指的是**上一个**提交；标了「本次提交中」的件，其新锚即紧随其后的那个提交。"
say "**sha256 与行字节始终取自工作树，是当前值，不滞后。**"
say "生成命令：\`scripts/gen-status.sh\`（内容全部由 git 实取，无手写值）"
say ""
say "> 🔴 **本文件是单一状态源。** 消息会延迟、会交叉；本文件不会——它随仓库走。"
say "> 判断一条评审意见是否已被处理：看该件「末次改动锚」是否晚于意见所针对的锚。"
say ""
say "> ⚠️ **若你是在共享目录读到本文件，它是副本，可能已过期。**"
say "> 副本自身也是「第二事实源」—— 本席这几轮反复栽的正是这一类。"
say "> **自检办法（不必问本席，也不要拿「生成自 commit」当判据）**："
say "> 把下表的 **sha256 行** 与你自己检出里的同名件实算一次（\`sha256sum <file>\`）——"
say "> 对得上即内容同源，对不上才是副本落后。"
say "> 🔴 **不要用顶部「生成自 commit」判落后**：它天然滞后一格（本文件在 pre-commit 生成，"
say "> 那一刻本次提交尚不存在），拿它当落后判据【必然误报】。"
say ""
say "| 件 | 版本 | 末次改动锚 | 行 / 字节 | sha256 |"
say "|---|---|---|---|---|"
for f in "$D/dingwei-platform-specdoc.md" "$D/dingwei-prototype.html" "$D/dingwei-platform-architecture.md"; do
  [ -f "$f" ] || continue
  v=$(grep -m1 -oE '\*\*版本\*\*: v[0-9]+\.[0-9]+' "$f" 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+' || true)
  [ -n "$v" ] || v=$(sed -n 's/^| \*\*版本\*\* | \(v[0-9][0-9]*\.[0-9][0-9]*\) |$/\1/p' "$f" | head -1)
  [ -n "$v" ] || v=$(grep -m1 -oE '原型 v[0-9]+\.[0-9]+' "$f" | grep -oE 'v[0-9]+\.[0-9]+' || true)
  c=$(git log -1 --format=%H -- "$f")
  # ⚠️ 本文件由 pre-commit 生成，那时【本次提交尚不存在】，git log 只能看到上一次改动。
  #    故对处于暂存区的件显式标注，否则「末次改动锚」会稳定地滞后一格 —— 看着精确，实则过期。
  if git diff --cached --name-only 2>/dev/null | grep -qx "$f"; then
    c="$c<br>⚠️ **本次提交中**（新锚见本文件顶部「生成自 commit」的下一个提交）"
  fi
  lb=$(wc -l -c < "$f" | tr -s ' ' | sed 's/^ //;s/ / \/ /')
  s=$(sha256sum "$f" | cut -d' ' -f1)
  say "| \`$(basename "$f")\` | **$v** | \`$c\` | $lb | \`$s\` |"
done
say ""
say "## 末次改动锚之后的提交（若为空，说明该件即为仓库最新）"
say ""
say '```'
git log --oneline -8
say '```'
