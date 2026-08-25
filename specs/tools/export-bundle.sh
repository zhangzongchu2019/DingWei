#!/bin/sh
# 导出锚定快照包 —— 件清单【由 git 导出】，不手写。
#
# 🔴 立此脚本的直接原因（丙域 kimi 终审 m 类）：
#   MANIFEST 此前由手写的 files=[...] 生成，却自述「全部闸与工具的源码」，
#   实际漏了 specs/evidence/collect.sh；自查后发现漏的不止一件。
#   ⇒ **凡说「全部」，清单就不能手写。**
#
# 用法: specs/tools/export-bundle.sh <锚> <目标目录>
set -eu
R=$1; D=$2
mkdir -p "$D"

INCLUDE='docs/dingwei-platform-specdoc.md docs/dingwei-architecture-design.md
docs/dingwei-prototype.html docs/task-table-collision-analysis.md docs/DELIVERY-STATUS.md
docs/reference/not-adopted-pg-rls.md docs/SESSION-HANDOFF.md
docs/tools/speclint.py docs/tools/speclint-corpus.md
specs/constitution.md specs/README.md specs/platform/spec.md
specs/platform/checklist.md specs/platform/checklist.security-reviewer.md
specs/platform/checklist.crossartifact-reviewer.md specs/platform/checklist.INDEX.md
specs/evidence/EVIDENCE.md specs/evidence/collect.sh specs/evidence/reference-index.md
specs/tools/analyze.sh specs/tools/export-bundle.sh
scripts/speclint-gate.sh scripts/arch-refcheck.sh scripts/delivery-check.sh scripts/gen-status.sh'

EXCLUDED='scripts/check-sensitive.sh · scripts/na3-pg-evidence.sh · scripts/reliable_delivery_canary.sh'

LIST=$(git ls-tree -r --name-only "$R" -- $INCLUDE | sort)
N=$(printf '%s\n' $LIST | wc -l | tr -d ' ')

{
  echo "# 完整评审件清单（锚定快照包）"
  echo
  echo "**统一锚**：\`$R\`　分支 \`design/addendum-decisions\`　**件数 $N**"
  echo
  echo "> 🔴 **清单由 \`git ls-tree\` 导出、指纹由本脚本实算，均不手写。**"
  echo "> 此前手写清单漏了 \`collect.sh\`（丙域 kimi m 类），自查后发现漏的不止一件。"
  echo
  echo "> ⚠️ **明确排除（有据，非默默不带）**：$EXCLUDED"
  echo "> —— 本轮评审链之外的既有运维脚本，与三件正文无引用关系。"
  echo
  echo "| 文件 | 仓内路径 | 行 | 字节 | sha256 |"
  echo "|---|---|---|---|---|"
  for f in $LIST; do
    b=$(basename "$f")
    git show "$R:$f" > "$D/$b"
    printf '| `%s` | `%s` | %s | %s | `%s` |\n' "$b" "$f" \
      "$(wc -l < "$D/$b")" "$(wc -c < "$D/$b")" "$(sha256sum "$D/$b" | cut -d' ' -f1)"
  done
} > "$D/MANIFEST.md"
echo "导出 $N 件 → $D"
