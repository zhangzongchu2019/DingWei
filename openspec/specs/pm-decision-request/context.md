# PM决策请求上下文

## 代码位置与最小现状

- 固定源码2af469dd6919d8eb6064aa7da0e646f0851becf8，未作线上实测。
- internal/feishu/feishu.go：既有Gateway/OutMessage、personal发送与RequestID、Receiver入站；pilot_question.go有私聊收件人与稳定ID约束，spec_clarification.go的ReplyApplication仅对有可信能力的transport证明入站应用。复用通道不等于新R9已接线。
- internal/store/spec_clarification*.go、console_spec_pm_flow.go与对应internal/m8路径：旧数据库Spec澄清/PM流程，不具有新Git point/commit代际契约；不以旧问答记录填充新PM点。
- internal/specread：R5固定commit逐主体只读；无MR写评论能力，不从只读令牌推导写权限。
- internal/changeflow/types.go的PMResponse/Snapshot.PMKnown与lifecycle.go守卫消费明确回应事实；真实GitHub/GongFeng Provider.WithAccess仍返回不可用占位，存在Reader.UpsertComment签名不证明真实写回可用。
- 新PM点解析、双渠道同请求、内容指纹/代际、超时催办与MR回写策略均为本变更提议，未实现。

## 环境与配置

沿现有飞书应用/私聊通道配置与受控身份绑定，不新建群通知。群通知按本次派单仍挂起；不依赖Slack。凭据仅说明受控管理位置，真实scope/位置由prodops核实，不读取值。OpenSpec1.16.0、Node≥22.12、telemetry0仅执行席/CI。

## 依赖

上游R5只读/主体仓库read、可信PM及飞书身份映射；R7不可变版本原则；下游R8消费PM完整集合与回应，MR写回需要额外提供方comment-write权限。来源链接集中proposal，平台能力时间线不假定已上线。

## 负责组

Console开发coder56solzzc.zzc单写；组长独立评审和报PM；prodops核实际通道/权限/运行依赖；质量保障组后续逐格验收。
