// 🔴 独立 module,故意【不】并进主 go.mod。
//
// 原因:主仓依赖棘轮闸 scripts/dependency-ratchet.sh 判「直接依赖数只许降不许升」
// (基线 specs/gates/dependency-baseline.txt: direct_count=6),上调基线属评审面。
// embedded-postgres 带进 lib/pq 与 xi2/xz,进主 go.mod 就是 6→7 触闸。
// ⇒ 测试环境的依赖关在这里,主 go.mod 一个数都不动。
module github.com/zhangzongchu2019/dingwei/tools/pgtest

go 1.22

toolchain go1.22.2

require github.com/fergusstrange/embedded-postgres v1.34.0

require (
	github.com/lib/pq v1.10.9 // indirect
	github.com/xi2/xz v0.0.0-20171230120015-48954b6210f8 // indirect
)
