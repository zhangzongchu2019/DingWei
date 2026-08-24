#!/bin/sh
# 评审证据包 —— 作者侧产出物，供门方【复算】，不是门方结论。
#
# 🔴 边界（terra 的 checklist §0 #3 明写）：
#   机器旁证只作旁证，不替代人审结论；「脚本通过」不得写成语义 PASS。
#   本脚本只做一件事：把每条断言与【产出它的命令】一起摆出来，让门方能自己跑一遍。
#   ⚠️ 作者不得代填 checklist 的任何勾选 —— 那是 reviewer-owned（§4.1 · §7.2）。
set -u
echo "# 评审证据包（作者产出，供门方复算）"
echo
echo "生成时刻：$(date -u +%Y-%m-%dT%H:%M:%SZ)"
echo "分支头：\`$(git rev-parse HEAD)\`"
echo
echo "> 🔴 **本包不含任何门方结论。** 每行给出【命令】与【该次输出】，门方可原样重跑。"
echo "> ⚠️ **机器旁证的能力边界**：它们证明的是「形状对不对」，不是「语义对不对」。"
echo "> 语义层判断（引对号是否引错义、枚举是否可反推、effect 是否真的单调收紧）**机器判不了**，归人审。"
echo
run() { # 标题, 命令
  echo "### $1"; echo; echo '```'; echo "\$ $2"; sh -c "$2" 2>&1 | sed 's/^/  /'; echo '```'; echo
}
echo "## 一、单件闸"
run "SpecDoc speclint（受控符号 · 字段声明 · 表完整性 · E 计数 · 权威定义唯一 等 14 条规则）" \
    "python3 docs/tools/speclint.py docs/dingwei-platform-specdoc.md"
run "speclint 语料自检（正反例须拦在预期规则上）" \
    "python3 docs/tools/speclint.py --selftest docs/tools/speclint-corpus.md"
run "speclint 变异体（改坏规则，其反例须随之失守 —— 证明语料有牙）" \
    "python3 docs/tools/speclint.py --mutate docs/tools/speclint-corpus.md"
run "架构设计引用核验（A§ 本件 / § SpecDoc 分别解析）" \
    "scripts/arch-refcheck.sh"
run "引用核验的反测四项（含 SpecDoc heading 重复 —— 曾漏接失败条件）" \
    "scripts/arch-refcheck.sh --selftest"
echo "## 二、跨制品"
run "analyze 跨制品一致性（件与件之间；单件自洽由上节负责）" \
    "specs/tools/analyze.sh"
echo "## 三、代码现状实测（架构设计 A§6.2 所载数字的来源）"
run "internal 包总数" "ls -d internal/*/ | wc -l"
run "workpulse 装载的包（架构设计称 19）" \
    "grep -rhoE 'zhangzongchu2019/dingwei/internal/[a-z0-9]+' cmd/workpulse --include=*.go | grep -v _test | sed 's|.*/||' | sort -u | paste -sd' '"
run "internal/view 是否存在（架构设计称不存在）" "ls -d internal/view 2>&1 || true"
run "仓储包外 sql.Open 处数（判据：恒为 0；现为 8 ⇒ 不达标）" \
    "grep -rhoE 'sql\\.Open\\(\"(pgx|sqlite)\"' --include=*.go internal | wc -l"
run "四处存储的建表点（36 / 11 / 5 / 9）" \
    "for p in internal/store/migrations internal/mcs/board/schema.sql internal/fanout internal/datad; do printf '%-34s ' \$p; grep -rhoP 'CREATE TABLE (IF NOT EXISTS )?\\K\\w+' \$p | sort -u | wc -l; done"
run "task 表跨存储重名（须在 SP7a 前定名，见 docs/task-table-collision-analysis.md）" \
    "grep -rlP 'CREATE TABLE (IF NOT EXISTS )?task\\b' internal/mcs/board/schema.sql internal/datad/pgstore.go"
run "RLS 相关代码（C.3 裁定不做 RLS；实测本就零实现）" \
    "grep -rn 'row_level_security\\|SET LOCAL\\|app\\.tenant' --include=*.go --include=*.sql . | wc -l"
echo "## 四、本包未覆盖的（明说，不留白）"
cat <<'TXT'
| 项 | 为什么本包给不出 |
|---|---|
| 三类越权负测（伪造 tenant / 跨租户读 / 缺 context） | 🔴 **受控仓储 API 尚未实现**，负测无被测对象。架构设计 A§12 已标「未建」 |
| 紧急面平台不可用负测 | 需实际部署环境，本包为静态仓库分析 |
| 敏感内容判定器覆盖面 | 判定器未实现（§22.5 为目标态） |
| 语义层的一切 | 机器判不了，见本文件头注 |
TXT
