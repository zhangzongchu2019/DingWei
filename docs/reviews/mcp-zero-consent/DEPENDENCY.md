# 拼音依赖离线核对

基线go.mod已依赖github.com/mozillazg/go-pinyin v0.21.0；本变更不添加或升级模块。缓存路径由go env GOMODCACHE实取为/home/dev/go/pkg/mod，对应目录存在；LICENSE首行The MIT License (MIT)，2016 mozillazg。固定使用该版本默认首读音的FirstLetter样式，逐字取首字母，因此重庆单行的固定结果为zqdx；不作词语语境推音。

GOPROXY=off下实际go test与race、隔离Hub E2E编译通过；最终原文见raw下相应日志。无法获取姓名时使用pm；旧手填登录的DisplayName校验保持原样，仅自动命名允许可信空姓名使用pm。tenant/open_id缺失仍先拒绝。
