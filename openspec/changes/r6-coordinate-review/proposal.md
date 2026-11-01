# Proposal

## Why

旧 Agent 派审携带数据库正文快照，不能沿用到 Git 管理的 OpenSpec，否则既可能超出消息体积限制，也可能借平台服务账号泄露私库内容。新派审需要只发固定坐标、由审阅主体独立获权读取，并把结论与被审版本绑定，使超时和改动后重审都可核查。

## What Changes

- 派审内容只包含仓库、变更编号与完整 commit SHA；投递路由/关联元数据不携带正文、补丁、附件、下载能力或凭据。
- 发起者与 Agent 独立核权，派审不授予仓库权限；接收方按指定 commit 和交接顺序读取。
- 审阅结论及 UTC 时刻绑定审阅人和该 commit；多人逐人展示缺审、超时提醒，实质修改后标「针对旧版本」并提示重审。

## Capabilities

### New Capabilities

- `console-coordinate-review`：新增 R6-001～008；最小基线/context 明确旧 snapshot 派审不等于坐标派审。

### Modified Capabilities

无。

## Impact

仅规格。未来涉及 Console 派审页面、Git 授权/坐标适配、审阅记录、提醒与审计。r44 定义基本派审和超时提醒；r46 的自动跨组负责人发现、复杂催办和升级编排不在本单实现。无源码转发、Git 写入、旧 Spec 新功能、任务锁、合并门或部署动作。

## 来源

