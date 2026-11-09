## 设计

`ProjectOne` 在 PG 事务中锁定并读取来源与目标身份快照。Projector 仅消费该快照，不额外连接身份数据库；抬头通过单一格式常量与生成函数形成，字段来自 Delivery.SourceAgent、source owner 快照及目标 Agent。Envelope 的 `origin_kind=extseat`、`source_display_name`、`source_external=true` 只在 Hub Project 处写入。

抬头不包含动态时间；投影继续按 delivery ID 对比持久化 envelope 的完整 JSON 字节，重投复用已生成的稳定内容。普通 session ingress 若携带伪造 extseat meta，由 Hub 清除该来源标记。

回信使用用户可见全名解析目标 Agent。Hub 对已解析的外部席位先执行现有 `routeExternalSeat` 授权与入队路径，再进入一般编组门；外部席位收件仍由 extseat 服务负责。

View 侧代码当前没有依据 `origin_kind` 或 `source_external` 标记来源的来源展示组件；本变更不新增 UI，接收端识别通过 envelope meta 与抬头完成。

## 约束

- 不改变身份模型或外部 Agent 身份生命周期。
- 身份无法在 PG 事务快照中验证时拒绝投影。
- 现有 GetSessionDelivery 同 ID 内容核对保持不变。
