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
say "> **自检办法（不必问本席）**：把上方「生成自 commit」与分支最新比一次——"
say "> \`git -C <repo> rev-parse origin/$B\` 不等于它，就说明本副本落后，以仓内 \`$D/DELIVERY-STATUS.md\` 为准。"
say "> 仓内那份由 pre-commit 自动重生成，不依赖任何人记得。"
say ""
say "| 件 | 版本 | 末次改动锚 | 行 / 字节 | sha256 |"
say "|---|---|---|---|---|"
for f in "$D/dingwei-platform-specdoc-v2.0.md" "$D/dingwei-prototype-v0.1.html" "$D/dingwei-architecture-design-v0.1.md"; do
  [ -f "$f" ] || continue
  v=$(grep -m1 -oE '\*\*版本\*\*: v[0-9]+\.[0-9]+' "$f" 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+' || true)
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
