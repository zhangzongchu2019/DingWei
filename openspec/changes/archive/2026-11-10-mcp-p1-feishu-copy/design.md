# 设计

## 投递边界

主消息的 PG 入队与飞书抄送记录需在同一业务事务内提交。飞书 API 由独立 worker 发送；单条抄送的配置、映射或网络失败不得回滚、阻塞主消息。worker 以稳定事件键领取并标记结果，重启后可继续处理。

## 收件人和来源

抄送收件人是外部席位的授权人本人。通过外部主体的 issuer/subject 与身份绑定確認 owner，再按同一飞书应用映射 OAuth issuer 到 Bot issuer；禁止按显示名、短名或跨主体猜测。来源抬头调用共享 `extshared.ExternalSenderHeader`。

## 消息内容

只在生成 Feishu copy payload 时按 UTF-8 边界截断，副本上限 20,000 字节，低于飞书消息 30 KB 上限（[飞书通用错误码](https://open.feishu.cn/document/server-docs/api-call-guide/generic-error-code)）；保留原正文不变写入原有收件队列。抄送格式包含来源抬头与目标标识。抄送消息附只读提示；入站回复事件识别该提醒来源并停止回流 MCP。

## 幂等与隔离

每个原始 delivery/message 使用稳定 copy event ID，PG 唯一键保证重复入队不产生多条副本。发送端使用派生 request ID；投递状态及退避独立于 MCP outbox/inbox。失败记录不修改主消息状态。

## 开关

MCP 飞书抄送开关默认开启；显式关闭时不创建或发送副本，但双向 MCP 消息行为维持现状。
