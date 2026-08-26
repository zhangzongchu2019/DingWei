#!/bin/sh
# 新版平台架构件的可复算集合检查。
# 能判：四组预期集合是否被目标件完整、唯一声明；接口 ID 与 N/A 理由码形状。
# 判不了：分域是否合理、接口 effect 是否完整、引用是否引对义；这些仍归全文人审。
set -u

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
DOC=${DOC:-"$ROOT/docs/dingwei-platform-architecture.md"}
SPEC=${SPEC:-"$ROOT/docs/dingwei-platform-specdoc.md"}
CONTRACT=${CONTRACT:-"$ROOT/specs/platform-architecture/spec.md"}
PROTO=${PROTO:-"$ROOT/docs/dingwei-prototype.html"}
bad=0
unknown_state=0

fail() { echo "✗ $*"; bad=$((bad+1)); }
ok() { echo "✓ $*"; }

for f in "$DOC" "$SPEC" "$CONTRACT" "$PROTO"; do
  [ -f "$f" ] || { echo "✗ 依赖不存在：$f"; exit 2; }
done

T=$(mktemp -d) || { echo '✗ 无法建立临时目录'; exit 2; }
trap 'rm -rf "$T"' EXIT HUP INT TERM

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

# 1. SpecDoc 口径：所有唯一二级 § 标题 ID，标题文字不参与 ID。
grep -E '^## §[0-9]+(\.[0-9]+)?([[:space:]]|$)' "$SPEC" \
  | sed -E 's/^## ( §)?/\1/; s/^## //; s/[[:space:]].*$//' \
  | sed 's/^ *//' > "$T/spec.expected"
extract_block 'COVERAGE-SPECDOC-BEGIN' 'COVERAGE-SPECDOC-END' "$DOC" \
  | awk 'NF && $0 !~ /^<!--/' > "$T/spec.inspected"
compare_set specdoc "$T/spec.expected" "$T/spec.inspected" || bad=$((bad+1))

# 2. 实现单元口径：冻结树中含至少一个非测试 Go 文件的唯一目录。
find "$ROOT/internal" "$ROOT/cmd" -type f -name '*.go' ! -name '*_test.go' -printf '%h\n' \
  | sed "s#^$ROOT/##" > "$T/impl.expected"
extract_block 'COVERAGE-IMPLEMENTATION-BEGIN' 'COVERAGE-IMPLEMENTATION-END' "$DOC" \
  | awk 'NF && $0 !~ /^<!--/' > "$T/impl.inspected"
compare_set implementation "$T/impl.expected" "$T/impl.inspected" || bad=$((bad+1))

# 3. 原型入口口径：nav 内所有唯一 href fragment。
sed -n '/<nav id="nav">/,/<\/nav>/p' "$PROTO" \
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

ids=$(grep -oE '^\| `(IDN|GOV|WRK|KNO|CAP|COL|AGT|MSG)-(CMD|QRY|CAP)-[^`]+`' "$DOC" \
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

allowed_na='NA-READONLY|NA-INPROCESS|NA-IMMUTABLE|NA-NO-EXTERNAL-EFFECT|NA-NO-DATA'
all_na=$(grep -oE 'N/A\([A-Z0-9-]+\)' "$DOC" || true)
bad_na=$(printf '%s\n' "$all_na" | grep -Ev "^N/A\(($allowed_na)\)$" || true)
if [ -n "$bad_na" ]; then
  echo "$bad_na" | sed 's/^/  ✗ 非受控 N\/A 理由码：/'
  bad=$((bad+1))
else
  ok 'N/A 理由码均在受控枚举内（当前可为 0 项）'
fi

if grep -q 'A§' "$DOC"; then
  fail '新架构件含旧 A§ 引用'
else
  ok '无旧 A§ 依赖'
fi

echo '受检范围：SpecDoc 二级 § 标题、生产 Go 包目录、原型 nav 入口、合同 RT 主题、架构接口 ID 与 N/A 理由码。'
echo '能力边界：本检查只判集合映射与 ID/理由码形状，不判设计语义。'

[ "$unknown_state" -eq 0 ] || exit 2
[ "$bad" -eq 0 ] || exit 1
exit 0
