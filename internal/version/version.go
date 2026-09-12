package version

const Version = "0.2.0.0"

// Commit 是构建时注入的源码提交号,用于回答「线上跑的这份是哪份源码编出来的」。
//
// 🔴 为什么不用 Go 自带的 VCS stamping:实测本仓 `go build -buildvcs=true`
// (含去掉 -trimpath 重试)**均未产出任何 vcs.* 条目**,`go version -m` 读不到来源。
// 故改为显式 -ldflags -X 注入,不依赖 Go 的自动探测。
//
// ⚠️ 这里**只注入提交号,不注入构建时间** —— 时间会让同一份源码每次编出不同产物,
// 直接破坏可复现构建(而可复现正是我们要拿回来的东西)。
//
// 未注入时保持 "unknown":**宁可显示不知道,也不显示一个可能是错的来源。**
var Commit = "unknown"
