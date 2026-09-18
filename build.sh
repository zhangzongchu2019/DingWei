#!/bin/sh
# 可复现构建脚本 —— 唯一允许的构建方式（T-070）
#
# 🔴 存在的理由：今晚实测,同一份源码在两台机器上能编出逐字节相同的产物,
#    但**线上跑的那份与我们编出来的对不上** ⇒ 线上二进制来源无法证明。
#    根因之一是构建方式散落在各人命令行里,参数靠记忆。此脚本把它固定下来。
#
# ⚠️ 只注入 commit,**不注入构建时间** —— 时间会让同源码每次产出不同二进制,
#    直接毁掉可复现性,而可复现正是本脚本要拿回来的东西。
#
# 🔴 -buildvcs=false 是跨机可复现的**必需项**(2026-09-11 两侧实测坐实):
#    Go 的 -buildvcs=auto 会在「能识别为 git 仓」时把 vcs.revision/vcs.time/vcs.modified
#    嵌进二进制,而**能不能识别取决于仓库是怎么取到的**:
#      普通 git clone   ⇒ 探测成功 ⇒ 带 vcs.* 条目(prodops 侧实测:vcs=git + 三条)
#      git worktree     ⇒ 探测不到 ⇒ **不带**(组长侧实测:vcs.* 条目数 0)
#    ⇒ 同一份源码、同一套工具链(compile/link/asm/go 本体四项哈希已逐字节核对一致),
#      仅因两人取仓方式不同,产物即非逐字节相同 —— 这正是跨机不可复现的根因。
#    来源已由 -X version.Commit 显式注入,VCS 戳是冗余的;
#    但它把产物绑在了「你是怎么 clone 的」这个环境细节上,正是可复现要消除的东西。
#
# ⚠️ 另:判脏用的 git status --porcelain 会被**本脚本自己的构建产物**污染
#    (默认 OUT=./bin,在仓内)。已把常见产物加进 .gitignore;
#    但若你把 OUT 指到仓内其它路径,仍会自伤 —— 建议 OUT 指向仓外。
set -eu
COMMIT="$(git rev-parse HEAD)"
if [ -n "$(git status --porcelain)" ]; then
  COMMIT="${COMMIT}-dirty"
  echo "⚠️  工作树不干净,commit 标记为 ${COMMIT}" >&2
  echo "    脏树产物**不得用于部署** —— 它无法被任何人重现。" >&2
fi
OUT="${1:-./bin}"
mkdir -p "$OUT"
for c in workpulse fcs file-service; do
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -buildvcs=false \
    -ldflags "-s -w -X github.com/zhangzongchu2019/dingwei/internal/version.Commit=${COMMIT}" \
    -o "$OUT/$c" "./cmd/$c"
done
echo "commit=${COMMIT}"
go version
sha256sum "$OUT/workpulse" "$OUT/fcs" "$OUT/file-service"
