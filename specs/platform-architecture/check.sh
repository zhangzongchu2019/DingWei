#!/bin/sh
# 新版平台架构件的可复算集合检查。
# 能判：四组预期集合是否被目标件完整、唯一声明；接口 ID 与 N/A 理由码形状。
# 判不了：分域是否合理、接口 effect 是否完整、引用是否引对义；这些仍归全文人审。
set -u

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
SELF=$(cd "$(dirname "$0")" && pwd)/$(basename "$0")
DOC=${DOC:-"$ROOT/docs/dingwei-platform-architecture.md"}
CONTRACT=${CONTRACT:-"$ROOT/specs/platform-architecture/spec.md"}
bad=0
unknown_state=0

fail() { echo "✗ $*"; bad=$((bad+1)); }
ok() { echo "✓ $*"; }

for f in "$DOC" "$CONTRACT"; do
  [ -f "$f" ] || { echo "✗ 依赖不存在：$f"; exit 2; }
done

T=$(mktemp -d) || { echo '✗ 无法建立临时目录'; exit 2; }
trap 'rm -rf "$T"' EXIT HUP INT TERM

if [ "${1:-}" = '--selftest' ]; then
  n=0; failed=0
  expect() {
    name=$1; file=$2; want_rc=$3; pattern=$4
    out=$(DOC="$file" sh "$SELF" 2>&1); rc=$?
    n=$((n+1))
    if [ "$rc" -eq "$want_rc" ] && printf '%s\n' "$out" | grep -q "$pattern"; then
      echo "✓ $name"
    else
      echo "✗ $name（rc=$rc want=$want_rc pattern=$pattern）"
      failed=1
    fi
  }

  cp "$DOC" "$T/base.md"
  awk '/COVERAGE-SPECDOC-BEGIN/{on=1} on && /^§0$/{next} {print} /COVERAGE-SPECDOC-END/{on=0}' "$T/base.md" > "$T/missing-spec.md"
  expect '缺一个 SpecDoc ID 必拒' "$T/missing-spec.md" 1 'UNKNOWN: §0'

  awk '/COVERAGE-SPECDOC-END/{print "§999"} {print}' "$T/base.md" > "$T/extra-spec.md"
  expect '多一个 SpecDoc ID 必拒' "$T/extra-spec.md" 1 'EXTRA: §999'

  awk '/COVERAGE-IMPLEMENTATION-BEGIN/{on=1} on && /^cmd\/fcs$/{next} {print} /COVERAGE-IMPLEMENTATION-END/{on=0}' "$T/base.md" > "$T/missing-impl.md"
  expect '缺一个实现单元必拒' "$T/missing-impl.md" 1 'UNKNOWN: cmd/fcs'

  awk '/COVERAGE-PROTOTYPE-BEGIN/{on=1} on && /^s1$/{next} {print} /COVERAGE-PROTOTYPE-END/{on=0}' "$T/base.md" > "$T/missing-proto.md"
  expect '缺一个原型入口必拒' "$T/missing-proto.md" 1 'UNKNOWN: s1'

  awk '/RESPONSIBILITY-COVERAGE-BEGIN/{on=1} on && /`RT-01`/{next} {print} /RESPONSIBILITY-COVERAGE-END/{on=0}' "$T/base.md" > "$T/missing-theme.md"
  expect '缺一个责任主题必拒' "$T/missing-theme.md" 1 'UNKNOWN: RT-01'

  awk '/IDN-CMD-AuthenticateChannel/ && !done {print; done=1} {print}' "$T/base.md" > "$T/dup-interface.md"
  expect '重复接口 ID 必拒' "$T/dup-interface.md" 1 '重复接口 ID'

  sed '0,/state:adapt/s//state:transition/' "$T/base.md" > "$T/bad-state.md"
  expect '非五态状态词必拒' "$T/bad-state.md" 1 '非法迁移状态'

  awk '/INTERFACE-FAMILY-COVERAGE-BEGIN/{on=1} on && /`IF-05`/{next} {print} /INTERFACE-FAMILY-COVERAGE-END/{on=0}' "$T/base.md" > "$T/missing-family.md"
  expect '缺强制接口族必拒' "$T/missing-family.md" 1 'UNKNOWN: IF-05'

  awk '/COVERAGE-SPECDOC-BEGIN/{print; on=1; next} /COVERAGE-SPECDOC-END/{on=0} !on{print}' "$T/base.md" > "$T/empty-set.md"
  expect '实检集合为空必须 UNKNOWN' "$T/empty-set.md" 2 '实检集合为空.*UNKNOWN'

  [ "$failed" -eq 0 ] && { echo "架构覆盖检查反测 $n/$n 全过"; exit 0; }
  exit 1
