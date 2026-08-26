#!/bin/sh
# 新版平台架构件的可复算形状检查。
# 能判：冻结树中的包/进程与原型导航是否在架构件有映射、接口 ID 是否唯一、必备章节是否存在。
# 判不了：分域是否合理、接口 effect 是否完整、引用是否引对义；这些仍归全文人审。
set -u

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
DOC=${DOC:-"$ROOT/docs/dingwei-platform-architecture.md"}
PROTO=${PROTO:-"$ROOT/docs/dingwei-prototype.html"}
bad=0

fail() { echo "✗ $*"; bad=$((bad+1)); }
ok() { echo "✓ $*"; }

[ -f "$DOC" ] || { echo "✗ 架构件不存在：$DOC"; exit 2; }
[ -f "$PROTO" ] || { echo "✗ 原型不存在：$PROTO"; exit 2; }

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

check_names() {
  label=$1
  shift
  miss=0
  for name in "$@"; do
    if ! grep -Fq "\`$name\`" "$DOC"; then
      echo "  ✗ $label 未映射：$name"
      miss=$((miss+1))
    fi
  done
  if [ "$miss" -eq 0 ]; then
    ok "$label 全量映射"
  else
    bad=$((bad+miss))
  fi
}

top_names=$(find "$ROOT/internal" -mindepth 1 -maxdepth 1 -type d -printf '%f\n' | sort)
# shellcheck disable=SC2086
check_names 'internal 顶层包' $top_names

mcs_names=$(find "$ROOT/internal/mcs" -mindepth 1 -maxdepth 1 -type d -printf '%f\n' | sort)
# shellcheck disable=SC2086
check_names 'internal/mcs 子包' $mcs_names

cmd_names=$(find "$ROOT/cmd" -mindepth 1 -maxdepth 1 -type d -printf '%f\n' | sort)
# shellcheck disable=SC2086
check_names 'cmd 进程' $cmd_names

nav_ids=$(sed -n '/<nav id="nav">/,/<\/nav>/p' "$PROTO" \
  | grep -oE 'href="#[^"]+"' | cut -d'#' -f2 | tr -d '"')
nav_total=0
nav_miss=0
for id in $nav_ids; do
  nav_total=$((nav_total+1))
  if ! grep -Eq "\`[^\`]*${id}[^\`]*\`" "$DOC"; then
    echo "  ✗ 原型入口未映射：$id"
    nav_miss=$((nav_miss+1))
  fi
done
if [ "$nav_total" -eq 0 ]; then
  fail '原型导航集合为空（不能把未读取判为全覆盖）'
elif [ "$nav_miss" -eq 0 ]; then
  ok "原型导航 $nav_total/$nav_total 已映射"
else
  bad=$((bad+nav_miss))
fi

ids=$(grep -oE '^\| `(IDN|GOV|WRK|KNO|CAP|COL|AGT|MSG)-(CMD|QRY|CAP)-[^`]+`' "$DOC" \
  | sed -E 's/^\| `([^`]+)`.*/\1/')
id_total=$(printf '%s\n' "$ids" | awk 'NF{n++} END{print n+0}')
id_dup=$(printf '%s\n' "$ids" | awk 'NF{n[$0]++} END{for(k in n) if(n[k]>1) print k}')
if [ "$id_total" -eq 0 ]; then
  fail '接口目录为空'
elif [ -n "$id_dup" ]; then
  echo "$id_dup" | sed 's/^/  ✗ 重复接口 ID：/'
  bad=$((bad+1))
else
  ok "接口 ID $id_total 项且无重复"
fi

if grep -q 'A§' "$DOC"; then
  fail '新架构件含旧 A§ 引用'
else
  ok '无旧 A§ 依赖'
fi

echo "受检范围：架构件 1、原型导航 $nav_total、internal 顶层包 $(printf '%s\n' "$top_names" | awk 'NF{n++}END{print n+0}')、mcs 子包 $(printf '%s\n' "$mcs_names" | awk 'NF{n++}END{print n+0}')、cmd 进程 $(printf '%s\n' "$cmd_names" | awk 'NF{n++}END{print n+0}')"
echo '能力边界：本检查只判集合映射与 ID 形状，不判设计语义。'

[ "$bad" -eq 0 ] || exit 1
exit 0
