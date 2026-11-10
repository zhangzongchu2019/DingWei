# MCP 一期飞书消息抄送

## Why

MCP 外部席位与其临时组目标之间的双向消息目前只进入 MCP 与 Hub 收件队列，授权人无法在飞书私聊中同步看到往来。按 rulings ㊿-补43，MCP 一期需要将每条往来作为只读提醒抄送给外部主体本人，并抑制飞书侧回复回流，避免通知形成消息循环。

来源：[团队迁移裁定](https://git.code.tencent.com/wego-ai/dingwei/blob/review%2Fopus50tc2k%2Frulings/docs/rulings/S-TEAM-MIGRATE-HOSTED-20261017.md)，㊿-补43、㊿-补44、㊿-补42、㊿-补65。基线为 r56.4 集成 `af752f641`，交付后纳入 r56.5。

## 待确认假设

- 飞书抄送内容上限采用 20,000 UTF-8 字节，低于官方 30 KB 消息体上限；最终上限若需调整，应保持只截断副本。

## 待 PM 决策

- 编号 PM-01：PG migration 编号已由组长按 ㊿-补65 分配为 `0111_extseat_feishu_copy.sql`；SQLite 0078 为备用，不使用。

## Scope

- 覆盖外部席位发往内部目标、内部目标发往外部席位两个方向。
- 复用 `extshared.ExternalSenderHeader` 生成服务端来源抬头。
- 飞书抄送独立截断正文；MCP、Hub 与 extseat 原消息正文保持完整。
- 默认开启，发送失败隔离并可重试，重复业务事件不重复创建抄送。
- 飞书侧回复不会回流 MCP。

## Out of scope

- 飞书回复转发、MCP 消息编辑或撤回同步。
- 改变 MCP 收发授权、临时组生命周期或主消息投递语义。
