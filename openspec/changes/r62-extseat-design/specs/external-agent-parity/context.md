# external-agent-parity 上下文

本 change 是设计，不是产品实现。基线产品main206d41b26dab646c0e50a111eed5a45ccf7c72f6；原设计124d24ee3818762f977920d5a015e4dfdca80663。本次按S-R62-EXTSEAT-AMEND和rulings148f1be7/㊿-补132③修订，TTL冻结3639406c独立保留。

- 当前开户必建AG：internal/extseat/service.go:141起经CreateTempGroup回调建身份与AG关联；internal/extshared/extshared.go:62起现有名字编码含AG。无配对开户是切片三目标，需要解耦，当前不能声称已支持。具体无AG新地址/兼容方案实现前冻结，旧地址不重写。
- 临时组在internal/tempgroup/service.go:71起还建np_group/member可达性镜像；并存授权不得将过期AG镜像当作独立普通组。
- 拒绝映射复用internal/mcpgw/gateway.go:201-203：group_dissolved针对已有组失效；无配对且无普通组用not_permitted，HTTP403/协议1003。不新增工具、scope或wire实现。
- design.md定义并存路径、无组入口/显示、PM01与copy默认；spec.md对应EAP-003/004/007；docs/design/R62-EXTSEAT-SLICES.md同步未来迁移槽、回退和验证。所有新产品接口/DDL仍需独立实现派单。
