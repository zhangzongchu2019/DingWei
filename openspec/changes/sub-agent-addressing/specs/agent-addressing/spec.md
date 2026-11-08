# Agent 寻址

## Purpose

本变更把 DingWei 单个主 Agent 的寻址从仅有一个根会话改为可明确指定 helper 管理的子会话，目的是防止错投与静默回落。涉及 Hub、helper、send.py、View、Console 与提及模块，不影响主身份授权和既有主会话投递。

## ADDED Requirements

### Requirement: AA-001 分层全名
全名 SHALL 由点号分隔、从右往左逐级细化：最右段为用户短名，次右段为 Agent 名，其左各段依次为子 Agent、子子 Agent、子子子 Agent。新增子段 MUST 只含字母与数字并遵循现有 Agent 名长度约束；根身份仍由 identity 校验。历史 external 两段根名保持既有兼容，不在一期获得子能力。

#### Scenario: AA-001-S01 写法对照
- **WHEN** 目标写作 `ts3dev55`
- **THEN** 发出时自动补为发送方所属用户下的 `ts3dev55.zzc`，表示主 Agent
- **WHEN** 目标写作 `r2.ts3dev55.zzc`
- **THEN** 表示 zzc 用户下 ts3dev55 的 r2 子 Agent
- **WHEN** 目标写作 `r211.r21.r2.ts3dev55.zzc`
- **THEN** 表示 r2 下 r21 下的 r211（第 3 级子 Agent）

### Requirement: AA-002 层数上限 3
子 Agent 层数 MUST 不超过 3（全名最多 5 段）。超出上限的目标 SHALL 被 Hub 拒收，并明确告知发送方超出层数上限。

#### Scenario: AA-002-S01 第 4 级子 Agent
- **WHEN** 目标写作 `x.r211.r21.r2.ts3dev55.zzc`
- **THEN** 投递失败，原因为超出层数上限，不投给任何上级 Agent

### Requirement: AA-003 子 Agent 不存在即拒收并提醒
Hub 以最右两段定位端点；子路径 MUST 与该端点 helper 上报的子 Agent 树逐级匹配。子 Agent 不存在、已结束，或端点未声明支持子 Agent 时，Hub SHALL 拒收并分别提醒「子 Agent 不存在」「已结束（原因）」「对端不支持子 Agent」，MUST NOT 回落投给父 Agent 或主 Agent。树未恢复 MUST 返回状态暂不可用，不能判不存在。

#### Scenario: AA-003-S01 不存在的子 Agent
- **WHEN** 发往 `r9.ts3dev55.zzc`，而 ts3dev55 上报的子 Agent 中没有 r9
- **THEN** 投递失败，发送方收到「子 Agent 不存在」提醒；ts3dev55 主 Agent 收不到该信

#### Scenario: AA-003-S02 结束与恢复中
- **WHEN** 目标有已结束墓碑，或注册树正在恢复
- **THEN** 分别提示「已结束（原因）」或状态暂不可用；不投父、不将恢复中判为不存在

### Requirement: AA-004 回程与权限
子 Agent 发出的信 SHALL 以其完整全名为发件人，对方回复 SHALL 回到同一子 Agent。子 Agent MUST 继承主 Agent 的权限与编组，不是新身份。

#### Scenario: AA-004-S01 回复回到子 Agent
- **WHEN** `r2.ts3dev55.zzc` 发信给 prodops.zzc，prodops 回复
- **THEN** 回复投到 r2 子 Agent，而非 ts3dev55 主 Agent

### Requirement: AA-005 提及只认主 Agent
人机交互默认的提及规则 SHALL 只支持主 Agent 的两种写法（短名、`Agent.用户`）。正文中三段及以上的提及写法 MUST NOT 被当作子 Agent 投递；系统 SHALL 提示发送方改用结构化目标，且不得因此丢弃整封信。

#### Scenario: AA-005-S01 混合提及
- **WHEN** 正文同时含 `@r2.agent.zzc` 与合法主 Agent 提及
- **THEN** 跳过子提及并提示改用结构化目标，合法主候选和整封原正文仍保留

