# S-SPEC-CLARIFICATION-THREAD · 一页设计（待审，docs-only）

依据第89条 `3f8d4fcde5747b5eb00bef403936a965d110fd85`；基r33 `ab0f214496e6a5df1c71826cc978f40528923b8f`。组长审＋prodops交叉核，批准后实现；真实飞书仍pending_config，启用另批。**PM三步：收到问题 → 选解释／需改spec并答复 → 看入帖或失败回执。**

**1．spec下的帖子。** 复用D3问题主题，每条提问／解释／技术说明固定`spec_id/revision/SHA、question_id、entry_refs[]、同spec递增楼号N、作者/角色/来源/时间/证据`。条目／子例ID须在该版本存在；旧文档无ID时用只读快照侧表ID（SHA＋位置），歧义拒绝，不改final。证据可空但明确未提供。正文、身份、锚点只追加不改不删；更正须新帖`corrects=N＋理由`，仅指同spec同版本既有帖，原文留存。PM追认代答也新增帖。D3 resolved不封死讨论，续帖不自动重开任务；归档只读。后继spec链接旧讨论，不自动继承解释；研发／验收可引用具体楼号，新更正通知相关人重核，不自动完成或签收。

**2．先分权威，再分类型。** owner显式配置PM人类账户；PM事先指定零或一位不同的人类代答人，配置／撤销追加审计、立即影响后续写入，不能追授历史身份。owner/editor/负责人/leader不再自动有权威答复权，未兼任PM／代答时只按既有权限提问或补材料。Agent一律“技术说明（非权威）”，同owner也不例外。代答即时有效但显著标“代答，PM未追认”，PM可再确认或更正；未配置PM不回退到owner。

| PM／代答先选 | 系统处理 |
| --- | --- |
| 解释 | 显式声明不改范围，追加回复；final正文／SHA／验收标准不变。有人追加异议则相关问题待澄清，PM更正N或转变更；系统不假称能自动识别自然语言中的隐性范围变化。 |
| 需改spec | 追加“变更提案，尚未生效”，同事务建D3 pending change，沿D1 Derive＋D3核真实同owner派生后继；保留旧任务变更门，不得用取消问题／解释清掉提案。 |

**3．飞书方案与准确归属。** 首期用D3机器人单聊文本通知，提示直接**回复原通知**：`解释：条目/子例ID 正文`（通知明确此提交即声明不改范围）；更正附`更正N`。需改spec给手机Console链接，在那里派生、核差异，不做多步卡片回调，避免一句话批准范围。首期飞书仅PM本人，代答用Console；不把PM Agent或共享owner的飞书身份当本人。通知只带问题号、版本、必要条目ID及登录链接，不外发正文／私密证据。格式不明、离线、未绑定、过期都明示失败；成功持久化后回“已入第N条”，回执发送失败不重写帖子。

r33 `questionMessage`尚声明不回写，`model.Message`／FCS信封也缺回复父消息。须补认证入站→持久队列→业务路由全链的`event_id/message_id/parent_id/root_id、message_type、tenant/app/channel/chat、sender_type/open_id`。出站持久化`provider_message_id→通知ID/问题代次/spec版本/PM/政策与绑定版本/7日有效期`；入站只接受text、人类sender、同应用租户的单聊，**parent精确匹配原通知**，不以root／转发／正文问题号替代。消息中的open_id经PG当前绑定唯一映射至仍active的该PM，重核当前政策、绑定及访问权；不能信显示名或客户端actor。

**4．可信入站、竞态与重放。** 仅接既有SDK认证WS；FCS须验证服务证明、租户、key epoch并完整携带元数据，缺能力保持pending_config，fake／legacy webhook／普通JSON／Agent输入不能冒充。首期不另开HTTP旁路。事件先可信持久收件再处理；双唯一键`(issuer,tenant,event_id)`、`(issuer,tenant,message_id)`＋规范化payload SHA，同键异文拒绝，同文仅返原楼号。PG持有效身份/绑定锁→SQLite事务重读政策/代次/版本→写帖/审计/D2输入与outbox→提交→释锁；改PM、撤代答、解绑、移组竞态要么先完整提交，要么拒绝；缺行/错误失败关闭，无跨库两阶段提交。回复早于出站Finish则待精确匹配最多24小时，匹配时重核7日效期和权限；Finish失败／unknown不猜映射、不盲重发，不能证实则告知未入帖、转Console。已匹配通知或专用答复格式的消息由此通道消费，错parent／格式错不交LLM猜题；其他聊天不受影响，日志不记正文和私密身份。

