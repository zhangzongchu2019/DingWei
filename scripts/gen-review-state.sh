#!/bin/sh
# 评审状态生成件 —— 从台账与闸实跑派生，供门方【从仓里读】而不是从消息里追。
#
# 🔴 立此件的直接原因：评审转述与推锚已交叉五次
#   （terra 抽 4633e6d · 归口重提丙域 · kimi 抽 65b33d0 · sol 抽 57a804a · terra 抽 57a804a）。
#   每次都是「门方按较早的锚复核，报回的缺口在更新的锚上已修」。
#   ⚠️ 根子不是谁疏忽：**我推锚快于转述，而状态只活在消息里**。
#   ⇒ 把状态落进仓：门方 checkout 任一锚即可读到「在这个锚上，各条 RV 处于什么状态」，
#     不必与消息比对，也不必读提交信息。
set -u
L=${L:-specs/gates/rv-ledger.txt}
[ -f "$L" ] || { echo "✗ 闸依赖缺失：$L"; exit 2; }

echo "# 评审状态（**机器生成，勿手改**）"
echo
echo "本件由 \`scripts/gen-review-state.sh\` 从 \`specs/gates/rv-ledger.txt\` 与闸实跑派生。"
echo "🔴 **门方请以本件为准判断「这个锚上某条是否已处置」**，不必与消息比对。"
echo "⚠️ 本件说的是【执笔方处置到哪一步】，**不是【门方是否认可】** —— 认可只由门方给。"
echo
# 🔴 **本件不内嵌 commit 锚**（同 A§0.1「能过期的东西都不写」）：
#   本件在 pre-commit 生成，此刻 HEAD 还是**父提交** ⇒ 写进去的锚在它自己所在的那个
#   提交上就是错的，克隆到该提交后闸 ㉒ 立刻不一致。
#   ⚠️ 与证据包「分支头」当初踩的是同一个坑（⑭ 已因此改判据）——
#     **产物写不进自己的哈希。**
#   ⇒ 锚由读者自取：`git rev-parse HEAD`。本件只说「在这个锚上各条处于什么状态」。
echo "> 🔴 本件**不写 commit 锚** —— 它在 pre-commit 生成，写进去的必然是父提交。"
echo "> 锚请自取：\`git rev-parse HEAD\`。"
echo
echo "## 计数"
echo
awk -F'\t' '!/^#/&&NF>1{d[$5]++; t++} END{printf "| 执笔方处置 | 条数 |\n|---|---|\n"; for(k in d) printf "| %s | %d |\n", k, d[k]; printf "| **合计** | **%d** |\n", t}' "$L"
echo
echo "## 未闭项（挂账 / 未决）—— 门方复核时看这里"
echo
# 🔴 须标【归谁】：只写「挂账」时，读的人分不清哪条在执笔方手上、哪条在等业主/ops。
#   归口方曾据此提醒「别只盯着 owner/ops 挂账」—— 提醒是对的，本表当时确实说不清。
echo "| ID | 域 | 严重度 | 评审方状态 | 执笔方处置 | **归谁** | 摘要 |"
echo "|---|---|---|---|---|---|---|"
awk -F'\t' '!/^#/&&NF>1 && ($5 ~ /挂账/ || $5 ~ /未决/ || $5=="未处置"){
  who="⚠️ **执笔方**"
  if ($1=="RV-执笔01" || $1=="RV-丙01") who="coordinator 归口"
  else if ($1=="RV-甲08") who="业主 + ops"
  else if ($1=="RV-乙04" || $1=="RV-甲10" || $1=="RV-甲06") who="SP1 接手人（业主待定）"
  printf "| %s | %s | %s | %s | %s | %s | %s |\n", $1,$2,$3,$4,$5,who,substr($7,1,40)}' "$L"
echo
echo "🔴 **归谁＝⚠️ 执笔方 的行，是【我能修而没修】的** —— 若出现，即为我的欠账，不得算进"
echo "「不在我手上」那四条。本表存在的意义之一就是让这一栏无处可藏。"
echo
echo "## 闸反测条数（抽锚核对用）"
echo
echo '```'
for g in prose-style evidence-fresh path-anchor conflict-marker rv-integrity legacy-name; do
  [ -x "scripts/$g-gate.sh" ] || continue
  printf '%-16s %s\n' "$g" "$(sh "scripts/$g-gate.sh" --selftest 2>/dev/null | tail -1)"
done
echo '```'
echo
echo "## 不在执笔方权限内的阻塞"
echo
echo "| # | 事 | 归谁 | 现状 |"
echo "|---|---|---|---|"
echo "| 1 | RLS 三条 fixture 实跑 | **ops** | 执笔方与乙域门方**均无 PG 环境**；在有人贴出输出前 C.3a.1a 不得称已验收 |"
echo "| 2 | checklist 落勾 | **三域各自** | $(sh scripts/checklist-executed-gate.sh 2>/dev/null | grep -o '合计：[0-9]* 项 · 已勾 [0-9]*' | head -1)；§4.1 reviewer-owned，执笔方不代填 |"
echo "| 3 | SP1 接手人 | **业主** | 未定；RV-乙04/甲10/甲06 的时点为执笔方初值 |"
echo "| 4 | ops 开仓权限 | **业主 + 工蜂管理员** | ops \`ls-remote\` 仍 Project not found ⇒ 分支保护配不了 |"
