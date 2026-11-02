# R9 技术前置事实（基线 r48）

核对对象固定为 `1e17255f28ced042784337903914ca8c126ef4cf`。下列文件:行均指该基线。只核现有代码和数据模型，不做真实飞书或GitHub探测；旧澄清能力不等同于R9决策请求已接线。

| 编号 | 判定 | 基线事实与证据（文件:行） |
|---|---|---|
| B01 单PM身份映射 | 部分具备 | 新平台Console会话解析出 `identity.User.UserIDLC()` 作为平台principal；新平台Feishu映射用 `(provider=feishu, issuer=bot channel, subject=open_id)` 查身份表。`internal/m8/newplat_view.go:223-244`；`internal/m8/newplat_feishu_inbound.go:12-18`。`np_identity(provider,issuer,subject)->np_user.user_id_lc` 有唯一映射；`np_user.type` 仅platform_admin/manager/member，无全局PM类型。`internal/identity/pgschema.sql:6-25`。既有澄清的PM归属是每Spec `spec_clarification_policy.pm_user`（及deputy_user），不是跨Spec角色；具体私聊回应还要求身份映射用户等于通知记录PM。`internal/store/migrations/0061_spec_clarification_thread.sql:3-14`；`internal/m8/spec_clarification_reply.go:190-215`。另有历史Console principal表 `console_principal`，独立于新平台 `np_user/np_identity`。`internal/store/migrations/0033_console_auth.sql:1-18`。故单Spec PM关联素材已具备，但R9全局唯一PM目录/三者统一绑定并不存在，需明确R9采用新平台principal并确保其与每点PM字段一致。 |
| B02 可信飞书私聊入站与防伪 | 部分具备 | Feishu长连接由SDK使用配置的AppID/AppSecret建立，收到事件后把sender open_id、chat类型、message ID及可信transport provenance写入内部消息。`internal/feishu/feishu.go:314-331,444-485`。现有Spec澄清私聊回复只接受authenticated native metadata、配置的bot channel/AppID/tenant、`IngressFeishuWSAuthenticated`；回复还须个人会话、用户文本、原通知parent及消息ID关联有效。`internal/m8/spec_clarification_reply.go:41-59,101-145`；`internal/store/spec_clarification_inbound.go:34-45`。处理时重新按channel+open_id映射当前active用户，并要求其UserID等于通知所记PM。`internal/m8/spec_clarification_reply.go:186-217`。Webhook verifier 将未验证来源标为 `untrusted`，无签名明文Webhook为兼容行为仍可接收，但不得被认证路径采纳。`internal/fcs/webhookverify.go:38-56`。这些检查能为复用该链路提供可信身份/关联基础，但当前仅用于澄清回复，没有R9 PM决策handler、决策代际绑定或跨渠道关闭逻辑；所以R9路径本身未具备，不能把普通消息正文/昵称当作可信PM答复。 |
| B03 Outbox、去重键、周期与重启 | 部分具备 | 旧任务通知有SQLite持久outbox，键由task/assignment version/event/recipient/role组成，并以唯一约束防重复入队；扫描未派发项目并标记完成。`internal/store/console_task_flow.go:84-93,353-394`。Hub启动期任务循环每5秒重试dispatch和各reconcile工作。`internal/m8/console_task_flow.go:438-460`。旧PM问题/澄清也有持久outbox和事件/请求唯一键：`pilot_question_outbox`含状态与provider message ID，按question/generation/kind/recipient/event version唯一；事件/答复有唯一键。`internal/store/migrations/0059_pilot_questions.sql:28-56`；`internal/store/migrations/0062_spec_clarification_transport.sql:24-38`。这是可复用的持久化/去重模式，且现有提醒仅为每代唯一一次`remind_60m`；不是R9规定的24小时首催及每6小时重复提醒周期。`internal/store/migrations/0059_pilot_questions.sql:46-56`。Hub ticker自身是进程内定时器；持久业务due/outbox可让既有流程重启后重新扫描，但不存在R9周期状态机或R9专用幂等周期键，不能称R9周期已持久实现。 |

## 结论与边界

B01～B03的基线事实盘点完成，均为「部分具备」，可关闭T-03.2核实子项；R9实现前需明确PM权威字段与身份映射、把可信私聊回复核验接入R9请求/代际、为R9的请求/提醒建立独立持久状态及按24小时/6小时运行的周期唯一键。PM01已定的平台记录、MR后置只表示首期不请求MR写权限；本文不执行真实私聊，不验证当前飞书服务可达性，也不声称完整双渠道闭环已就绪。

## S-R49 实施设计交接（仍未实现）

设计把逐point唯一负责人的规范字段定为R9 responsibility mapping中的 `pm_user_id_lc`，必须对应Identity PG `np_user.user_id_lc` active用户；Console session的 `identity.User.UserIDLC()`须匹配该值，飞书 `(provider,issuer,subject)`解析到 `np_identity.user_id_lc` 后也须精确匹配。既有 `np_user.type`没有PM枚举，`spec_clarification_policy.pm_user`仅是旧Console Spec每Spec配置，不能自动作为R9映射；不得从Git作者、显示名或proposal自由文本猜PM。映射写入来源/谁有权修改在现有代码中未知，设计列为组长待定。证据：`internal/m8/newplat_view.go:223-244`、`internal/m8/newplat_feishu_inbound.go:12-18`、`internal/identity/pgschema.sql:6-25`、`internal/store/migrations/0061_spec_clarification_thread.sql:3-14`。

设计拟用0074新增责任映射、request、append-only response、跨渠道delivery/outbox、Feishu inbound去重键和审计表；request与两个渠道待办同事务，提醒due/period持久化，重启扫描恢复；唯一键防同一请求、事件和提醒period重复。可信回复只接已认证native WS、原始personal reply、可核provider parent message与当前request generation，沿用App/tenant/PM/current read复核。基线支撑与限制见 `internal/store/migrations/0062_spec_clarification_transport.sql:11-38`、`internal/m8/spec_clarification_reply.go:41-59,186-217`。

待组长定：谁是每point PM映射的权威配置者/导入源；映射新增修改是否采用现有R7责任映射或新增受控配置入口。PM的身份主键、Console和Feishu映射链及防旧代际规则已在本设计写明。此设计不表示真实飞书通道或周期闭环已运行。
