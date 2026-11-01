# Design

## Context

最小基线/context 区分通用 Risk 与新能力。需求来源限定 proposal 固定文档及本单补充；七类和三路已派定，草案匹配参数及外部接口仍需冻结。本轮只写规格。

## Goals / Non-Goals

- 覆盖 R1-001～008：可重放识别、双端提醒、负责人知悉、追加版本、非阻塞和R9边界。
- 不迁移仓库，不创建令牌，不接真实MR/Console，不实现R9或新的合并审批门。

## Decisions

1. **纯判定与适配分离（R1-001～003）。** 提供方先核当前主体/受控仓库，取得固定base/head的完整diff、标签快照及同版本仓库规则。逻辑判定输入为 diff＋labels＋规则版本；规则版本是随请求提供的不可变规则包，不在纯函数里按版本号读可变注册表。包身份包含平台默认表版本、仓库规则blob、匹配语义版本。返回去重有序类别与规则依据或无法判定，不读时钟/网络、不投递。
2. **规则可复现（R1-001/002）。** 标签精确分类；路径路线包括修改、删除和重命名涉及的规范仓内路径，重命名旧/新路径均交冻结匹配语义判断。关键字只来自真实文本新增行，排除文件头、删除及上下文；已声明二进制文件没有文本新增行，不影响标签/路径路线。不完整取件或解析失败是无法判定，不能当零命中。YAML schema、glob/大小写、平台/仓库规则如何合并、上限待R1-A03核准；本单不交付生效配置。
3. **七类候选默认表（R1-001/002）。** 下表来自固定草案，用于规则评审及21格正反样本，不能把自然语言类别说明直接当成已实现算法。

| 标签 | 草案默认路径示例 | 新增行关键字/模式示例 |
| --- | --- | --- |
| risk:funds | cmd/workpulse/hosted_quota_routes.go；业务计费/支付/结算/配额目录由各仓补 | 金额、计费、扣费、退款、结算、price、billing、quota |
| risk:permission | internal/identity/、internal/secretbox/、internal/filepolicy/、internal/nodegate/、deploy/**/nginx*、*auth* | 权限、角色、管理员、鉴权、token、Authorization、admin、role、grant |
| risk:data-delete | internal/store/migrations/、internal/store/migrate/migrations/；各仓清理/保留代码 | DELETE FROM、DROP 、TRUNCATE、删除、清理、retention、purge |
| risk:api-contract | cmd/workpulse/*routes*.go、internal/m8/*handler*、tools/sessionhelper/sessionhelper/ | HTTP路径、ok/error回执字段、信封meta、版本协商字段；具体可执行模式待冻结 |
| risk:sla | internal/scheduler/、internal/schedule/、*timeout* | 超时、重试、限流、timeout、retry、rate、deadline |
| risk:migration | 迁移目录、*.sql | ALTER TABLE、CREATE TABLE、RENAME、迁移、backfill |
| risk:privacy | internal/feishu/、internal/redact/、internal/obs/及各仓用户资料/外发目录 | 手机、邮箱、身份证、地址、open_id、个人、隐私、跨境、导出、export |

4. **提供方契约（R1-003）。** GitHub与工蜂分别实现受控仓库/MR发现、固定SHA diff/标签快照、当前读取与评论写权限、评论查回/幂等及合并事件。具体endpoint/字段/凭据位置待R1-A01/02；不假设两个提供方API相同。坐标/缓存/去重含提供方与实例；迁移后重新核映射，不改绑旧事实。
5. **双端投递与隐私（R1-004/006）。** 同一稳定判定输入聚合全部命中，每端一条并分别留结果；持久化重试只补缺失端，未知响应先按提供方查回/幂等能力核实，不盲目再次发评论。仅提供规则ID/类别/标签/路径/行号/固定commit，不摘抄新增源码行；评论只写原MR，Console继承仓库当前ACL。默认表/标签/路径都作不可信文本安全显示。按业主2026-11-01T04:0xZ已决PM01 A，同commit标签/规则变化更新双端原提醒并追加可追溯修订，保留旧输入/命中与历史知悉。命中集合变化（含减少/归零）重置该修订知悉；集合不变不因该变化重置。同输入重试不追加修订或重置知悉，旧修订迟到知悉不覆盖新修订。更新评论能力、修订幂等/并发及跨端失败查回仍待R1-A01～04实际核实，不用新建评论静默代替已决策略。
6. **知悉不等于审批（R1-005～008）。** 负责人由可信MR/变更映射取得，知悉绑定提醒、版本与身份并记录UTC；提供方合并事实单独留来源及接收时刻。C2命中重新提醒，不继承C知悉，已合并MR也不漏。风险、投递失败和无法判定不产生R1 required check或合并阻塞；R9独立决策不因已知悉被清掉。

## Risks / Trade-offs

- 默认词宽泛有误报：用固定规则版本与七类×三路近似反例交QA，不用在线语义猜测代替规则；误报通过规则评审修订而不改写历史。
- 提供方只读与评论scope不同：无写权限显示评论失败，不能用他人权限或文件服务绕过。
- 未知投递易重复：具体幂等/查回能力未核前不声称恰好一次，不进入双端实施。

## Migration Plan

按已决PM01 A冻结规则/负责人/ACL与评论更新接口→另派实现→纯函数正反/变异与提供方契约验证→隔离GitHub/工蜂MR的双端实测→独立验收→另申请预发。无源码转发、真实评论或发布授权。

## Open Questions

R1-PM01已由业主2026-11-01T04:0xZ批准A并回写，R1-A01～04仍待核；识别三路并集、仅新增行、七类与非阻塞边界已由本单确定。strict只证明规格结构，不证明规则已部署或提醒已送达。
