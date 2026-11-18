# 组到期而消息未到期：现行行为补核

状态：PG未投影层的常驻回归通过；已投影Hub队列存在既有继续投递边界，已先报组长，语义待裁定。未改生产行为、未把此处写成无条件安全PASS。

## 锚与来源
组长2026-11-18T17:33:05Z追加核项，收到时初交90c7ec3a已经推送。原冻结产品行为源2fd6aebacbb3dae3ac3fa82d84968367552151ca不变；补测源0b9a10f9227a35eaa01117cb767b9c883e6fb9ec仅追加internal/tempgroup/lifetime_expired_group_pg_test.go。原四个产品/测试文件字节未变，没有迁移或语义修法。

## 正常新请求的不变量与夹具界限
Send/EnqueueFromHelper按受理时刻t写message expiry=t+MessageTTL（inbox.go:77、156），在同一事务调用TouchGroup。tempgroup/service.go:161用GREATEST把group expiry提升到至少t+GroupTTL。新构建两者同168h，因此正常同构成功受理后group expiry>=该新消息expiry；不能据同为7天就断言正常新请求必然出现组先过期。旧expiry不回填也不等于禁止正常合法续组。

防御回归刻意在隔离PG夹具设独立存储的组expiry为day3，保留消息day7、token day14；这不是声称业务请求自然生成倒置，更不是生产库改写。提前主动解散同样会触发组不可用，但本格明确覆盖自然deadline判定而非仅dissolved标记。

## 按阶段的现行行为
| 阶段 | 组过期但消息仍有效的处理 | 对端可见性 |
|---|---|---|
| PG inbox仍queued/leased | authTx/ReadGroup先拒绝；不是在轮询里误记message_expired；消息扫描器只按自身expiry，组结算稍后将未确认行dead/group_unavailable | 外部Check、Wait、WaitReply、Receipt均ErrGroupDissolved，无摘要/正文/回执状态；有效token不能靠Authenticate续活组 |
| PG outbox仍queued、尚未投影 | ProjectOne先重核组，落dead/group_unavailable，不调用Projector | 不会向内部helper新增这条投影/正文 |
| PG outbox已经projected，SQLite session_delivery仍queued | 当前结算明确不回退已投影行；Hub发送前没有extseat AG/消息expiry旁证重核，原目标上线后仍可收到 | 真实PG+真实WS探针已证仍送达原delivery ID；不能把前两行结论外推至此 |
| inbox已received、helper已持久接收或历史已显示 | 本片没有撤回/抹除已交付内容的实现 | 不追溯删除已有内容；不把未投影拒绝包装为撤回已收消息 |

静态坐标（2fd）：tempgroup/service.go:96-107、161；extseat/service.go:234-248、321-334；inbox_controls.go:30-43、60-66；inbox.go:210-219、345-348、354-360、437-447；outbox.go:37-38、49-52、66-71；tempgroup/worker.go:11-43、46-83；cmd/workpulse/r53_extseat.go:164装配Finalize。自然到期立即拒绝不依赖每分钟扫描；通知/结算失败时待通知标记保留，并不重新授权外部轮询。如果组Finalize持续失败，inbox可能暂留不可领取的queued/leased；自身消息expiry到达后仍会由消息扫描器记message_expired。故group_unavailable是组结算在消息先到期前成功收口的死因，不把异步重试冒称同步保证。

外部wire呈现由源码核对（未冒称本补件另跑真实HTTP）：mcpgw/gateway.go:201将ErrGroupDissolved映射HTTP403、应用code1003、message=group_dissolved；REST:381-386，MCP当前codec:324-329及兼容codec2025.go:101-107在每请求鉴权时拒绝。请求进行中若重核到组过期，同样走mapped错误，不是空收件箱或假成功。token仍有效并不使组有效，且Receipt鉴权失败时不会泄露内部dead_reason。

## 已投影边界的两源实证与定性边界
外部消息在有效组内Send→真实ProjectOne/HubExtseatProjector写SQLite，目标helper当时离线；隔离夹具的组/消息创建时钟取现实时间前18h；投影后把独立组deadline设为创建后12h，再推进PG服务时钟18h，组已过期6h、消息在旧24h和新168h下均仍有效（探针显式断言），然后执行真实groups.Expire及seats.SettleDissolvedGroup，外部Check已拒；再连接原内部目标真实WebSocket并声明ACK能力，收到原ID，安全断言R62_PROJECTED_MESSAGE_DELIVERED_AFTER_GROUP_EXPIRY转红。

同一原样探针：2fd产品rc1，main206d41b26dab646c0e50a111eed5a45ccf7c72f6亦rc1，均非编译错或panic。初次较粗的期限夹具保留在initial-*，最终证据采用组expiry仍晚于创建时刻、并显式验证消息未到期的收紧夹具；两组不累计为不同缺陷或变异刀。这证明既有行为，不证明两TTL新增回归，不推算发生率。使用合成消息/身份、本地临时PG/SQLite/loopbackWS，不接在线席位或现网。探针未并入会把整包恒置红的常驻测试，只作为可复核Git证据文本。

机制：extseat/inbox.go:438注释已明确不回退已投影投递，:447只更新PG state=queued；m8/extseat.go:178只入SQLite通用队列，无AG/expiry旁证。hub.go:4913起drain，sub_agent.go:455的写前检查只覆盖现有outbound intent/subagent旁证，extseat该行没有outbound intent，outbound_recovery.go:205-208直接返回nil；相应生产文件main→2fd逐字节零差异。回退为旧常量不会补此边界。

组长待裁定：是否保留“合法投影即交给Hub既有可靠投递”的接管语义，并明确生命周期拒绝只针对尚未投影项；或要求对已投影但尚未发送的队列新增AG围栏/撤销机制（须稳定身份、代际与期限旁证，另批范围，不能声称能撤回已进helper的内容）。本补件不选定也不实施任何语义变更。

## 常驻回归与变异
TestR62ExpiredGroupCannotDeliverUnexpiredQueuedMessagesPG覆盖：过期时Check/Wait/WaitReply/Receipt/Authenticate拒绝且无正文；ProjectOne不调用Projector并正确dead/group_unavailable；message-only Expire不误判尚未到期inbox；真实group Expire→NotifyPending→Finalize完成后inbox终局；group/message/token期限全部不被拒绝请求重写。新增测试与全部TestR62实际-count=3：rc0、18顶层/45含子项PASS、0FAIL/0SKIP。

增量两刀单计（不与原两TTL旧值刀合并）：outbox组不可用死因改message_expired，编译0/业务红；组结算inbox SQL加false跳过更新，编译0/业务红；每刀原始/变异/恢复SHA及日志见group-mutations.json，恢复基线rc0。两源已投影红探针是边界复现，不计KILLED刀。

验证复用：原2fd四包三遍/两TTL刀/strict保留原锚原始读数，不冒称在0b9整包重跑；0b9生产文件及原测试文件均未变，新增测试以源码SHA绑定上述三遍读数。新SOURCE_COMMIT=0b9的完整canonical另跑，读数以canonical-followup.json为准；文档补记后strict另记。组长裁定前不修产品、不推main、不部署。

最终增量门实读：SOURCE_COMMIT=0b9的canonical完整rc0，Claude/Codex各1270/1270、inventory97，BUILD_TESTS_OK helper_profiles=2 python=27 shell=2；追加文档strict42/0。原三遍整包明确复用2fd，不冒称0b9重跑；summary.json分列绿色PG回归与两个源的红色已投影边界。
