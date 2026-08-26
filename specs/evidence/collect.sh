#!/bin/sh
# 评审证据包 —— 作者侧产出物，供门方【复算】，不是门方结论。
#
# 🔴 边界（terra 的 checklist §0 #3 明写）：
#   机器旁证只作旁证，不替代人审结论；「脚本通过」不得写成语义 PASS。
#   本脚本只做一件事：把每条断言与【产出它的命令】一起摆出来，让门方能自己跑一遍。
#   ⚠️ 作者不得代填 checklist 的任何勾选 —— 那是 reviewer-owned（§4.1 · §7.2）。
set -u

# 🔴 本件往 stdout 打。执笔方曾写成 `collect.sh >/dev/null` 并在提交信息里
#   声称「证据包已重出」—— rc=0，而文件一个字没变。是闸 ⑭（证据锚时效）抓出来的。
#   ⇒ 给出 --write：把「重出证据包」这件事变成一个不可能只做一半的动作。
if [ "${1:-}" = "--write" ]; then
  _out=specs/evidence/EVIDENCE.md
  "$0" > "$_out".tmp || { rm -f "$_out".tmp; echo "✗ 证据包生成失败，原件未动"; exit 1; }
  # 🔴 乙域 RV-乙09：包内存着失败输出而照常冻结，是本轮被抓的形态之一。
  if grep -q '^  (rc=[1-9]' "$_out".tmp && ! grep -q 'expect-fail' "$_out".tmp; then
    echo "✗ 证据包内有失败项，拒绝写入（原件未动）："
    grep -B12 '^  (rc=[1-9]' "$_out".tmp | grep '^### ' | sed 's/^### /  · /'
    echo "  🔴 **包内输出必须是当前输入上的成功输出** —— 否则读包的人拿到的是一次失败的快照。"
    echo "  ⇒ 先把失败项修好，再重出。"
    rm -f "$_out".tmp; exit 1
  fi
  mv "$_out".tmp "$_out"
  echo "✓ 已写入 $_out（分支头 $(git rev-parse --short=12 HEAD)）"
  exit 0
fi
echo "# 评审证据包（作者产出，供门方复算）"
echo
echo "生成时刻：$(date -u +%Y-%m-%dT%H:%M:%SZ)"
echo "分支头：\`$(git rev-parse HEAD)\`"
echo
echo "> 🔴 **本包不含任何门方结论。** 每行给出【命令】与【该次输出】，门方可原样重跑。"
echo "> ⚠️ **机器旁证的能力边界**：它们证明的是「形状对不对」，不是「语义对不对」。"
echo "> 语义层判断（引对号是否引错义、枚举是否可反推、effect 是否真的单调收紧）**机器判不了**，归人审。"
echo