**5．D3／D2与三处展示。** 统一收窄`questionAnswerer`及旧HTTP／Agent HTTP／MCP／store直调；旧`substantive`不可绕过类型与锚点。复用D3不可变answer正文，新表只添线程元数据／楼号，不存两份可分叉正文；同一SQLite事务写答复、元数据、审计、D2唯一wake。保留每卡每actor256条／每60秒10条限额，失败整笔回滚。技术材料只触发D2条件重核，不能算业务权威首答／解除范围门或自动签收；沿用60分钟一次提醒、unknown不重试。spec页按版本分页显示问题、楼层、角色、更正链、后继；任务页显示关联主题和引用楼号；project_context在原授权／预算内附clarifications，不作指令。spec读者可读新公开帖；PM／代答的显式问题授权只给该主题／必要条目快照，不扩整spec；Agent仍按R9当前角色/成员重核。旧私有D3历史不扩ACL。Console用active人类cookie＋Origin；飞书由可信适配器生成内部身份，不提供可传open_id的免登录写口。

**6．迁移、版本与回滚。** 预留SQLite `0061_spec_clarification_thread`（集成前核号，schema_migrations）：追加政策历史、回复元数据/锚点/楼号、通知关联与可信收件/幂等账；不改0059枚举，不改PG schema。旧答复原样标“历史答复，权威性未核”，不按今天角色追授；PM可补锚点追加确认。保留旧pending change、阻断、unknown，不追发历史提醒；旧通知无映射不接回复。helper/MCP补类型/锚点schema，同批统一钉号（建议0.2.3.8，排期另批）；旧版只读兼容，缺字段写明确拒绝/提示升级，不走旧宽权限。

`DW_SPEC_CLARIFICATION_ENABLED`、`DW_SPEC_CLARIFICATION_FEISHU_REPLY_ENABLED`默认关；飞书还须业主批准D3出站、真实绑定、完整入站能力。线程开关开启时旧写口同步收窄，关闭只冻结写，**不恢复旧权限**；已知线程回复关闭后不落普通路由，其他聊天照旧。回滚先关飞书收发/线程写、停worker，冻结旧D3/D2/D1关联写路径，备份DB/收件账/outbox，保留新表只读；有写后r33旧二进制不得直接写库，仅允许冻结回读。兼容修复版复验后另批开放，不删帖、不重放unknown。

**7．验收与撤修必红计划（尚未运行）。** Console PM／预指定代答、飞书本人解释各一帖一wake，追认/更正保留旧文；负格：①旧宽角色/同owner Agent冒充；②撤权/改PM/解绑/移组与写并发；③错锚点/版本/跨spec更正/改删；④解释改final、绕变更门、假后继；⑤假入站/错租户应用/群聊/错parent/仅root/伪造sender；⑥双ID重放/同键异文/重启；⑦回复先到/Finish失败/过期/旧通知；⑧审计/D2/限额失败零残留；⑨三处越权/游标漂移/已完成任务不重开；⑩关开关/旧helper/真实旧r33冻结回读与恢复演练。实现交件需全仓Go、vet、canonical、隔离HOME helper、浏览器、race及指定红证据；真实飞书只在另批窗口验收，不能以夹具替代。

只读核对：r33 `internal/store/pilot_question_types.go`、`pilot_question.go`，`internal/m8/pilot_question_worker.go`、`internal/feishu/feishu.go`、`internal/fcs/contract.go`；锁定SDK v3.9.7确有ParentId/RootId与发送返回MessageId，但本项目尚未贯通。[官方接收消息](https://open.feishu.cn/document/server-docs/im-v1/message/events/receive)、[官方SDK](https://github.com/larksuite/oapi-sdk-go)。
