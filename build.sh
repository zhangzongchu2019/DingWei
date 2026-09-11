#!/bin/sh
# 可复现构建脚本 —— 唯一允许的构建方式（T-070）
#
# 🔴 存在的理由：今晚实测,同一份源码在两台机器上能编出逐字节相同的产物,
#    但**线上跑的那份与我们编出来的对不上** ⇒ 线上二进制来源无法证明。
#    根因之一是构建方式散落在各人命令行里,参数靠记忆。此脚本把它固定下来。
#
# ⚠️ 只注入 commit,**不注入构建时间** —— 时间会让同源码每次产出不同二进制,
#    直接毁掉可复现性,而可复现正是本脚本要拿回来的东西。
set -eu
COMMIT="$(git rev-parse HEAD)"
if [ -n "$(git status --porcelain)" ]; then
  COMMIT="${COMMIT}-dirty"
  echo "⚠️  工作树不干净,commit 标记为 ${COMMIT}" >&2
  echo "    脏树产物**不得用于部署** —— 它无法被任何人重现。" >&2
fi
OUT="${1:-./bin}"
mkdir -p "$OUT"
for c in workpulse fcs; do
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath \
    -ldflags "-s -w -X github.com/zhangzongchu2019/dingwei/internal/version.Commit=${COMMIT}" \
    -o "$OUT/$c" "./cmd/$c"
done
echo "commit=${COMMIT}"
go version
sha256sum "$OUT/workpulse" "$OUT/fcs"