# 🔴 评审对象内容指纹（乙域 terra R2 轮4 · RV-乙08）：
#   原先只盖一个「分支头」commit 锚，而证据包在 pre-commit 生成 ——
#   它盖的是【父提交】，随后的提交又改了三件正文与分析工具，
#   于是「证据锚」与「被评审的内容」根本不是同一份东西，提交距离 ≤1 只是**代理判据**。
#   ⇒ 直接记被分析对象的 sha256：指纹对得上，就与提交拓扑无关地证明了「这份证据analyzed 的就是这份内容」。
echo "## 评审对象内容指纹（本包分析的就是这些字节）"
echo
echo '```'
# 🔴 乙域 terra R2 轮4（RV-乙09）：首版指纹面漏了三类**实际影响本包输出**的件 ——
#   本脚本自己（collect.sh）、`docs/tools/speclint.py`、`docs/DELIVERY-STATUS.md`。
#   ⚠️ 判据是【凡影响输出者皆须指纹】，不是【我想到的那几个目录】：
#     首版按目录列举，于是「生成器自身」这个最该记的反而不在里面。
#   基线件（specs/gates/*.txt）同理 —— 棘轮基线一改，⑦ 的输出就变。
for _f in docs/dingwei-platform-specdoc.md docs/dingwei-architecture-design.md \
          docs/dingwei-prototype.html docs/DELIVERY-STATUS.md \
          specs/evidence/collect.sh \
          $(ls scripts/*.sh 2>/dev/null) \
          $(ls specs/tools/*.py specs/tools/*.sh 2>/dev/null) \
          $(ls docs/tools/*.py 2>/dev/null) \
          $(ls specs/gates/*.txt 2>/dev/null); do
  [ -f "$_f" ] && echo "REVIEWED $(sha256sum "$_f" | cut -d' ' -f1)  $_f"
done
echo '```'
echo
echo "> 🔴 **指纹对不上即本包非当期产出** —— 与「分支头」是两个不同的保证："
echo "> 分支头说的是【在哪个提交上跑的】，指纹说的是【跑的是哪些字节】。"
echo "> 提交距离只是前者的代理，改动落在那一个提交里时它判不出来（乙域 RV-乙08）。"
echo
# 🔴 乙域 terra R2 轮4（RV-乙09）：原实现**丢弃退出码** ——
#   包里存着的 analyze 输出实际是一次 FAIL，而当时重跑才通过，
#   于是「包内输出」与「当前终审输入」根本不对应，读包的人却看不出来。
#   ⇒ 每项记录 rc；有非预期失败时 --write **拒绝写入**，把「重出成功输出」
#     变成一个不可能跳过的步骤，而不是一句叮嘱。
_FAILED=""
run() { # 标题, 命令 [, expect-fail]
  echo "### $1"; echo; echo '```'; echo "\$ $2"
  _o=$(sh -c "$2" 2>&1); _rc=$?
  printf '%s\n' "$_o" | sed 's/^/  /'
  echo "  (rc=$_rc)"
  echo '```'; echo
  if [ "$_rc" -ne 0 ] && [ "${3:-}" != "expect-fail" ]; then
    _FAILED="$_FAILED
  · $1（rc=$_rc）"
  fi
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
# 🔴 原来这一行把「现为 8」写死在标签里，而数字由右边的命令实算 —— 两处不同源，
#    数一变标签就说谎。且旧命令只数 pgx|sqlite 两个字面 driver 名（按名字判非按效果判），
#    换 driver 或改用 pgxpool 就数不到。两处一并改：标签不带数，口径统一交给闸。
run "仓储包外的库句柄处数（判据：恒为 0；口径与实数均出自 scripts/repo-boundary-gate.sh）" \
    "scripts/repo-boundary-gate.sh"
run "四处存储的建表点（36 / 11 / 5 / 9）" \
    "for p in internal/store/migrations internal/mcs/board/schema.sql internal/fanout internal/datad; do printf '%-34s ' \$p; grep -rhoP 'CREATE TABLE (IF NOT EXISTS )?\\K\\w+' \$p | sort -u | wc -l; done"
run "task 表跨存储重名（须在 SP7a 前定名，见 docs/v0.1/task-table-collision-analysis.md）" \
    "grep -rlP 'CREATE TABLE (IF NOT EXISTS )?task\\b' internal/mcs/board/schema.sql internal/datad/pgstore.go"
# ⚠️ 标签只描述【这条命令搜了什么】，不描述裁定，也不外推结论 ——
#   原标签写「C.3 裁定不做 RLS」，而现行裁定是 C.3a「PG ＋ RLS（人层）采纳」，
#   标签在证据包里替一个已被推翻的裁定作证（甲域 RV-甲41）。
run "RLS 字面标记出现次数（仅覆盖下列三个字面量，**不足以代表 RLS 合同的全部实现形态**）" \
    "grep -rn 'row_level_security\\|SET LOCAL\\|app\\.tenant' --include=*.go --include=*.sql . | wc -l"
echo "> 🔴 **本项零命中不得外推为「RLS 零实现」** —— 它只说明这三个字面量没出现。"
echo "> 现行裁定为 C.3a「PG ＋ RLS（人层）✅ 采纳」；RLS 合同的验收判据见架构 A§12 与 SpecDoc §22.2c，"
echo "> **其覆盖面远大于本项**。扩 detector 或改判据前，本项只作字面子集旁证（甲域 RV-甲41）。"
echo
echo "## 四、本包未覆盖的（明说，不留白）"
cat <<'TXT'
| 项 | 为什么本包给不出 |
|---|---|
| 三类越权负测（伪造 tenant / 跨租户读 / 缺 context） | 🔴 **受控仓储 API 尚未实现**，负测无被测对象。架构设计 A§12 已标「未建」 |
| 紧急面平台不可用负测 | 需实际部署环境，本包为静态仓库分析 |
| 敏感内容判定器覆盖面 | 判定器未实现（§22.5 为目标态） |
| 语义层的一切 | 机器判不了，见本文件头注 |
TXT