- 委员会[全面切换决议](https://git.code.tencent.com/wego-ai/dingwei/blob/c2d9c9fdd0416828739f7042d2ceb9bc71f603c6/docs/decisions/OPENSPEC-SWITCH-DECISION-20261031.md)第 3、4、15、21 条（读取顺序补充依第 19 条）。
- [本月计划](https://git.code.tencent.com/wego-ai/dingwei/blob/edadb623f0bad3b64b3bdc53a297c8d0c3de05bc/docs/plans/DINGWEI-PLAN-2026-11.md)第 2、3 节；G2/G1；按组长本单仅提取已裁定的 r44 R6/R7，计划其它草案不转为授权。
- 源码基线 d1a7a686110c9721c61f403bb9cac4cae0b694ee；R11 实现分支不动。规格场景是未来验收依据，尚未执行产品验证。

## 待确认假设

- R6-A01 prodops 核实发起者与 Agent 的可信身份、各自仓库 read 查询和指定 SHA 可读性；不能从同 owner、同项目或服务账号权限推导获权。
- R6-A02 指派人名单由获权发起者提供；自动跨组/跨仓发现在 r46 独立变更；本派审提醒不升级给组长。本单的 Console 仍须逐人显示待审、完成和旧版本，不能合并成虚假的「所有人已审」。
- R6-A03 沿用 Hub 可信路由与持久投递作为传输，不另开连接；具体请求/回执字段、去重持久化与提醒执行机制待实施设计核实，不冒称现有旧协议支持新语义。

## 待 PM 决策（执行口径已定，裁定出处已核）

- R6-PM01：沿用原建议及当前R7已批A-conservative-exemptions-v2：tasks勾选、交接记录节、内容不变archive移动三类豁免，其余受控内容变化保守判实质；不能只因SHA变动失效，也不能把原结论改绑新SHA。来源为下引第⑯条业主“其他按建议推进”；R7-PM01按原建议执行是该条明确记录的**组长解读**，不是补造业主逐字列出A策略的原话。本单不改R7目录/政策。
- R6-PM02：下引第⑯条记录的业主原话为：「不升级，但，可以在console页面中给出一个待办任务提醒（提醒到阅读），如果这个提醒30分钟没有被打开，就发送到外接的飞书/slack/企微/google等渠道提醒（至多3次，时间间隔1小时）」。执行上Console待办到被指派主体阅读/打开，30分钟未打开首催、最多3次、不升级；**首期只用现有飞书私聊、把“时间间隔1小时”按“至少1小时”实现，均为同条组长注/解读**，不作为业主原话。任一次真实打开停止后续外发，打开不等于已审/通过；三次耗尽但未打开时Console待办仍在。

### 裁定出处核查记录（补记后已核）

已独立fetch工蜂review/opus50tc2k/rulings，完整tip **916a9dff34cbf53905bfa31305677d5a79221978**。固定出处：[docs/rulings/S-TEAM-MIGRATE-HOSTED-20261017.md 第⑯条](https://git.code.tencent.com/wego-ai/dingwei/blob/916a9dff34cbf53905bfa31305677d5a79221978/docs/rulings/S-TEAM-MIGRATE-HOSTED-20261017.md)，Hub收信2026-11-01T02:23:43Z，delivery dw-dlv_cb96858f21fea86861ffccfa44c3b5bc；该条由组长于2026-11-01T15:19:01Z补记，另含02:26:41Z补充。本次R6-PM02依据02:23:43Z原话；R7-PM01依据同条“其他按建议推进”及其组长解读。私聊首期范围与至少一小时间隔的解释归属已在上文分别标明。

此前所核rulings25a9a441和提案分支6a204818没有此直接记录，70c6367e中的出处待核属于当时状态；现在以本固定第⑯条替换当前出处判断，不将早先本席41f10f48的转述提升为业主原话证据，也不改变已审通过的提醒/依赖内容。

## T-03 依赖事实回写（已核与未就绪分开）

固定事实：[prodops R5只读实测](https://git.code.tencent.com/wego-ai/dingwei/blob/e8b14b69b65cbd2a9fee246d9866c0f96168228a/docs/reviews/prodops-r5-facts-20261101.md)，2026-11-01T08:37～08:43Z；实现源R5 5a9cafddf29e38ea617d13f2092f3730c93a88a5。前者是prodops现场报告，本席仅独立Git取件/对照代码，未重做真实API或数据库采样。

- R6-A01已核底座：GitHub仓zhangzongchu2019/dingwei-dev，稳定ID1441460280；GET /repositories/{id}核返回ID，GET collaborators/{login}/permission查指定用户有效read，ref解析完整commit后固定tree/blob；不能从服务账号admin或用户名相似推用户/Agent授权。
- 原文可读不等于任意版本可用：实测main=6ed4d1e86795569e33cf9de2ba03988699c2a830没有openspec，读取404；规格分支68c178a5a9ce7c4cc4df61036c713da380e137e7有openspec。不得悄悄回退该分支或HEAD冒充所派C。
- 实测recursive=1返回truncated=false、无分页；截断必须完整逐级子树回退，不能用局部tree。raw Contents读取proposal为200/5947字节；R5实现另选Git blob base64并校验Git blob摘要/长度，这个实现选择不是prodops已测raw端点的同一证据。symlink=blob/120000、submodule=commit/160000为报告引用的类型规则，目标树没有这些对象；实现拒绝不跟随，不能称生产做过正向链接实验。
- 实测速率core.limit=5000、remaining=4995（采样时刻读数），不是固定容量承诺；实现逐请求读Remaining/Reset、Retry-After及二级限速，遇限流/权限未知不放行。报告未审计令牌完整scope，不能据metadata/read响应证明令牌仅只读或可写MR。
- 已有R5代码：internal/specread/service.go的Service.Read与Coordinate、Binding/Provider.Authorize；先核当前绑定/仓库read、固定完整SHA并受限读取，交付前再核绑定和read；github.go落实受控HTTPS GitHub适配。单文件2MiB、单API10秒/整次15秒、并发4、响应8MiB/tree10000项/回退至多256请求均是实现保护值，不是提供方实测容量。
- 凭据仅记录管理位置/etc/dingwei/credentials/github-dingwei-dev.pat及R5服务端credential-file配置，不读取值、不传Agent；DW_OPENSPEC_READ_ENABLED默认关，真实T-08/绑定/逐主体验收未完成不叫可用。Console principal↔GitHub账号可信绑定在报告时缺失，受派Agent映射与各自read接线也仍需事实验证；R5已有接口不能替代该缺口。
- 工蜂只有Git只读SHA对照事实，专用只读账号/令牌及稳定ID、逐用户权限/tree/原文/速率接口仍不可用；R5工蜂适配失败关闭，不另开SSH/raw内容通道或向消息/文件服务补件。R6首期实际范围限R5能完整核读且主体获权的GitHub仓，不能从本提案推在工蜂的交件行为推出产品工蜂读取已可用。

T-03勾选仅表示本次依赖/PM口径的文档回写完成；真实映射、提供方/渠道接线及实施前核验仍是T-04以后前置，不因勾选全部变成已满足。

## S-R6-IMPL待确认假设（不代替T-07）

- 既有owned-Agent资格用于首期派审，Agent独立Git账号必须管理员可信映射；私聊对应该owned Agent的owner已验证且唯一的Feishu身份，缺失则失败可见、不猜收件人。该真实主体/渠道映射需独立验收确认；不以本地夹具认定已具备真实权限。
- R9尚无运行时，已提供不可变共享事件归属和双候选冲突测试；将来R9必须接同一`ClaimNotificationEvent`，不得另造事件ID规避统一所有者。R9停止条件及PM待决不在本单改动。
- 增量规格与rulings口径未改。T-04～T-06实现随r47且默认关闭；真实工蜂API、R5上线前置和T-07/T-08仍未满足，不能据构建通过启用。