fi

ANCHOR=$(awk -F'`' '/\*\*冻结施工锚\*\*/{print $2; exit}' "$CONTRACT")
SPEC_SHA=$(grep -m1 '| SpecDoc v' "$CONTRACT" | grep -oE '[0-9a-f]{64}' | head -1)
PROTO_SHA=$(grep -m1 '| 原型 v' "$CONTRACT" | grep -oE '[0-9a-f]{64}' | head -1)
case "$ANCHOR" in ''|*[!0-9a-f]*) echo '✗ 冻结施工锚解析失败'; exit 2;; *) :;; esac
[ ${#ANCHOR} -eq 40 ] || { echo '✗ 冻结施工锚不是 40hex'; exit 2; }
[ ${#SPEC_SHA} -eq 64 ] && [ ${#PROTO_SHA} -eq 64 ] || { echo '✗ 输入 sha256 解析失败'; exit 2; }
git cat-file -e "$ANCHOR^{commit}" 2>/dev/null || { echo "✗ 冻结锚不可取：$ANCHOR"; exit 2; }
git show "$ANCHOR:docs/dingwei-platform-specdoc.md" > "$T/spec.frozen" || exit 2
git show "$ANCHOR:docs/dingwei-prototype.html" > "$T/proto.frozen" || exit 2
got_spec_sha=$(sha256sum "$T/spec.frozen" | awk '{print $1}')
got_proto_sha=$(sha256sum "$T/proto.frozen" | awk '{print $1}')
[ "$got_spec_sha" = "$SPEC_SHA" ] || { echo "✗ 冻结 SpecDoc sha 不符：$got_spec_sha != $SPEC_SHA"; exit 1; }
[ "$got_proto_sha" = "$PROTO_SHA" ] || { echo "✗ 冻结原型 sha 不符：$got_proto_sha != $PROTO_SHA"; exit 1; }
echo "✓ 冻结输入可取且 sha256 一致（anchor=$ANCHOR）"

extract_block() {
  begin=$1
  end=$2
  file=$3
  awk -v b="$begin" -v e="$end" '
    index($0,b){on=1; next}
    index($0,e){on=0; exit}
    on{print}
  ' "$file"
}

compare_set() {
  label=$1
  expected=$2
  inspected=$3
  sort -u "$expected" -o "$expected"
  sort -u "$inspected" -o "$inspected"
  exp_n=$(awk 'NF{n++} END{print n+0}' "$expected")
  got_n=$(awk 'NF{n++} END{print n+0}' "$inspected")
  if [ "$exp_n" -eq 0 ] || [ "$got_n" -eq 0 ]; then
    echo "✗ $label：预期或实检集合为空 ⇒ UNKNOWN（expected=$exp_n inspected=$got_n）"
    unknown_state=1
    return 2
  fi
  comm -23 "$expected" "$inspected" > "$T/$label.unknown"
  comm -13 "$expected" "$inspected" > "$T/$label.extra"
  unknown_n=$(awk 'NF{n++} END{print n+0}' "$T/$label.unknown")
  extra_n=$(awk 'NF{n++} END{print n+0}' "$T/$label.extra")
  echo "[$label] 预期=$exp_n 实检=$got_n 未知=$unknown_n 多余=$extra_n"
  if [ "$unknown_n" -gt 0 ]; then
    sed 's/^/  UNKNOWN: /' "$T/$label.unknown"
  fi
  if [ "$extra_n" -gt 0 ]; then
    sed 's/^/  EXTRA: /' "$T/$label.extra"
  fi
  [ "$unknown_n" -eq 0 ] && [ "$extra_n" -eq 0 ]
}

for h in \
  '## 2. 目标态业务域' \
  '## 6. 接口目录' \
  '## 8. 数据架构' \
  '## 9. 运行与部署架构' \
  '## 10. 安全架构' \
  '## 12. 现状包到目标架构映射' \
  '## 13. 原型入口到架构能力' \
  '## 18. 验收矩阵'
do
  grep -Fqx "$h" "$DOC" || fail "缺必备章节：$h"
done

# 1. SpecDoc 口径：冻结输入中所有二级标题的首个稳定 ID（§N / B2 / C / E / F）。
grep -E '^## ' "$T/spec.frozen" | sed -E 's/^## //; s/[[:space:]].*$//' > "$T/spec.expected"
extract_block 'COVERAGE-SPECDOC-BEGIN' 'COVERAGE-SPECDOC-END' "$DOC" \
  | awk 'NF && $0 !~ /^<!--/' > "$T/spec.inspected"
compare_set specdoc "$T/spec.expected" "$T/spec.inspected" || bad=$((bad+1))

# 2. 实现单元口径：冻结锚中含至少一个非测试 Go 文件的唯一目录。
git ls-tree -r --name-only "$ANCHOR" -- internal cmd \
  | grep -E '\.go$' | grep -Ev '_test\.go$' | sed 's#/[^/]*$##' > "$T/impl.expected"
extract_block 'COVERAGE-IMPLEMENTATION-BEGIN' 'COVERAGE-IMPLEMENTATION-END' "$DOC" \
  | awk 'NF && $0 !~ /^<!--/' > "$T/impl.inspected"
compare_set implementation "$T/impl.expected" "$T/impl.inspected" || bad=$((bad+1))

# 3. 原型入口口径：冻结原型 nav 内所有唯一 href fragment。
sed -n '/<nav id="nav">/,/<\/nav>/p' "$T/proto.frozen" \
  | grep -oE 'href="#[^"]+"' | cut -d'#' -f2 | tr -d '"' > "$T/proto.expected"
extract_block 'COVERAGE-PROTOTYPE-BEGIN' 'COVERAGE-PROTOTYPE-END' "$DOC" \
  | awk 'NF && $0 !~ /^<!--/' > "$T/proto.inspected"
compare_set prototype "$T/proto.expected" "$T/proto.inspected" || bad=$((bad+1))

# 4. 责任主题口径：合同机器块内的稳定 RT ID。
extract_block 'RESPONSIBILITY-THEMES-BEGIN' 'RESPONSIBILITY-THEMES-END' "$CONTRACT" \
  | grep -oE '`RT-[0-9]+`' | tr -d '`' > "$T/theme.expected"
extract_block 'RESPONSIBILITY-COVERAGE-BEGIN' 'RESPONSIBILITY-COVERAGE-END' "$DOC" \
  | grep -oE '`RT-[0-9]+`' | tr -d '`' > "$T/theme.inspected"
compare_set responsibility "$T/theme.expected" "$T/theme.inspected" || bad=$((bad+1))

# 5. 十个强制接口族。
extract_block 'INTERFACE-FAMILIES-BEGIN' 'INTERFACE-FAMILIES-END' "$CONTRACT" \
  | grep -oE '`IF-[0-9]+`' | tr -d '`' > "$T/family.expected"
extract_block 'INTERFACE-FAMILY-COVERAGE-BEGIN' 'INTERFACE-FAMILY-COVERAGE-END' "$DOC" \
  | grep -oE '`IF-[0-9]+`' | tr -d '`' > "$T/family.inspected"
compare_set interface_family "$T/family.expected" "$T/family.inspected" || bad=$((bad+1))

ids=$(grep -oE '^\| `(IDN|GOV|WRK|KNO|CAP|COL|AGT|MSG|PLT)-(CMD|QRY|CAP)-[^`]+`' "$DOC" \
  | sed -E 's/^\| `([^`]+)`.*/\1/')
id_total=$(printf '%s\n' "$ids" | awk 'NF{n++} END{print n+0}')
id_dup=$(printf '%s\n' "$ids" | awk 'NF{n[$0]++} END{for(k in n) if(n[k]>1) print k}')
if [ "$id_total" -eq 0 ]; then
  echo '✗ 接口目录为空 ⇒ UNKNOWN'
  exit 2
elif [ -n "$id_dup" ]; then
  echo "$id_dup" | sed 's/^/  ✗ 重复接口 ID：/'
  bad=$((bad+1))
else
  ok "接口 ID $id_total 项且无重复"
fi

family_refs=$(extract_block 'INTERFACE-FAMILY-COVERAGE-BEGIN' 'INTERFACE-FAMILY-COVERAGE-END' "$DOC" \
  | grep -oE '`(IDN|GOV|WRK|KNO|CAP|COL|AGT|MSG|PLT)-(CMD|QRY|CAP)-[^`]+`' | tr -d '`')
missing_ref=0
for ref in $family_refs; do
  printf '%s\n' "$ids" | grep -Fqx "$ref" || { echo "  ✗ 接口族引用不存在：$ref"; missing_ref=1; }
done
[ "$missing_ref" -eq 0 ] || bad=$((bad+1))

allowed_na='NA-READONLY|NA-INPROCESS|NA-IMMUTABLE|NA-NO-EXTERNAL-EFFECT|NA-NO-DATA'
all_na=$(grep -oE 'N/A\([A-Z0-9-]+\)' "$DOC" || true)
bad_na=$(printf '%s\n' "$all_na" | grep -Ev "^N/A\(($allowed_na)\)$" || true)
if [ -n "$bad_na" ]; then
  echo "$bad_na" | sed 's/^/  ✗ 非受控 N\/A 理由码：/'
  bad=$((bad+1))
else
  ok 'N/A 理由码均在受控枚举内（当前可为 0 项）'
fi

states=$(grep -oE 'state:[a-z-]+' "$DOC" || true)
state_total=$(printf '%s\n' "$states" | awk 'NF{n++}END{print n+0}')
bad_states=$(printf '%s\n' "$states" | grep -Ev '^state:(new|retain|adapt|rewrite|retire)$' || true)
missing_state_rows=$(awk '
  /^### 12\.1 /{on=1} /^## 13\./{on=0}
  on && /^\| `/ && $0 !~ /state:(new|retain|adapt|rewrite|retire)/ {print}
' "$DOC")
if [ "$state_total" -eq 0 ]; then
  echo '✗ 迁移状态集合为空 ⇒ UNKNOWN'
  unknown_state=1
elif [ -n "$bad_states" ] || [ -n "$missing_state_rows" ]; then
  [ -z "$bad_states" ] || printf '%s\n' "$bad_states" | sed 's/^/  ✗ 非法迁移状态：/'
  [ -z "$missing_state_rows" ] || printf '%s\n' "$missing_state_rows" | sed 's/^/  ✗ 映射行缺五态：/'
  bad=$((bad+1))
else
  ok "迁移状态 $state_total 行均属 new/retain/adapt/rewrite/retire"
fi

if grep -q 'A§' "$DOC"; then
  fail '新架构件含旧 A§ 引用'
else
  ok '无旧 A§ 依赖'
fi

echo '受检范围：冻结 SpecDoc 全部二级标题、冻结生产 Go 包目录、冻结原型 nav、合同 RT/IF 集合、接口 ID、N/A 理由码与迁移五态。'
echo '能力边界：本检查只判集合映射与 ID/理由码形状，不判设计语义。'

[ "$unknown_state" -eq 0 ] || exit 2
[ "$bad" -eq 0 ] || exit 1
exit 0
