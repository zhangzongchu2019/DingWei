# Console Session History

## Purpose

本变更把 Agent 结构化会话历史从仅依赖 Hub 进程内帧环改为按授权读取的有界持久记录，目的是让 Console 在 Hub 重启或 helper 重连后仍能连续回放已接收历史。涉及 internal/m8 回放链路与 internal/store 持久化，不影响 Agent 身份授权、投递协议或未上送到 Hub 的本地会话内容。

## ADDED Requirements

### Requirement: R15-001 按消息或回合保留有限历史

Hub SHALL 按 PM-R15-01 定义的逻辑消息／回合单位保存每个 Agent 最近 N 条结构化会话历史；存储单位 SHALL NOT 是 SSE/WebSocket 帧数量。若维持 owner 默认口径，N 为 1000；不同定义及 N 必须由 PM 明确后冻结。

#### Scenario: R15-S001 回放一条已持久化历史

- **WHEN** 已授权 Console 请求某 Agent 的历史游标范围，且该范围内存在已提交记录
- **THEN** 系统按逻辑序号升序返回这些记录及可继续读取的 cursor，记录数按逻辑单位计数

#### Scenario: R15-S002 超出 N 时淘汰最旧记录

- **WHEN** 某 Agent 新增第 N+1 条逻辑记录
- **THEN** 系统保留最近 N 条、淘汰最旧记录，并让从已淘汰位置继续的 cursor 收到可检查的 history gap 标记

### Requirement: R15-002 Hub 重启和 helper 重连后延续持久历史

历史持久提交后，Hub SHALL 在进程重启及同一已授权 Agent helper 断线重连后读取该历史；Hub 进程 epoch SHALL NOT 单独使仍连续的持久 cursor 失效。若历史发生真实淘汰、撤权删除、身份代际变化或存储不可用，系统 MUST 明确报告 gap／不可用状态，不得静默声称完整连续。

#### Scenario: R15-S003 Hub 重启后旧记录可见

- **WHEN** Agent 已有持久记录，Hub 重启后相同 Agent 身份再次可用并请求重启前 cursor
- **THEN** 重启前记录按原序返回；若 cursor 连续且记录未淘汰，客户端无需仅因进程 epoch 变化执行显式 resync

#### Scenario: R15-S004 helper 断线重连后旧记录可见

- **WHEN** helper 断线导致当前 terminal state 关闭，随后同一身份重连并请求最近历史
- **THEN** 已提交历史仍可见且顺序不变，实时新记录接续在历史高水位之后

### Requirement: R15-003 历史回放与实时流无重复、无静默缺失

历史回放 SHALL 使用稳定且可验证的 Agent/session generation 与单调 cursor；服务端 MUST 在持久历史高水位和实时订阅之间建立无竞态切点，并按记录 ID／cursor 去重。无法证明连续时，系统 MUST 返回明确 gap 或 resync 控制，不得静默跳过记录。

#### Scenario: R15-S005 回放切点接上实时流

- **WHEN** 客户端在历史读取和实时订阅的切点附近持续产生并读取事件
- **THEN** 从切点前最后一条历史到切点后实时事件的 cursor 严格递增，测试对照源事件 ID 后重复计数为 0、缺失计数为 0

### Requirement: R15-004 历史受现有 Agent 所有权授权保护

历史读取 MUST 以服务端认证主体解析出的 Agent owner／授权范围为准；客户端自报的 owner、session 或 cursor SHALL NOT 扩大可见范围。持久化 SHALL NOT 自动授予平台管理员或其他 Agent 跨身份读取权限。

#### Scenario: R15-S006 他人不可见

- **WHEN** 主体 B 请求主体 A 的 Agent 历史，即使 B 提交 A 的名称、session key 或 cursor
- **THEN** 服务端按现有授权拒绝或返回不泄露存在性的空拒绝结果，且不返回 A 的正文、条数或可枚举游标

### Requirement: R15-005 容量、过期和单条体积均有硬上限

持久历史 SHALL 同时受 PM 决定的每 Agent 条数、单条字节、每 Agent 总字节、Hub 全局字节和保留时长上限约束；淘汰／过期 SHOULD 通过有界工作执行，任何超限都 MUST 保留足以报告 history gap 的最小元数据且不得输出正文到日志。

#### Scenario: R15-S007 超过单条体积上限

- **WHEN** 新事件超过已定单条字节上限
- **THEN** 系统拒绝持久化完整记录或按已批准的明确策略处理，并发出可观测的大小限制结果；不得静默截断后当作完整历史返回

#### Scenario: R15-S008 TTL 到期后清理

- **WHEN** 记录超过 PM 定义的保留时长且清理器运行
- **THEN** 过期正文被删除，清理批次有界，旧 cursor 得到可检查的 gap／expired 结果

### Requirement: R15-006 撤权或身份删除后清除历史正文

在 PM-R15-05 定值的撤权／删除事件发生后，系统 SHALL 按确定时限使相关历史不可再读取并删除对应正文、索引及可关联缓存；仅保留 PM 批准的最小非正文审计／gap 元数据。重新授予同名身份时 SHALL NOT 暴露旧代际正文。

#### Scenario: R15-S009 撤销后历史不可读取

- **WHEN** Agent credential 被撤销或 owner 删除／解绑并达到规定清理时限
- **THEN** 所有历史读取入口拒绝旧身份，正文和关联索引已清理，使用旧 cursor 的请求不能取回正文

### Requirement: R15-007 迁移和回退不伪造历史或意外销毁数据

schema 迁移 SHALL 为新历史数据创建独立、可回滚应用版本兼容的存储结构；系统 SHALL NOT 将 `session_delivery` 或 `message` 行数推定为完整事件历史或自动伪造回填。应用回退 MUST NOT 自动删除仍受保留策略约束的历史表；销毁历史与备份到期须按批准的数据处置策略执行。

#### Scenario: R15-S010 首次迁移无虚构回填

- **WHEN** 新历史 schema 安装到已有 delivery/message 数据的数据库
- **THEN** 仅从迁移后实际接收且通过校验的结构化事件建立历史，旧投递行不会被当成完整历史展示

#### Scenario: R15-S011 应用回退保留策略数据

- **WHEN** 应用版本回退到不读取新历史表的版本
- **THEN** 回退不自动 drop 表或绕过 TTL／撤权清理策略，且重新升级后仍可按策略读取保留记录