### Requirement: AA-006 Agent 感知子 Agent
helper SHALL 向 Hub 上报其子 Agent 树（名字、父、状态），并告知每个会话自己的完整全名与直接子 Agent 清单。子 Agent 默认名为模型会话 UUID 无连字符规范表示的末 8 位随机段（同父冲突时逐步向左加长，不取时间戳前缀），可另配别名，两者都可寻址。

#### Scenario: AA-006-S01 名称与上下文
- **WHEN** 同父下模型会话 UUID 随机末8位碰撞或新增别名
- **THEN** 新默认名加长且保持字母数字，已有名不变；别名与默认名指同一节点，每个会话获知自身全名和直接孩子

#### Scenario: AA-006-S02 同一分钟创建
- **WHEN** 同一父 Agent 在同一分钟创建两个 UUIDv7 thread，且两者前8位相同
- **THEN** 默认名分别取无连字符UUID的随机末8位；若末8位也碰撞，新节点逐步加长尾段，最终两个默认名不同，已有名保持不变

### Requirement: AA-007 参数化生命周期与逐条退信
helper MUST 持久化并应用已批准默认：显式任务完成即退开、累计空闲 1h、最长存活 2d、平台上限 5d（按节点各自创建时刻计算）、退出宽限 10min；父结束时级联关闭后代。状态 SHALL 为 allocating、active、draining、ended，运输离线独立。显式完成或期限触发先封口；普通 turn/completed 不算任务完成。确定未执行的排队信逐条幂等退回原发送节点，可能已执行者保留不确定证据，不伪造 ACK。墓碑、别名、截止与结束原因跨重启保留。

#### Scenario: AA-007-S01 退出封口与重启
- **WHEN** 父关闭且孩子有两封未执行排队信和一封执行状态不明信，随后 helper 重启
- **THEN** 两封逐条退回且不重复；不明信不假报未执行；新信提示已结束（原因），不重置截止时间

### Requirement: AA-008 协商与可信分道协议
Hub/helper SHALL 协商 sub_agents_v1、支持深度和树 revision。结构化路径、node_id/generation、规范全名和 from 由可信连接与原生 threadId 绑定校验，冲突/代次不符拒收；未就绪、未支持、别名冲突与策略缺失须明确区分。共享 app-server MUST 单 reader/writer broker 分派到独立分道队列；未知事件不猜父。正文不改；未协商不得将子地址降级成父地址。

#### Scenario: AA-008-S01 混部与并行
- **WHEN** 对端不支持子路径，或共享 app-server 的A分道被中断而B仍运行
- **THEN** 前者拒收且父不收信；后者只关闭A；事件与发信来源均不串到B或主会话

### Requirement: AA-009 持久作用域与安全回退
Hub SHALL 同事务保存投递与来源/目标路径；helper 账本及证据 MUST 按分道、代次、thread 隔离，旧记录为主分道。r55 对账保护（每席1请求、同thread间隔15s、每轮200条、时间容差1s、显示不ACK）不变。回旧二进制前 MUST 封口、逐条退信并隔离歧义子投递；不能让旧 claimer 按父地址执行，不删除墓碑或制造完成。

#### Scenario: AA-009-S01 旧代次与回退
- **WHEN** 旧代次通知到达，或回退时仍有歧义子信
- **THEN** 不推进新代次状态；未完成持久隔离时阻止降级，主会话历史保持

### Requirement: AA-010 一期拒收与作用域展示
第一期 SHALL 只允许 Codex helper 管理一级并列 thread；二三级明确不支持，超过三级明确超层数。在线树、View/Console 授权票据、缓存、游标和成本 MUST 按节点/代次隔离。旧文本在线表只列主席，主身份管理和任务单执行者权限不变。文件命名空间保持根作用域，不以放宽路径校验替代可信来源。

#### Scenario: AA-010-S01 树形显示与旧客户端
- **WHEN** 同一主席有两个孩子且旧在线客户端仍连接
- **THEN** 新目录显示树及状态，旧文本只列主席；两个孩子的订阅与费用不相互覆盖
