#!/bin/sh
# 架构事实源切换核验：预期集合由当前树中所有新旧架构路径引用自动派生。
set -eu
NEW=docs/dingwei-platform-architecture.md
OLD=docs/dingwei-architecture-design.md
HIST=docs/v0.1/dingwei-architecture-design-v0.34.md
[ -f "$NEW" ] || { echo "✗ 新架构件缺失：$NEW"; exit 2; }
[ -f "$HIST" ] || { echo "✗ 历史件缺失：$HIST"; exit 2; }
[ ! -e "$OLD" ] || { echo "✗ 旧架构仍占现行路径：$OLD"; exit 1; }
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
rg -l 'dingwei-(platform-architecture|architecture-design)\.md' scripts specs docs \
  -g '!docs/v0.1/**' -g '!docs/dingwei-revision-history.md' \
  -g '!specs/platform/checklist*.md' -g '!specs/gates/rv-ledger.txt' \
  -g '!specs/evidence/EVIDENCE.md' -g '!specs/gates/status-stale-exceptions.txt' \
  -g '!docs/SUPERSEDED-CLAIM-OWNERSHIP.md' | sort -u > "$T/expected"
while IFS= read -r f; do grep -q 'dingwei-platform-architecture\.md' "$f" && echo "$f"; done < "$T/expected" | sort -u > "$T/inspected"
comm -23 "$T/expected" "$T/inspected" > "$T/unknown"
echo "预期集合 $(wc -l < "$T/expected" | tr -d ' ')："; sed 's/^/  /' "$T/expected"
echo "实检集合 $(wc -l < "$T/inspected" | tr -d ' ')："; sed 's/^/  /' "$T/inspected"
echo "未知集合 $(wc -l < "$T/unknown" | tr -d ' ')："; sed 's/^/  /' "$T/unknown"
[ ! -s "$T/unknown" ] || { echo '✗ 活跃引用仍未切到新架构件'; exit 1; }
grep -q '`dingwei-platform-architecture.md`' docs/DELIVERY-STATUS.md || { echo '✗ DELIVERY-STATUS 未登记新架构'; exit 1; }
! grep -q '`dingwei-architecture-design.md`' docs/DELIVERY-STATUS.md || { echo '✗ DELIVERY-STATUS 仍登记旧架构'; exit 1; }
! sed -n '1,/^# 附录 D/p' docs/dingwei-platform-specdoc.md | grep -q 'A§' || { echo '✗ SpecDoc 现行正文仍引用旧 A§'; exit 1; }
! grep -q 'A§' docs/dingwei-prototype.html || { echo '✗ 原型仍引用旧 A§'; exit 1; }
echo '✓ 架构唯一事实源切换闭合（未知集合 0）'
