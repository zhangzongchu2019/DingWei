# DingWei v0.2 项目计划（执行版）

> 状态：执行版
>
> 部署目标：`ts3poc.wegoab.com`（`193.112.157.47`）
>
> 部署方式：Docker 容器化部署与运维
>
> 首轮评审稿历史锚：`cd1d08fa0137ca12be668792aee23556605beb8f`；当前执行版冻结锚以工蜂 `design/v0.2` 受评派件的 commit/tree 与本文件 SHA256 三元组为准，不在文件内写自引用 commit
>
> 设计输入：`specs/v0.2/baseline-manifest.tsv` 所列权威件，加上由 `specs/v0.2/check.py` 自守的域主轴合同 `specs/v0.2/spec.md`；manifest 每行 source 必须是完整 40 位、为当前锚 ancestor 且可复算对应文件指纹，短锚、自由文本、不可达锚或指纹不符均 FAIL

## 1. 目的与边界

本计划把 DingWei v0.2 从第一版可运行系统推进到完整生产形态所需的版本、资源、并行开发拆分、Mock 边界、集成顺序和验收门统一成一份执行入口。

本计划不修改已经收口的 SpecDoc、目标架构、SP3、原型或其评审结论。实施中若发现设计缺口，应登记变更请求并按原治理流程处理，不得由开发任务静默改写设计。

### 1.1 名词与版本轴

- `design/v0.2`：工蜂设计基线分支；其输入、义务和闸以 `specs/v0.2/` 为准。
- `VERSION`：hub/sessionHelper 运行时版本唯一真相；当前值及兼容规则从仓根文件实取，不与本计划阶段号换算。
- `P0.2`～`P1.0`：本计划的产品交付阶段号，仅用于路线和资源规划。
- `DW02-*`：第一版实施 WBS 编号，避免与 `V02-OBL-*` 义务编号混淆。
- `DW02-M0`～`DW02-M5`：本计划第一版里程碑，不等同于目标架构 §14 的 `M0`～`M6` 迁移阶段，也不与 `V02-OBL-*` 义务共用前缀；证据、状态和跨席沟通必须写全限定名，禁止用裸 `M0/M1/M2`。

本计划不是 greenfield 替换令。`P0.2` 在 ts3poc 与既有 v0.1 链路并存，使用独立路径、容器、数据库和 deployment anchor；未完成双轨比对、回滚演练和独立 cutover 授权前，不停用旧链路、不迁移旧写权威、不触碰目标架构 §14.3 的不可逆点。

### 1.2 第一版目标

第一版必须闭合以下最小链路：

```text
飞书用户
  → 飞书事件接入
  → 身份绑定与认证
  → DingWei canonical envelope
  → 从受信结构化输入解析 recipient set
  → 路由到指定 Agent
  → Agent 收取并处理
  → 回复进入可靠投递
  → 飞书收到回复
  → Web UI 可查看 Agent、消息和运行状态
```

同时满足：

1. `REQ-P02-01`：系统可在目标服务器以 Docker Compose 最小化运行。
2. `REQ-P02-02`：四个开发小组可以基于冻结合同独立开发和 Mock 测试。
3. `REQ-P02-03`：四组完成后按 3、10、30 Agent 分级集成联测。
4. `REQ-P02-04`：UI 以原型 v1.51 为基础，补齐真实系统需要的加载、错误、确认、权限和投递状态交互。
5. `REQ-P02-05`：第一版保证 30 个 Agent 正常在线并可可靠收发消息；按 `DW02-M0` 冻结的真实/模拟比例验收，默认真实 3 + 模拟 27，验收报告必须写明比例与可主张的结论范围。
6. `REQ-P02-06`：第一版外部渠道只接飞书；其他渠道保留适配边界，不进入交付范围。
7. `REQ-P02-07`：外部身份字段在 EDGE 被剥离；internal API 不暴露到公网且缺 AccessCtx 时 fail-closed。
8. `REQ-P02-08`：入站正文默认不进入普通日志和审计；最小 secret detector 失败时拒绝对应落盘 effect。

### 1.3 第一版明确不做

- 企微、Slack、Google Chat 等其他渠道。
- 200+ Agent 容量目标。
- 多企业自动供给和完整多租户运营。
- 完整知识治理、skill 晋升和复杂审批。
- Kubernetes 化和跨机房高可用。
- 生产计费、商业化运营和完整多模态附件处理。
- SIDE-01 紧急固定路由、离线 allowlist 和恢复补账；整体推迟到 `P0.6`，`P0.2` 不交付半套紧急面。
- `safe_mode`；`P0.2` 实现 `live/ready/degraded`，完整安全模式随治理版交付。

未实现能力可以在 UI 中保留入口，但必须明确显示“未启用”，不得返回伪造成功结果。

## 2. 从第一版到完整形态的版本路线

规划共六个产品阶段：第一版 `P0.2`，之后经过四个能力扩展阶段，以 `P1.0` 完整生产版收口。阶段号不替代仓根 `VERSION`、Git commit、镜像 digest、部署锚或设计基线编号。

| 产品阶段 | 定位 | 核心交付 | 对应架构迁移/未实测项 | 退出条件 |
|---|---|---|---|---|
| `P0.2` | 最小可运行 | 飞书、30 Agent、统一信封、可靠投递、基础任务、核心 UI、Docker 部署 | 新建面覆盖 §14 M0～M2 的必要地基；启动 `U-01` 测量并向原 owner 提交关闭证据，启动 `U-03` 验证 | 按 `DW02-M0` 冻结比例完成 30 Agent 分级联测和 24 小时稳定性验证，报告明确真实/模拟比例与结论边界 |
| `P0.3` | 协作闭环 | 项目/任务 DAG、执行与门分离、帖子、通知、摘要、证据和返工 | 对应 §14 M3；关闭 `U-03`，验证 `U-05` 单活 consumer | 一个真实小项目在 DingWei 内完成全生命周期 |
| `P0.4` | 知识与能力 | 分层知识、上下文快照、检索审计、skill/MCP 目录及安装撤销 | 对应 §14 M4；验证 `U-06` 外部 digest anchor | 新 Agent 可按授权取得任务上下文与能力，结束后不越权继承 |
| `P0.5` | 治理与多租户 | 企业隔离、principal/角色、待决审批、规则版本、RLS 和安全审计 | 补齐 §14 M1/M2 多租户实证；关闭 `U-02` | 至少两个企业通过数据、权限、消息和 Agent 隔离测试 |
| `P0.6` | 规模化运行 | 200+ Agent、资源池、自动供给、SIDE-01、多渠道、容灾和完整可观测 | 对应 §14 M5；关闭 `U-07`，逐项演练 §9.4 六类降级 | 200+ Agent 稳定运行；每类故障的允许/禁止效果均有证据 |
| `P1.0` | 完整生产形态 | §14 M6 cutover、全域闭环、高可用、灾备、安全、升级回滚、容量与运营体系 | 关闭剩余 `U-*` 与 `U-04` 仓侧保护验证 | 功能、安全、容量、故障恢复和运维五类验收全部通过 |

### 2.1 P0.2：最小可运行

- 单企业、单飞书应用。
- 飞书私聊和群聊显式 `@机器人`。
- P0.2 收件人只来自两类受信结构化输入：UI 显式选择产生的 `agent_id` command，或由 IDN/AGT 权威行维护的飞书会话/群绑定；正文、别名和自然语言不得改变 recipient set。
- Agent 注册、心跳、上线、下线、寻址和连接代际。
- 消息入站、路由、投递、ACK、重试、死信与去重。
- 基础项目、任务、指派和结果回传。
- Agent 列表、工作面、对话、任务、渠道和健康 UI。
- PostgreSQL、outbox、审计、备份和恢复。
- Docker Compose 一键启动及 30 Agent 容量验收。
- EDGE 头清洗、internal API PEP、最小 redaction、generation/CAS、DATA-01 repository seam。
- `live/ready/degraded` 健康语义及企业 DB、MSG 两类降级演练。

### 2.2 P0.3：协作闭环

- 项目、任务和 node DAG。
- planner、executor、reviewer、ops 等角色。
- execution 与 gate verdict 分离。
- FAIL evidence、revision node 和依赖重连。
- 不可编辑帖子流、更正帖、附件引用和封存 digest。
- 通知 taxonomy、待决队列、周报和阻塞管理。

### 2.3 P0.4：知识与能力

- 平台、企业、项目和任务级知识。
- visibility、生命周期、锚和 stale 检测。
- 派发时上下文快照、先授权过滤后检索和检索审计。
- skill/MCP catalog、版本、归属和分发策略。
- 安装、卸载、撤销和 generation fencing。

### 2.4 P0.5：治理与多租户

- 多企业注册与平台库/企业库边界。
- principal、渠道身份和 Agent 溯源绑定。
- 项目角色、effect 权限、规则版本和风险分级。
- 单签、双签、预授权、紧急 allowlist 和吊销。
- RLS、跨企业负测、拒绝面防泄漏和完整审计。

### 2.5 P0.6：规模化运行

- 200+ Agent runtime 资源池。
- 自动供给、启停、重启、租约和回收。
- session generation、owner epoch、横向扩展和故障转移。
- sidecar 紧急恢复面和恢复后审计补账。
- 企微、Slack、Google Chat 等渠道适配。
- 多模态、限流、熔断、背压、灰度和容量预测。

### 2.6 P1.0：完整生产形态

- 八个目标业务域与平台机制完整闭环。
- 多副本、高可用、备份、时间点恢复和灾备演练。
- migration 前滚/回滚、镜像签名、SBOM 和漏洞治理。
- 密钥轮换、最小权限、安全审计和正式 SLI/SLO。
- 安装、升级、扩容、备份、恢复和应急运行手册。

## 3. 各版本需要业主提供的资源

凭据、token、cookie、数据库口令、模型 key 和私钥不得写入本计划、Git、DingWei 消息或测试日志；只通过服务器受控 secret、环境注入或既有凭据通道提供。

### 3.1 资源总表

| 版本 | 必须提供 | 可后补 |
|---|---|---|
| `P0.2` | 四组负责人、独立测试负责人、目标服务器授权、飞书应用、模型并发与费用、Agent 配置、试用/验收人员、24h 值守 | 正式监控大屏、额外域名优化 |
| `P0.3` | 真实低风险项目、负责人/执行者/评审者、流程裁定、独立测试、对象存储或只存 ref 的明确降级裁定 | 更多项目模板 |
| `P0.4` | 首批知识、skill/MCP 清单、知识与能力 owner、分发策略 | 大规模历史知识迁移 |
| `P0.5` | 第二试点企业、安全负责人、权限审批规则、隔离数据库资源 | 外部合规认证 |
| `P0.6` | 扩容节点、200+ Agent 模型容量、多渠道应用、运维值守 | 异地灾备机房 |
| `P1.0` | 生产集群、正式域名与证书、安全测试、灾备、正式上线签批 | 商业化运营资源 |

### 3.2 P0.2 资源

#### 人员与能力要求

具体人选以 §4.0 为唯一编组事实源。能力要求为：1-a 具备控制面/数据与安全边界能力；1-b 具备消息、Agent、飞书和可靠投递能力；1-c 具备前端、交互和 UI 自动化能力；1-d 具备合同、Mock、CI、集成测试与发布工程能力。每组固定两席，分别以“设计开发”和“测试验收”为主角色并互为能力备份。

另设 1 名独立版本测试负责人、2 名独立设计评审、1 名主运维、1 名业务验收人和 3～5 名飞书试用人员。总集成负责人由 1-d 设计开发席兼任，不另计第七名开发者。
- 24 小时稳定性窗口采用自动告警 + 至少 1 名 on-call；独立测试不承担单人连续值守。

任一开发组缺一席时不得把组内测试验收标为完成；可继续低风险实现，但出组门顺延。独立测试或独立设计评审缺席时不得代签，相关版本/设计变更顺延或记 INCONCLUSIVE。

#### 目标服务器

业主需授权对 `193.112.157.47`：

1. 先做 CPU、内存、磁盘、端口、Docker、nginx、TLS 和现有负载的只读勘察。
2. 勘察通过并取得部署授权后，创建 DingWei 专用目录、网络、volume 和容器。
3. 配置 `ts3poc.wegoab.com` 对应 UI/API/飞书回调反向代理。
4. 创建 PostgreSQL、备份任务、日志轮转和健康检查。
5. 执行受控启停、升级、回滚和故障演练。

P0.2 业务范围仍是单企业，但隔离负测额外创建 `dingwei_ent_test` 测试 database；它不承载业务、不提供自动供给，也不构成提前交付多企业运营。

勘察前不把单一总数作为采购承诺。`DW02-M0` 只输出并由业主签收容量方法件：公式、单位、分母、安全系数、取数锚、真实/模拟三档测量方案、降级规则、追加节点触发线和签收模板；此时不宣称 P0.2 实测容量。以下两张实测表由 `DW02-M1` 骨架压测首次回填，并在 `DW02-M3` 的 3/10/30 曲线后定稿，作为扩至 10/30 Agent 的前置门：

| 平台服务 | CPU/内存取值依据 | 测量环境 | 磁盘分项 | 是否可降级 |
|---|---|---|---|---|
| edge/control/message/worker/feishu-adapter/ui | 3 Agent 骨架压测的 p95 与峰值，乘安全系数 | 目标机 / 同规格机 / 非目标机 | 镜像、日志 | UI/监控可降级，信任边界和消息面不可省 |
| PostgreSQL | 连接数、事务率、outbox lag | 目标机 / 同规格机 / 非目标机 | PG 数据、WAL、备份 | 不可切到其他库 |
| prometheus/grafana | 指标基数和保留期 | 目标机 / 同规格机 / 非目标机 | 指标数据 | Grafana 可后置，核心指标采集不可省 |

| Agent 档位 | 单 Agent 单位资源 | 测量环境 | 3/10/30 计算方式 | 验收用途 |
|---|---|---|---|---|
| 模拟 Agent | `DW02-M1` 实测 CPU、RSS、连接和消息率 | 目标机 / 同规格机 / 非目标机 | 单位 p95 × 数量 × 1.5 | 协议、寻址和可靠性 |
| 轻量真实 Agent | 真实 CLI 空闲/工作 p95 | 目标机 / 同规格机 / 非目标机 | 单位 p95 × 数量 × 1.5 | 小规模真实链路 |
| 完整真实 Agent | 模型 CLI、side process 和工作目录 p95 | 目标机 / 同规格机 / 非目标机 | 单位 p95 × 数量 × 1.5 | 业主要求的全真实验收 |

磁盘必须分别预算 PG/WAL、日志、审计 ref、Agent 工作目录、镜像和备份。非目标机数据一律标为 provisional 推算，只用于方法件自检，不得作为 10/30 Agent 扩容门；目标机或经业主确认的同规格机数据才可签收为容量实测。若 3/10 实测推算表明单机不足，全真实 30 Agent 的前置资源是追加节点；在资源未提供时，允许的降级验收只能是“真实 3 + 模拟 27”，不得宣称 30 个真实模型 Agent PASS。`U-01` 只在 `DW02-M3` 形成 3/10/30 实测曲线后产出本计划侧关闭证据，并提交架构登记的原 owner 判定；本计划不越权自闭。

#### 飞书

- 一个 P0.2 企业自建应用和应用管理员。
- 机器人、事件订阅和回调地址配置权。
- 私聊、群内显式 `@机器人`、发送消息和卡片所需权限。
- 一个开发联调群。
- 普通用户、项目负责人、平台管理员三类测试身份。

#### Agent 与模型

- 第一版支持的 Agent/CLI 类型。
- 每类 Agent 的模型提供方、账号或 API 配额和费用归属。
- 账号能否共享及供应商并发限制。
- 30 个 Agent 的稳定全名、角色、所属组和环境 profile。
- 明确 30 Agent 验收是全真实，还是允许真实 Agent 与模拟 Agent 分阶段完成。

真实/模拟比例、模型并发额度、费用与追加节点门统一以本节容量方法件和 §10 为准，不采用更弱的口头或“建议值”口径。

#### 产品裁定与验收

- 是否确认 P0.2 为单企业、飞书单渠道。
- 普通用户是否可直接选择 Agent，以及 UI 显式选择/飞书会话绑定两类结构化寻址机制的权限边界。
- Agent 新建、关闭、重启权限。
- 项目创建、任务指派和人工审批权限。
- 第一版必须上线的原型页面。
- “30 Agent 正常运行”的准确 SLO。
- 是否承载真实业务，或只作为内部试点。
- 至少 24 小时稳定性观察和一次恢复演练窗口。

### 3.3 P0.3 资源

- 1～2 个真实、低风险试点项目。
- 每个项目的 owner、executor、reviewer 和 ops。
- 真实任务拆解、评审、返工和交付样例。
- 必须独立评审的任务类型及职责互斥规则。
- 取消、暂停、重派、FAIL 返工和通知规则。
- 至少两个完整项目周期的试运行窗口。
- 附件只保存 scoped ref；若需保存 blob，必须在本阶段提供对象存储、分类和下载再鉴权能力。

### 3.4 P0.4 资源

- 平台纪律、企业制度、项目介绍、开发测试发布手册和故障手册。
- 每份知识的 owner、可见范围、有效期、权威源和更新方式。
- 首批 skill/MCP 清单、owner、版本、环境和使用范围。
- 安装、升级、撤销、强制推送和人工审批策略。
- 至少一名知识管理员和一名能力管理员。

### 3.5 P0.5 资源

- 第二个企业或内部隔离测试组织。
- 两企业各自的管理员、普通用户和明确不可互访的数据。
- 平台库、至少两个企业数据库及最小权限账号。
- 安全负责人和独立权限测试人员。
- 低、中、高危动作以及单签、双签、预授权规则。
- 数据留存、解绑、离职、吊销和紧急操作制度。
- 跨企业、越权、重放和伪造身份专项测试授权。

### 3.6 P0.6 资源

- 至少 2～3 台 Agent runtime 节点。
- 具备故障迁移能力的控制/消息节点和独立 PostgreSQL。
- 对象存储、集中日志、指标、告警和备份空间。
- 200+ Agent 的模型账号、并发/token 配额和月度预算。
- 主备模型策略及容量降级规则。
- 每个新增渠道的企业应用、管理员、权限、测试群和测试账号。
- 运维 owner、值守时段、升级联系人、变更窗口和故障演练安排。

### 3.7 P1.0 资源

- 正式生产集群、域名、证书、高可用 PostgreSQL 和对象存储。
- 正式 secret 管理、集中日志、告警、备份和异地灾备。
- 渗透测试、依赖/镜像扫描、数据分类和密钥轮换制度。
- 产品、技术、安全和运维四类验收负责人。
- 首批正式用户、上线窗口、回滚窗口和上线后观察期。

## 4. P0.2 四组并行开发拆分

### 4.0 正式编组与授权边界

| 组/角色 | 设计开发/负责人 | 测试验收/组员 | 主责 |
|---|---|---|---|
| 1-a 控制与数据组 | `zzc-chatgpt56solzhangzongchu-2642` | `zzc-sonnet50tczc2000-2642` | EDGE/control、IDN/WRK/COL-lite、Repository、数据库 |
| 1-b 消息与 Agent 组 | `zzc-opus50tczc2000-2642` | `zzc-kimi30kqtuxerh-2642` | MSG/AGT/FCS/JOB、飞书、Agent runtime |
| 1-c Web UI 组 | `zzc-qwen38maxkhovvob4-2642` | `zzc-opus50tchangshark-2642` | UI、交互、展示状态、前端测试 |
| 1-d 平台集成与测试工程组 | `zzc-chatgpt56soltchangshark-2642` | `zzc-sonnet50tchangshark-2642` | 合同、统一 Mock、测试资产、CI、Compose、可观测、集成与 release set |
| 运维组 | `zzc-chatgpt56terrazhangzongchu-2642` | — | 经授权的环境勘察、部署、回滚和运行证据 |
| 独立测试 | `zzc-chatgpt56terratchangshark-2642` | — | 测试计划审核、独立执行、证据抽验与版本测试 verdict |
| 独立设计评审 | `zzc-deepseekv4pro08130ca16c13-2642` | `zzc-shencedeepseek-2642` | 后续设计变更全文评审、架构/计划一致性与独立 verdict；不得承担 1-a～1-d 交付 |
| 业务验收人 | 业主在 `DW02-M2a` 前填入 | — | 业务可用性验收，不替代技术测试或设计评审 |
| 飞书试用人员 | 业主在 `DW02-M2a` 前填入 3～5 席 | — | 真实交互试用与问题反馈，不拥有测试 verdict |

组名映射固定为 `1-a ≡ 第一组`、`1-b ≡ 第二组`、`1-c ≡ 第三组`、`1-d ≡ 第四组`，两种称谓均为合法别名，并由 `DW02-001` 对撞映射完整性。1-a 只写控制/数据与其镜像，不改消息、UI 或目标服务器；1-b 只写消息/Agent/飞书/worker，不取得裸 DB/Tx、不改 nginx；1-c 只写 UI 与前端测试，不拥有业务权威数据或自行解释权限；1-d 可写合同、Mock、测试计划/用例模板、CI/Compose/部署与集成脚本，但不得单方面改业务语义、代替业务组签收或直接操作目标服务器；运维组不改业务源码，只能在 §10 授权后执行对应环境动作。任何组均无权自行 cutover、停旧链路、使用生产凭据或执行不可逆 migration。

1-d 设计开发席 `zzc-chatgpt56soltchangshark-2642` 兼任总集成负责人，不新增独立第七席。其拥有合同发布、跨组门禁与 release set 归口权，但不得单独签收自己或 1-d 产出的合同、Mock、测试资产、镜像、CI、Compose、nginx/部署脚本、备份或回滚件；业务语义由受影响业务组签收，测试可执行性由独立测试签收，Compose/CI/nginx 由运维核验，备份/回滚由运维执行并由独立测试或业主抽验，设计变化由独立设计评审签收，资源/发布门由业主和运维签收。

独立测试负责人为 `zzc-chatgpt56terratchangshark-2642`，不得参与被测功能的主要开发，也不归 1-d 开发负责人代签。组内“测试验收”属于开发自测/出组门，只能签本组交付，不构成独立 verdict；版本 PASS/FAIL/INCONCLUSIVE 仅由独立测试签字。1-d 负责测试工程与计划编排，独立测试对 Mock 保真度、测试模板、追踪矩阵、CI 门槛和故障注入具有审计、增补要求与否决权；运维负责人不得代签测试 verdict，独立测试负责人不得执行目标环境变更。24h 窗口由运维 on-call 连续记录，独立测试按交接点抽验并作最终判定；独立测试缺席时窗口顺延或结论为 INCONCLUSIVE，不允许临时代签。

两名独立设计评审不得承担 1-a～1-d 的设计、实现、组内验收或发布职责；任何后续计划/合同/架构实质变更至少由其中一席全文评审，触及其回避面时改由另一席，二者均回避时由业主另派门方。

本项目的职责分离与独立性以稳定 DingWei 会话为执行 principal，而不是以 Git 邮箱、账号所有者或会话名后缀推断自然人。每个会话独立保存上下文、证据和 reviewer/tester-owned verdict，禁止代另一会话签字或复用其 verdict；业主的业务授权与最终裁定不能替代独立设计评审或独立测试证据。若业主另要求自然人层面也互异，须新增真人资源并重做本表，当前 Agent 编组不宣称满足该更强条件。

### 4.1 权威对象与唯一 owner

组名是交付责任，不改变目标架构的域归属。任何权威对象只有一个 owner；其他组只能持稳定 ID、版本快照或 ref。

| 权威对象 | 目标域/机制 | 唯一 owner 组 | 其他组允许持有物 |
|---|---|---|---|
| enterprise、principal、channel binding、AccessCtx | IDN | 第一组 | `AccessCtxRef` 与服务端签发的不可变上下文 |
| project、task、assignment、execution result | WRK | 第一组 | task/node ID、版本快照、result ref |
| post、audit timeline；notification 在 P0.3 实现 | COL/PLT Audit | 第一组 | thread/post/audit ref；第三组只展示，不写权威行 |
| Agent instance、runtime order、health | AGT | 第二组 | 第一组只持 `agent_id`、assignment ref 和查询快照 |
| session、envelope、delivery、ACK、channel receipt | MSG | 第二组 | 其他组只持 envelope/delivery ref |
| outbox relay、retry、reconcile worker state | JOB/PLT | 第二组 | 第一组只在业务事务写 outbox，不直接投递 |
| Repository、DB registry、migration、transaction port | DATA/PLT | 第一组 | 其他组只经受控 application/API 使用，不取得裸 DB/Tx |
| UI view state | ADP | 第三组 | 仅缓存可丢失展示状态，不成为业务事实源 |

KNO、CAP 在 `P0.2` 不实现，分别由 `P0.4` 承载。PLT 是共用机制层，不是第九业务域；本版的数据机制由第一组 owner，消息/worker 机制由第二组 owner，部署发布由总集成负责人归口。

### 4.2 第一组：控制平面与数据基础

交付责任：`EDGE-01`，以及 `CTRL-01` 内的 `IDN/GOV-lite/WRK-lite/COL-lite` application/domain 和 `DATA-01` 受控仓储 seam。

`GOV-lite` 在 P0.2 只覆盖 Agent 新建/关闭/重启、普通用户选择 Agent、任务指派与受保护 Agent command 五类 effect 的 allow/deny policy、规则版本和 fail-closed 判定；不做多级审批、双签、预授权、紧急 allowlist、规则运营或完整治理 UI，这些进入 P0.5。

运行单元：第一组交付代码运行于 `edge` 与 `control`；`control` 承载包括 AGT 在内的 application/domain 包，AGT 包由第二组交付但不得部署进 `message`。`message` 只承载 session/envelope/routing/delivery/ACK。

交付任务：

- P0.2 服务骨架、统一配置和依赖注入。
- `AccessCtx`、统一接口信封和错误分类。
- 飞书身份到 principal 的绑定与解析。
- 企业、principal、项目、任务和 COL-lite 权威模型；Agent 只做查询 façade，不建第二份 Agent 权威表。
- 最小任务状态机：`created → assigned → running → succeeded/failed/cancelled`，每个 mutating command 带 expected generation/state。
- PostgreSQL repository、migration、事务、outbox 和幂等表。
- EDGE 头清洗、API 鉴权、最小 redaction、审计、request/correlation ID。
- 按冻结合同实现控制平面 handler 与 Mock server；不另造 OpenAPI、事件 schema 或 fixture 真相源。

MVP API：

- `POST /api/v1/auth/feishu/session`
- `GET /api/v1/me`
- `GET/POST /api/v1/agents`
- `GET /api/v1/agents/{id}`
- `GET/POST /api/v1/projects`
- `GET/POST /api/v1/tasks`
- `POST /api/v1/tasks/{id}/assign`
- `POST /api/v1/tasks/{id}/result`
- `GET /api/v1/audit-events`
- `GET /healthz`、`GET /readyz`

组内 Mock：飞书身份、Agent 在线目录、消息投递成功/超时/重复 ACK、固定时钟和固定 ID。

完成门：

1. `AC-1A-01 [integration]`：空库可一键 migration。
2. `AC-1A-02 [component]`：所有写接口以 `request_id` 幂等。
3. `AC-1A-03 [security-negative]`：入站自述的 `enterprise/principal/org/tenant/actor` 字段在 EDGE 被剥离，下游不可见；伪造 principal 负测通过。
4. `AC-1A-04 [recovery/rollback]`：outbox 在进程重启后不丢事件，且业务事务只写 outbox、不直接投递。
5. `AC-1A-05 [security-negative,integration]`：所有数据访问经 Repository `WithAccess` 与 DB registry；裸 DB/Tx 不逃逸仓储层，并复用现有 handle-escape detector。P0.2 同一 PostgreSQL 实例内至少分 `dingwei_platform`、业务库 `dingwei_ent_<code>` 与仅供隔离负测的 `dingwei_ent_test`，registry 落平台库，应用只以 opaque database key 选库、禁止硬编码企业 DSN；企业 A `AccessCtx` 访问测试企业 resource 时目标企业库 query count 为 0，并返回 `NOT_FOUND` 或 `TENANT_NOT_BOUND`。
6. `AC-1A-06 [component,security-negative]`：1-a 权威对象（project、task、assignment、execution result、COL-lite）的状态变更以 expected generation/state 做 CAS，并发只一胜，其余返回 `FENCED/CONFLICT`；Agent 代际由 1-b 持有，façade 只透传 `expected_generation` 并原样映射 `FENCED`，不本地判定或缓存代际。
7. `AC-1A-07 [security-negative]`：audit/log 不出现原始正文和 secret；redaction 判定器失败时拒绝落盘 effect。
8. `AC-1A-08 [contract,integration]`：API 合同和 PostgreSQL 集成测试通过。

### 4.3 第二组：消息、Agent 运行与飞书渠道

交付责任：`AGT application/domain + MSG-01 + FCS-01 + JOB-01 + sessionHelper/Agent runtime`；`P0.2` 不交付 SIDE-01 紧急面。

运行单元：AGT application/domain 包由第二组交付、集成进 `control`；`message` 只运行 MSG session/envelope/routing/delivery/ACK，`worker` 运行 JOB，`feishu-adapter` 运行 FCS，Agent runtime 独立运行。

交付任务：

- canonical envelope 编解码。
- Agent 稳定全名、连接代际、在线目录和心跳。
- 从受信结构化 command 或 IDN/AGT 会话绑定取得 recipient set，再执行寻址、队列、投递、ACK、重试和死信；不得读取正文决定权限或跨边界投递。
- 重复消息、重连和迟到 ACK fencing。
- 飞书 webhook 验签、challenge 和事件去重。
- 飞书消息与 canonical envelope 双向转换。
- 私聊和群聊显式 `@机器人`。
- 飞书限流、退避和错误分类。
- Agent runtime Docker 镜像和 30 Agent 负载工具。
- outbox relay、row lease、fencing token、retry、dead-letter 和 reconcile worker。

MVP API：

- `POST /webhooks/feishu/events`
- `GET /ws/agents`
- `POST /internal/v1/envelopes`
- `POST /internal/v1/deliveries/{id}/ack`
- `GET /internal/v1/sessions`
- `GET /internal/v1/agents/{id}/health`
- `POST /internal/v1/agents/{id}/commands`
- `GET /healthz`、`GET /readyz`

组内 Mock：第一组 AccessCtx/Agent/Task API、飞书 OpenAPI 的 200/429/5xx/超时、Agent 在线/忙/重连/重复 ACK/迟到 ACK，以及 outbox port fake（可注入 lease 过期、并发抢占和迟到 ACK）。

完成门：

1. `AC-1B-01 [component,Mock]`：同一飞书事件重复到达不产生重复业务 effect。
2. `AC-1B-02 [security-negative]`：旧连接不能 ACK 新 generation 的投递。
3. `AC-1B-03 [recovery/rollback]`：服务重启后未完成投递可以恢复。
4. `AC-1B-04 [integration]`：单 Agent 故障不阻塞其他 Agent。
5. `AC-1B-05 [capacity]`：30 Agent 模拟连接稳定运行至少 2 小时。
6. `AC-1B-06 [security-negative]`：`/internal/v1/*` 只在容器内网络开放，EDGE/nginx 不代理；缺 `access_ctx_ref` 返回 `UNAUTHENTICATED`。
7. `AC-1B-07 [security-negative]`：`agents/{id}/commands` 必须命中 GOV-lite effect policy；缺政策或缺业主权限裁定即 fail-closed。
8. `AC-1B-08 [recovery/rollback]`：杀死并替换 worker 后，旧 lease owner 不得提交 ACK。
9. `AC-1B-09 [security-negative]`：audit/log 不出现原始正文和 secret；Agent ID 只作溯源，直接用 Agent ID 请求受保护 effect 返回 `FORBIDDEN`。
10. `AC-1B-10 [security-negative,Mock]`：正文中出现任意 Agent 全名或别名时 recipient set 不变；只有结构化 binding/command 可改变收件人。无 binding 且无显式 recipient 时返回明确错误，不默认投递、不广播。
11. `AC-1B-11 [contract,security-negative]`：同一连接产出的 envelope 携带同一不可变 `SessionAuthRef`；连接换代后旧 ref 不复用、不续期；receiver 对 ref 与 assignment/binding 不一致的 envelope 拒绝并记审计。

### 4.4 第三组：Web UI 与展示适配

责任范围：Web/UI 接入适配与展示状态；不拥有 COL、Agent、任务、审计或投递权威数据。

交付任务：

- 把原型 v1.51 转换为真实前端路由和组件。
- 登录态、权限守卫、Agent 列表和 Agent 工作面。
- Agent 对话、任务列表、任务指派和结果展示。
- 飞书渠道状态、投递记录、失败原因和受控重试。
- 最小审计/帖子只读时间线。
- `live/ready/degraded` 页面和明确禁用 capability 展示。
- 集成测试 UI 入口；统一 fixture 与 mock-control 由总集成负责人提供，本组只消费。

第一版优先页面：

1. 登录与平台概览。
2. Agent 列表。
3. Agent 工作面。
4. 与 Agent 交互。
5. 最小任务/项目看板。
6. 渠道状态。
7. 投递与审计记录。
8. 平台运行健康。

补充交互：加载、空数据、无权限、服务不可用、表单校验、防重复提交、危险操作确认、消息状态、Agent 状态、搜索筛选分页、correlation ID 复制和“未启用”提示。

组内 Mock：由冻结 OpenAPI 生成控制面 Mock，以 SSE/WS fixture 模拟 Agent 状态，提供 30 Agent 名册及成功、超时、无权限、冲突和部分失败场景。

完成门：

1. `AC-1C-01 [Mock,E2E]`：不依赖真实飞书和真实 Agent 即可完整演示。
2. `AC-1C-02 [component]`：所有写操作正确处理重复点击和后端冲突。
3. `AC-1C-03 [component,E2E]`：关键页面有组件测试和端到端测试。
4. `AC-1C-04 [integration]`：UI 不把“已入队”显示成“已送达”，所有状态来自结构化后端结果。
5. `AC-1C-05 [integration]`：UI 可见 `live/ready/degraded`、依赖、禁用 capability 和失败信息。
6. `AC-1C-06 [security-negative]`：前端 console、错误上报和埋点不含消息正文、secret 或原始身份材料，只允许 `correlation_id` 与结构化错误码。

### 4.5 第四组：平台集成、测试工程与发布工程

第四组不拥有业务域或业务权威数据。其负责合同与统一 Mock 发布、测试计划和用例模板、CI、Docker Compose、nginx、指标告警、备份、升级、回滚、跨组集成和 release set；目标服务器动作仍由运维组执行，各运行单元 owner 提供镜像与 health contract。其任务为 `DW02-000a/b`、`DW02-001`、`DW02-040`～`DW02-058`。

运行单元镜像唯一构建 owner：`edge/control` 为第一组，`message/worker/feishu-adapter/agent-runtime` 为第二组，`ui` 为第三组，`mock-feishu/mock-control/mock-agent` 为第四组。`control` 镜像由第一组构建并对 digest 负责，内含第二组以独立合同版本交付的 AGT 包；组内 E2E 时第一组使用 AGT fake，真实包在 `DW02-M3` 第 2 级接入。AGT 包版本变化必须触发 `control` 镜像重建和合同测试。`nginx/postgres/prometheus/grafana` 为受控第三方镜像，不计入自研运行单元构建 owner 分母，但必须锁定 digest。

完成门：

1. `AC-1D-01 [integration]`：空环境按文档执行 Compose 可拉起全部服务（含 `mock-*` 开发 profile），健康检查全绿，命令与输出留证。
2. `AC-1D-02 [contract]`：每个运行单元镜像均带 commit、构建时间、制品 SHA 与所含跨组包版本，registry 中可按 digest 取回；`latest` 不作部署锚。
3. `AC-1D-03 [component]`：每个容器的健康检查、资源限制与日志轮转均在 Compose 中可机器核验。
4. `AC-1D-04 [recovery/rollback]`：备份可在空库恢复并通过 migration 一致性检查；回滚命令在演练环境实跑成功，现有 vhost location 回归通过。
5. `AC-1D-05 [contract,integration]`：每个产品版本的测试计划、Mock 测试计划、集成测试计划、测试用例集和追踪矩阵均已冻结；独立测试负责人签收测试范围，但不代替各业务组签收合同语义。

## 5. 跨组合同与 Mock 纪律

总集成负责人是 `contracts/v0.2-mvp1` 的唯一 owner。`DW02-M0` 先产出合同，四组逐一签收职责相关边界后才开始实现；各组只按合同实现 handler、codec 和 consumer，不各造 schema 或 fixture。

冻结目录包含：

- `openapi.yaml`
- `asyncapi.yaml` 或等价事件 schema
- `canonical-envelope.schema.json`
- `routing-recipient.schema.json`，限定 `recipient_source=ui_explicit|channel_binding`，正文不得改变 recipient set
- `access-context.schema.json`
- `agent-status.schema.json`
- `error.schema.json`
- `internal-call-envelope.schema.json`
- `outbox-port` 合同，定义 claim/lease/renew/ack/dead-letter/fencing token、错误码和可注入 fake
- `audit-record.schema.json`
- `interface-registry.tsv`，记录 HTTP/WS path、目标架构 interface ID、owner 域、effect 和 `public|internal-only` exposure；EDGE location 由 exposure 列生成
- `redaction-rules.version`
- `mock-fixtures/`
- 合同版本与兼容规则

首批核心事件：

- `PrincipalBound`
- `AgentRegistered`
- `AgentHealthChanged`
- `SessionConnected`、`SessionDisconnected`
- `EnvelopeAccepted`
- `DeliveryQueued`、`DeliveryAcknowledged`、`DeliveryFailed`
- `TaskAssigned`、`TaskResultRecorded`

规则：

1. 字段只可兼容增加；改名、删字段或改变语义必须升合同版本。
2. command 必带 `request_id`。
3. event 必带 `event_id`、`schema_version` 和 `correlation_id`。
4. Agent 只见 canonical envelope，不感知飞书协议。
5. Mock 与真实实现共同运行 consumer-driven contract tests。
6. 合同变化必须同步四组并登记，不以口头约定替代。
7. 所有 internal command 带 `interface_id/request_id/access_ctx_ref`，有代际对象带 `expected_generation/state`。
8. 错误合同覆盖目标架构 §5.3 的完整错误集合；缺少政策或身份一律 fail-closed。
9. `mock-control` 和 `mock-fixtures` 只由总集成负责人发布；第一、三组不得各造不一致的假控制面。
10. 正文、secret 和原始身份材料不得进入普通 AuditRecord；审计只落结构化 metadata/ref 与 redaction version。
11. `SessionAuthRef` 在 connection → envelope → receiver 全程不可变；换代后旧 ref 不复用。
12. outbox port 由第一组定义并 owner，第二组 owner 消费实现；worker 只能经该受控 port 访问 outbox，不取得裸 DB/Tx。
13. 跨组集成进同一运行单元的包必须有独立版本号和兼容规则；包版本变化触发宿主镜像重建与 consumer/provider contract tests。

## 6. P0.2 部署拓扑

```text
Internet
   │
ts3poc.wegoab.com
   │
[宿主 nginx / TLS：独立 /dingwei-v02/ location]
   │
[dingwei-edge]
   ├── [dingwei-ui]
   ├── [dingwei-control] ───────┐
   ├── [dingwei-message] ───────┼──> [PLATFORM_DB + ENTERPRISE_DB]
   │        └──> [agent-runtime-01 ... agent-runtime-30]
   ├── [feishu-adapter] ───> [Feishu API]
   └── [dingwei-worker] ────────┘
```

`message ↔ agent-runtime` 只走受控 Agent 协议；`control/message/worker` 经受控 Repository/DB registry 访问数据库；`feishu-adapter` 只经 FCS 接口访问飞书和 control/message，不直连数据库；Agent runtime 不直连 PostgreSQL。图中服务名、数据库访问边集与本段枚举必须由 `DW02-001` 的计划状态检查逐项对撞。`P0.2` 不画、不部署 SIDE-01 紧急旁路；该能力在 `P0.6` 整体交付。

Compose 服务建议：

- `nginx`
- `edge`
- `ui`
- `control`
- `message`
- `worker`
- `feishu-adapter`
- `postgres`，同一实例内至少建 `dingwei_platform`、业务库 `dingwei_ent_<code>` 与仅供隔离负测的 `dingwei_ent_test`，registry 落平台库
- `agent-runtime`，通过 profile 或生成配置启动 30 个实例
- `mock-feishu`、`mock-control`、`mock-agent`，仅开发 profile
- `prometheus`、`grafana`，在 MVP 后半段纳入

`/internal/` 不配置公网 location；飞书回调、UI 和公共 API 均在独立 `/dingwei-v02/` 前缀下。服务器勘察必须列出既有 nginx server/location，保护现有 noVNC 与 `/test-pod-shared-data-file/`。如评审后改用独立子域，须另做 DNS/TLS 授权，不静默切换。

持久化 volume：PostgreSQL、消息恢复数据、Agent 工作目录、审计导出和备份。证书与 secret 只引用宿主受控路径。

以下部署门是 §4.5 共享发布工作流的动作纪律；交付出口以 §4.5 完成门为准：

1. 镜像带 commit、构建时间和制品 SHA，不以 `latest` 作正式部署锚。
2. migration 独立执行；失败时应用不得带病启动。
3. 容器设置健康检查、资源限制和日志轮转。
4. 发布前登记组件、环境、commit 和镜像 digest。
5. 未经授权不得直接操作目标服务器或飞书正式应用。
6. nginx 变更前备份到 `/etc/nginx/backups/`，变更后依次执行 `nginx -t`、受控 reload 和现有 location 回归；回滚恢复备份并再次 `nginx -t`。
7. migration 采用 expand-contract：任一发布的 schema 同时兼容当前版与前一版镜像；删列、删表和收紧约束延后至少一个发布并按目标架构 §14.3 另立不可逆门。回滚到 N-1 镜像后 smoke 必须通过。

## 7. P0.2 里程碑与集成计划

### DW02-M0：实施准备，计划 2～3 个工作日

- 前置：§10 的服务器只读勘察授权和飞书开发应用/管理员/测试群已到位；否则里程碑不开始计时。Agent 启停/选择与寻址机制在本里程碑内裁定，逾期按 §10 fail-closed。
- 总集成负责人产出并冻结 MVP 范围、API、事件、canonical envelope、internal call、audit/redaction 和 interface registry。
- 四组分别签收与本组职责相关的合同/测试边界；缺任一签收不得进入实现。
- 建立四个组的目录、CI、Mock profile 和合同测试；冻结 §9.1 前五类资产的模板：版本测试计划、可执行测试用例集、Mock 测试计划、集成测试计划与追踪矩阵；将本计划纳入文档闸，并为计划产物建立存在性/状态检查。
- 只读勘察目标服务器，输出资源、现有 nginx server/location、端口和 volume 清单。
- 冻结流量模型：消息/分钟、峰值倍数、消息大小分布、24h 总量、真实/模拟 Agent 比例，以及首投/终态 SLO 目标值和 24h 趋势阈值。
- 输出容量方法件与 `U-01` 关闭判据/取数口径；不在无 P0.2 实测数据时关闭 `U-01`。

退出条件：合同目录有总集成 owner 和四组签收，四组只依赖统一 Mock 均能启动；§9.1 前五类测试资产模板由独立测试负责人签收，证据清单与版本报告在执行后生成；容量方法件、资源上限、流量模型、SLO/趋势阈值和外部授权清单由业主签收。由总集成负责人判定并留锚。

### DW02-M1：最小骨架与地基，计划 5～8 个工作日

- 控制面可启动并连接 PostgreSQL。
- 消息面可接受模拟 Agent。
- UI 可读取 Mock Agent 名册。
- Compose 可拉起开发环境。
- 飞书 webhook challenge 通过。
- EDGE 剥离伪造身份字段，internal API 无公网暴露，redaction 负测通过。
- Repository seam、generation/CAS、outbox port/relay 与受信结构化 recipient set 的最小纵切可运行。
- 以骨架 + 模拟 Agent 回填首版容量实测表；只有目标机或业主确认的同规格机实测可在签收后放行 10/30 Agent 扩容。
- 四组提交本阶段功能/组件测试用例；1-d 执行 Mock 测试计划并产出第一版 Mock 测试报告。

退出条件：模拟飞书消息能够按结构化 recipient set 穿过系统并由模拟 Agent 回应；首版容量实测表、地基负测和合同测试留锚。若 §10 第 4 项逾期导致只能在非目标环境测量，则容量表标为 provisional，不放行扩容，扩容门顺延至 `DW02-M3` 取得目标机曲线。由总集成负责人判定并留锚。

若正式回调授权延迟，允许使用飞书开发应用和受控临时回调完成组内验证，但不得把临时入口登记为部署锚。

### DW02-M2a：数据面与合同实现，计划 3～5 个工作日

- 第一组完成身份、Repository seam、任务、COL-lite 和 outbox 写侧。
- 第二组完成 Agent/session、消息、worker、飞书和 Agent runtime 基座。
- 第三组完成 P0 UI：登录、Agent 列表、Agent 对话、健康页。
- 共享发布工作流完成 Compose、nginx 开发配置和核心指标采集。
- 1-d 汇总四组测试用例，更新需求—合同—用例追踪矩阵；独立测试负责人审核覆盖面。

退出条件：schema/migration、codec、handler、mock consumer/provider contract tests 全绿；generation/CAS、worker fencing、handle-escape、跨库隔离、recipient 正文不生效与 SessionAuthRef 不可变负测通过。由总集成负责人判定并留锚。

### DW02-M2b：业务面与组内 E2E，计划 3～5 个工作日

- 四组完成各自剩余 P0 交付面。
- 第三组 P1 页面（工作面、任务看板、渠道、投递/审计）在不影响主链出口时完成；未完成项不得阻断核心链，但须明确标“未启用”。
- 各组在不依赖其他组临时环境的前提下通过组内 E2E。
- 各组提交组内 E2E 用例与证据；1-d 形成进入组合集成前的测试就绪报告。

退出条件：单元测试、集成测试、合同测试和组内 E2E 全绿；`live/ready/degraded` 三态及企业 DB、MSG 两行降级各有一次实跑证据。由总集成负责人判定并留锚。

### DW02-M3：组合集成，计划 3～5 个工作日

按以下顺序接真：

1. 控制平面 + UI。
2. 控制平面 + 消息面。
3. 飞书适配 + 模拟 Agent。
4. 真控制平面 + 真消息面 + 真 UI。
5. 真飞书 + 3 Agent。
6. 扩至 10 Agent。
7. 扩至 30 Agent。

接真前必须冻结 `P0.2` 集成测试计划：环境、版本/镜像、拓扑、数据集、步骤、失败注入、预期结果、证据位置、回滚和判定人。每一级均按专门用例执行，不以临时命令代替测试计划。

任一级失败，先在当前规模修复，不跳级掩盖问题。

退出条件：七级全部通过，每一级均留 commit、镜像 digest、测试用例 ID、命令、日志/指标和判定人；形成版本集成测试报告、缺陷清单与需求追踪结果；形成 3/10/30 实测容量曲线、核定正式配额并产出 `U-01` 本计划侧关闭证据交原 owner 判定；`U-03` 的 PG 并发语义有明确 PASS/FAIL/INCONCLUSIVE。

### DW02-M4：30 Agent 稳定性验证，至少 24 小时

- 30 Agent 同时在线并可分别寻址。
- 30 Agent 同时收消息不串线。
- 首投成功率＝一次投递即 ACK 的 delivery/总 delivery，目标值由 `DW02-M0` 冻结。
- 终态成功率＝成功终态 delivery/总 delivery；永久失败必须进入可查询死信。对账恒等式：总 delivery＝成功终态＋死信＋观察窗关闭时仍在重试队列且低于冻结上界的 in-flight；四项分别取自 delivery ledger、dead-letter ledger 和 retry queue 指标，差值即静默丢失，判据为 0。in-flight 超上界即延长窗口或判 INCONCLUSIVE，不得吸收为成功。
- 以 `correlation_id` 对账每 Agent 应收集合与实收集合，两者相等，且不同 Agent 实收集合交集为空。
- 重复事件不产生重复业务 effect。
- 重启 message、worker、PostgreSQL 客户端连接和单 Agent 后可恢复。
- 飞书 429、短时 5xx 和超时可退避恢复。
- 日志可按 `request_id/correlation_id/delivery_id` 串联。
- 每分钟采样 RSS、goroutine、连接、队列深度和磁盘；24h 线性趋势阈值由 `DW02-M0` 流量模型冻结，越界即 FAIL/INCONCLUSIVE，不凭肉眼判 PASS。

最终数值 SLO 由业主在 `DW02-M0` 冻结；测试报告必须写对象、公式、分母、取数锚、观察窗口和判定人。

### DW02-M5：ts3poc 第一版发布，计划 1～2 个工作日

冻结 release set：Git commit、镜像 digest、Compose、env 模板、migration、飞书权限、健康检查、备份点、回滚命令和 30 Agent 验收报告。

本次只做与 v0.1 并存的 ts3poc 部署，不切换旧写权威、不停旧 outbox、不吊销旧 Agent generation。退出条件：release set 全项登记、回滚到 N-1 镜像后 smoke 通过、备份恢复与回滚演练成功、现有 vhost location 回归通过、业务验收人和总集成负责人签收。任何 cutover 另立授权与计划。

## 8. P0.2 工作分解编号

| 编号 | 任务 | 主责 |
|---|---|---|
| `DW02-000a` | 合同件产出与统一 Mock | 总集成负责人 |
| `DW02-000b` | 合同评审与四组签收 | 四组负责人 |
| `DW02-001` | 仓库、目录、CI、开发 profile；计划文档闸及计划产物存在性/状态检查（含组名映射、拓扑服务/边集与正文枚举对撞） | 总集成负责人 |
| `DW02-002` | EDGE 头清洗、限流、路由与信任边界 | 第一组 |
| `DW02-010` | AccessCtx、接口信封和错误模型 | 第一组 |
| `DW02-011` | enterprise、principal 与飞书身份绑定 | 第一组 |
| `DW02-012` | 项目/任务控制面与 Agent façade | 第一组 |
| `DW02-013` | Repository seam、migration、outbox 写侧、COL-lite 与审计 | 第一组 |
| `DW02-020` | canonical envelope codec | 第二组 |
| `DW02-021` | Agent instance、session 与在线目录 | 第二组 |
| `DW02-022` | 投递、ACK、重试、死信与 fencing | 第二组 |
| `DW02-023` | 飞书事件接入与回复 | 第二组 |
| `DW02-024` | Agent runtime 镜像与 30 Agent 工具 | 第二组 |
| `DW02-025` | JOB outbox relay、lease、fencing 与 reconcile | 第二组 |
| `DW02-030` | Web 骨架与权限守卫 | 第三组 |
| `DW02-031` | Agent 列表与工作面 | 第三组 |
| `DW02-032` | Agent 对话与消息状态 | 第三组 |
| `DW02-033` | 任务看板与只读审计时间线 | 第三组 |
| `DW02-034` | 渠道与 `live/ready/degraded` 健康页 | 第三组 |
| `DW02-040` | Docker Compose、nginx 与部署脚本 | 总集成负责人 + 运维 |
| `DW02-041` | 可观测、备份与回滚 | 总集成负责人 + 运维 |
| `DW02-042` | 运行单元镜像构建、跨组包版本与制品登记 | 总集成负责人 + 各运行单元 owner |
| `DW02-050` | 跨组合同测试 | 总集成负责人 + 四组 |
| `DW02-051` | 3/10/30 Agent 分级联测 | 四组 + 独立测试 |
| `DW02-052` | 24 小时稳定性测试 | 运维 on-call + 独立测试 |
| `DW02-053` | ts3poc 发布和部署锚登记 | 运维 + 总集成负责人 |
| `DW02-054` | 各版本测试策略、模板与追踪矩阵 | 1-d + 独立测试负责人 |
| `DW02-055` | 各组功能/组件测试用例集 | 1-a/1-b/1-c/1-d 各自产出，独立测试审核 |
| `DW02-056` | 各版本 Mock 测试计划、用例、执行证据与报告 | 1-d + 合同 owner |
| `DW02-057` | 各版本集成测试计划、用例、环境与回滚编排 | 1-d + 四组负责人 + 运维 |
| `DW02-058` | 独立测试执行报告与 PASS/FAIL/INCONCLUSIVE 签字 | 独立测试负责人 |

## 9. 测试交付、质量门与风险

### 9.1 每版本测试输出

`P0.2`、`P0.3`、`P0.4`、`P0.5`、`P0.6`、`P1.0` 每个版本都必须独立产出以下受版本控制的测试资产，不得只在消息、临时日志或口头验收中存在：

| 产物 | 建议路径 | 最小内容 | owner / 签字 |
|---|---|---|---|
| 版本测试计划 | `tests/<version>/test-plan.md` | 范围、非范围、风险、环境、数据、级别、进入/退出条件、人员、时间窗 | 1-d 编排；独立测试负责人签收 |
| 可执行测试用例集 | `tests/<version>/test-cases.tsv` | case ID、需求/合同锚、级别、前置、fixture、步骤、预期、owner、自动化状态、证据位置 | 各组产出本组用例；独立测试审核 |
| Mock 测试计划 | `tests/<version>/mock-test-plan.md` | provider/consumer、fixture、固定时钟/ID、故障矩阵、限流/超时/重放、合同版本、退出条件 | 1-d owner；相关业务组签语义 |
| 集成测试计划 | `tests/<version>/integration-test-plan.md` | 组件组合顺序、镜像 digest、环境拓扑、数据准备、失败注入、回滚、证据和判定人 | 1-d owner；四组、运维、独立测试签收 |
| 追踪矩阵 | `tests/<version>/traceability.tsv` | requirement/contract/interface → case ID → evidence → result | 1-d 维护；独立测试核验 |
| 测试证据清单 | `tests/<version>/evidence/manifest.tsv` | case ID、commit、镜像、命令、日志/指标/截图路径、SHA256、执行人和时间 | 执行者写入；独立测试抽验 |
| 版本测试报告 | `tests/<version>/test-report.md` | PASS/FAIL/INCONCLUSIVE、覆盖率、未测项、缺陷、风险接受、复测记录 | 独立测试负责人唯一签字 |

`test-cases.tsv` 至少区分 unit、component、contract、Mock、integration、E2E、security-negative、capacity、recovery/rollback 九类。unit 是所有实现包的基线自测类别，不作为 REQ/AC 的唯一最低类别，但每个生产包至少有一条 unit case；`REQ-P02-*` 与 `AC-1A-*`～`AC-1D-*` 是稳定追踪 ID，各组完成门方括号内为最低非 unit 测试类别。每个稳定 ID、blocker/important 风险、对外或 internal interface、允许/禁止 effect 都必须落到至少一个 case ID。`DW02-054` 必须机器核验稳定 ID 无重复、无遗漏且每个 ID 至少映射一个用例；无用例、无证据或无追踪关系不得计为已测试。

Mock 测试不是开发自测的附注，而是正式测试输出：必须覆盖正常、超时、429、5xx、重复、乱序、迟到 ACK、断线重连、lease 过期、fencing、权限拒绝、redaction 失败和部分依赖降级。Mock 与真实实现必须运行同一 consumer/provider contract tests；Mock PASS 不等于集成 PASS。

每个版本进入组合集成前必须先冻结该版本的集成测试计划；版本范围或合同变化时，测试计划、用例和追踪矩阵必须同提交更新。集成测试报告必须逐级记录组件组合、失败停留点和复测结果，不允许跳级掩盖失败。

### 9.2 每版本共同质量门

- 范围、合同和验收标准先冻结。
- 开发自测与独立测试分离。
- Mock 和真实实现使用同一合同测试。
- 版本测试计划、测试用例集、Mock 测试计划、集成测试计划和追踪矩阵先于执行冻结；测试报告与证据清单后于执行生成。
- 开发者可以编写和执行本组用例，但不得为自己的主要交付签独立测试 PASS。
- 不允许跳过失败测试或把 UNKNOWN 记为 PASS。
- 发布只来自干净 commit 和可复现镜像。
- 数据 migration、备份、恢复和回滚均有演练证据。
- 生产或正式渠道动作必须逐项授权。
- `specs/v0.2/check.py`、仓库提交闸与既有未闭 RV 在每个里程碑保持可见；UNKNOWN 不得折叠为 PASS。
- 触碰目标架构 §14.3 任一不可逆点前，必须另立门、回滚决策和业主授权。
- `live/ready/degraded` 与 §9.4 允许/禁止效果进入故障演练；健康检查不得以 SystemCtx 读取业务行证明数据库正常。
- 原始正文、secret 和身份材料不进入普通日志/审计；redaction 失败不得 fail-open。

### 9.3 当前主要风险

| 风险 | 影响 | 控制措施 |
|---|---|---|
| 30 个真实模型 Agent 的资源与费用未确定 | P0.2 容量口径不清 | `DW02-M0` 冻结真实/模拟比例、供应商并发、预算和追加节点门；`DW02-M1/M3` 分两次回填实测 |
| 单机资源不足或与现有服务冲突 | 无法稳定部署 | 部署前只读勘察，按 3/10/30 曲线分配资源 |
| 四组接口漂移 | 集成延迟 | 先冻结合同，consumer-driven contract test 阻断不兼容变更 |
| 独立测试单席不可用 | 版本 verdict 无人可签、24h 窗口中断 | 运维 on-call 连续记录并交接；独立测试抽验和终判；其缺席时顺延或记 INCONCLUSIVE，任何角色不得代签 |
| 运维单席不可用 | 24h 记录中断、部署/回滚无人执行 | 自动告警保留观测；业主在变更窗口前另指备用 on-call；缺席时窗口/发布顺延或记 INCONCLUSIVE，任何开发/测试席不得代操作 |
| 独立设计评审席冲突或回避 | 设计变更失去独立门 | 两席互为回避替代；二者均回避时由业主另派未参与交付的门方 |
| 实现范围增长而工期未重估 | 地基被压缩、退出门降格 | `DW02-M0` 由总集成负责人按冻结范围重估；`DW02-M1` 已增至 5～8 天，`DW02-M2a/M2b` 的天数须在 M0 按冻结负测/演练范围重估后回写正文，不沿用评审稿数值 |
| 飞书限流、重试和重复事件 | 重复 effect 或消息堆积 | 快速 ACK、异步处理、幂等、退避和死信 |
| UI 原型直接照搬静态状态 | 真实错误和权限面缺失 | 补齐加载、失败、权限、确认、重试和审计交互 |
| MVP 把后续完整治理提前做完 | 版本失控 | 严格执行第一版不做清单，后续只保留接口边界 |
| 既有 v0.1 与新链路关系不清 | 误切写权威或破坏回滚 | P0.2 仅并存；cutover 另立计划和授权 |
| `RV-执笔01/RV-丙01/RV-甲08` 仍未闭 | 设计/仓储边界不能被实施计划代签 | `DW02-M0` 实取 REVIEW-STATE；各原 owner 裁定，计划只登记不越权闭项 |
| ts3poc 既有 vhost location 冲突 | noVNC/共享目录回归 | 独立路径、全量 location 勘察、备份、nginx -t、reload 与回滚 |

## 10. 开工前业主确认清单

下表的“截止”是里程碑门，不是当前文档日期承诺；责任人姓名由业主填入。逾期默认一律收紧，不自动视为同意。

| # | 确认项 | 责任角色 | 截止 | 逾期默认 |
|---|---|---|---|---|
| 1 | 指定四个开发小组及负责人 | 业主 | `DW02-M0` 前 | 不开工 |
| 2 | 确认 1-d 负责人兼总集成负责人、独立测试、两席独立设计评审、主运维与备用 24h on-call | 业主 | `DW02-M0` 前 | 不开工 |
| 3 | 授权 `193.112.157.47` 只读勘察 | 业主 + ops | `DW02-M0` 前 | 不接触服务器 |
| 4 | 勘察后逐项授权 Docker/nginx/PostgreSQL | 业主 + ops | `DW02-M1` 前 | 仅本地/Mock；M1 容量表降为 provisional，不放行扩容 |
| 5 | 准备飞书开发应用、管理员、权限、测试群与回调 | 业主侧飞书管理员 | `DW02-M0` 前 | `DW02-M0` 不起算 |
| 6 | 确定 30 Agent 全真实或分阶段比例 | 业主 | `DW02-M0` | 默认真实 3 + 模拟 27，只能验协议容量 |
| 7 | 确定 Agent 类型、模型并发、预算和追加节点门；M0 签方法/上限，M1/M3 签实测 | 业主 | `DW02-M0/M1/M3` | 不承诺 30 个真实模型 Agent |
| 8 | 指定试用人员和业务验收人 | 业主 | `DW02-M2a` 前 | 不进入真实业务联测 |
| 9 | 确认 P0.2 单企业、飞书单渠道、仅 ts3poc 试点 | 业主 | `DW02-M0` | 按最小范围，禁止生产转述 |
| 10 | M0 冻结流量模型、SLO、24h 窗口、趋势阈值和演练范围；M1/M3 回填实测 | 业主 + 总集成负责人 | `DW02-M0/M1/M3` | 不进入 `DW02-M4` |
| 11 | 裁定 Agent 新建/关闭/重启、普通用户选 Agent 权限，以及 UI 显式选择/飞书会话绑定两类结构化寻址机制 | 业主 | `DW02-M0` | effect fail-closed；寻址只允许 UI 显式选择，不从正文推断 |
| 12 | 对 `RV-甲08` 给出原 owner 裁定，并确认另两条 RV 归口状态 | 业主 + ops/coordinator | `DW02-M0` | 保持阻断可见，不由本计划代闭 |

## 11. 评审收拢与处置

评审对象为评审稿 `cd1d08fa0137ca12be668792aee23556605beb8f`。三席均按全文实施计划评审，不重开 SP3、原型或架构。

| 评审席 | 结论 | 主要发现 | 执行版处置 |
|---|---|---|---|
| `zzc-opus50tchangshark-2642` | NO；3 阻断、12 重要、6 建议 | worker 无 owner、权威对象双 owner、EDGE/sidecar 不一致；设计锚、版本轴、COL 分层、CAS、Repository seam、SLO、第三组过载、授权前置、vhost、资源、Mock、健康语义 | 全部采纳或显式后移；阻断均在 §§1、4～8 闭合 |
| `zzc-opus50tczc2000-2642` | NO；5 阻断、12 重要、7 建议 | EDGE 剥离不足、internal PEP 缺失、里程碑撞名/无迁移、合同双 owner、redaction 缺失；worker、拓扑、CAS、SLO、资源、值守、降级、对象存储、RV/编号 | 全部采纳或显式后移；最小闭合集均进入正文与 WBS |
| `zzc-chatgpt56solzhangzongchu-2642` | NO（自审） | Agent runtime 拓扑误画成依赖 DB、资源建议值不可作采购承诺、SLO 缺公式、授权门与执行门未分离 | 与两席意见合并，分别落 §§3、6、7、10 |

### 11.1 发现到落点

| 归并发现 | 决定 | 执行版落点 |
|---|---|---|
| EDGE、internal PEP、Agent 权限 | 采纳 | §§1.2、4.2、4.3、5、6、8、10 |
| worker/JOB 无 owner | 采纳，唯一 owner 第二组 | §§4.1、4.3、8 `DW02-025` |
| 权威对象双 owner、COL 错层 | 采纳 | §4.1 唯一 owner 表；第三组只读展示 |
| sidecar 半实现 | 后移，不做半套 | §§1.3、2.5、4.3、6；整体进入 P0.6 |
| 合同与 Mock 双 owner | 采纳，总集成唯一 owner | §§5、7 `DW02-M0`、8 `DW02-000a/b` |
| CAS、DATA-01 seam、worker fencing | 采纳 | §§4.2、4.3、7、8 |
| redaction/AuditRecord | 采纳最小集 | §§1.2、4、5、9 |
| 里程碑/版本轴撞名 | 采纳并消歧 | §§1.1、2、7、8 |
| v0.1 双轨/cutover 缺失 | P0.2 仅并存；cutover 另立计划 | §§1.1、2.6、7 `DW02-M5`、9 |
| SLO/30 Agent 不可复算 | 采纳公式、流量模型和集合对账 | §§3.2、7 `DW02-M0/M4`、10 |
| 第三组过载、发布责任错误 | 采纳，UI 分 P0/P1；共享发布归总集成+ops | §§4.4、4.5、7、8 |
| vhost 冲突与 nginx 回滚 | 采纳独立路径与勘察回归 | §§3.2、6、7、9 |
| 健康/降级语义缺失 | P0.2 做 live/ready/degraded 与 DB/MSG 两行；safe_mode 后移 | §§1.3、2.1、4.4、7、9 |
| 对象存储时点 | P0.3 只存 ref；保存 blob 时资源前移 | §3.3 |
| 设计锚、基线闸、未闭 RV | 引用单一 manifest，不复制第二真相源；RV 保持原 owner | 文档头、§§9、10 |
| 计划入闸与路径锚 | 采纳为机械任务；待建路径不冒充现存锚 | `DW02-001` 在 `DW02-M0` 将本件纳入合适文档闸，并为计划产物建存在性/状态检查 |

### 11.2 第二轮全文评审收拢

第二轮冻结对象为 `c5571bf5a775abc464adb7e4a13c91f4dcbe789c`。两份 reviewer-owned 全文意见分别位于专用评审分支，均以该锚为唯一父提交；本轮按两份全量 findings 一次性修订，不做单点往返。

| 归并发现 | 决定 | 本轮落点 |
|---|---|---|
| M0 容量实测门自锁、`U-01` 越权关闭 | 拆为 M0 方法件、M1 首测、M3 定稿；只向原 owner 交关闭证据 | §§2、3.2、7、10 |
| recipient 来源缺失、正文可能参与路由 | 只允许 UI 显式 command 与 IDN/AGT 会话绑定；新增结构化合同与负测 | §§1.2、2.1、3.2、4.3、5、7、10 |
| outbox port 未冻结、第二组无法独立 Mock | 第一组 owner port 合同，第二组 owner consumer；新增 fake 与 fencing 语义 | §§4.1、4.3、5、7 |
| 拓扑边错误、AGT 交付责任与运行单元混用 | 重画拓扑；AGT 包由第二组交付但运行于 control，message 只承载 MSG | §§4.2、4.3、6 |
| façade CAS 门不可证伪 | CAS 只覆盖第一组权威对象；Agent 代际由第二组判定并透传错误 | §4.2 |
| baseline provenance 错标且无机器校验 | 改为真实末次改动锚；全行 source 统一完整 commit 并做指纹、可达性与覆盖见证 | 文档头、`baseline-manifest.tsv`、`spec.md`、`check.py` |
| 单 postgres 与 DB registry/跨库边界不闭 | 同实例双 database、opaque key 选库、跨库 query-count 负测 | §§4.2、6 |
| SessionAuthRef 不可变传播无门 | 加入 envelope 合同和第二组完成门 | §§4.3、5 |
| 发布工作流无完成门、migration N-1 回滚无规则 | 新增四项发布完成门与 expand-contract/N-1 smoke | §§4.5、6、7 |
| 静默丢失为 0 不可计量 | 增 delivery 对账恒等式、取数锚和 in-flight 关窗规则 | §7 `DW02-M4` |
| 计划入闸、健康演练处置表宣称但正文无落点 | 补 WBS/M0 机械任务与 M2b 三态/降级实跑出口 | §§7、8 |
| 30 Agent 目标口径、工期、里程碑前缀、判定人不闭 | 统一比例声明；M1 地基加时；改用 `DW02-M*`；各出口留锚 | §§1～3、7、9、10 |
| UI redaction、EDGE exposure、notification 时点 | 加 UI 泄漏门、registry exposure 列、notification 后移标注 | §§4、5 |

### 11.3 第三轮全文评审收拢

第三轮冻结对象为 `4c826fb46dedafa218d1250ea377d5e41415b1b2`。两票为一张 NO、一张 PASS，整轮按 NO 处理；两票的阻断、重要和建议均进入下表逐条采纳、不采纳或显式处置，不因 verdict 不同而整批吸收或丢弃。

| 归并发现 | 决定 | 本轮落点 |
|---|---|---|
| provenance 仅覆盖 2/12、伪造短锚可绿、无覆盖见证 | 阻断闭合：12 行全部改完整真实锚，invalid/unreachable/mismatch 均 FAIL，打印预期/实检/未标注/未知集合 | 文档头、`baseline-manifest.tsv`、`spec.md`、`check.py` |
| provenance 锚不校 ancestor、可达性清单与 manifest 分叉 | 从 manifest 派生 source ancestry 集合，另保留两个历史锚；不可达错误与内容漂移分开 | `spec.md`、`check.py` |
| source 尾部自由文本 PASS 无第二载体 | 删除自由文本 verdict，source 只存可复算 commit | `baseline-manifest.tsv` |
| FCS/MSG 数据库边与图注不一致 | 保持架构 §9.2：control/message/worker 连 DB，FCS 不直连；增加边集机械对撞任务 | §§6、8 `DW02-001` |
| P0.2 跨企业负测缺第二测试库 | 增 `dingwei_ent_test`，仅供负测、不构成多企业运营 | §§3.2、4.2、6 |
| control 跨组包导致镜像 owner/触发不明 | 按运行单元指定唯一构建 owner，AGT 包独立版本并触发 control 重建；新增制品 WBS | §§4.5、5、8 `DW02-042` |
| M1 容量实测环境未定义 | 表增测量环境；非目标机一律 provisional 且不放行扩容 | §§3.2、7、10 |
| `DW02-M*` 命名建议 | 不采纳；保留全限定名，禁止在证据/状态中使用裸 `M2`，避免与目标架构迁移阶段混写 | §1.1 |
| M2a/M2b 新增出口未重估 | 采纳：M0 按冻结负测/演练范围重估并回写正文，不沿用评审稿天数 | §9.3 |

### 11.4 第四轮与业主新增裁定

第四轮只由第三轮 NO 方按业主裁定对 `ce5384b633fd0936da1bfd5cda09d66e15394745` 全文续评，结论 PASS；该票另带 2 条重要与 4 条建议。随后业主把三组改为四组并行，并要求测试成为每版本的独立交付体系。

| 发现/裁定 | 决定 | 落点 |
|---|---|---|
| 两条整合父线 `4220195/6a98629` 退出 ancestry 守护 | 采纳，恢复为历史锚；manifest source 仍动态派生 | `spec.md`、`check.py` |
| 第三轮 PASS 票 findings 状态不可判 | 采纳，改为两票 findings 均逐条处置 | §11.3 |
| mock 三件缺镜像 owner | 采纳，归第四组；第三方镜像明确不计自研分母但锁 digest | §4.5 |
| provenance 未标注/未知为硬编码字面量 | 采纳，改为实算计数变量 | `check.py` |
| source 锚不可达与 path 不存在误用同一文案 | 采纳，先判 commit、再判 path，分别报错 | `check.py` |
| source 第四列语义标签建议 | 不采纳；source 只存可复算 commit，语义留 commit message/评审件，避免恢复自由文本第二真相源 | `spec.md` |
| 四组并行编制 | 采纳 1-a/1-b/1-c/1-d 正式编组，运维独立 | §4.0～4.5、§§7～10 |
| 每版本必须有专门测试输出 | 采纳测试计划、用例集、Mock 计划、集成计划、追踪矩阵、证据清单和独立报告 | §9.1、`DW02-054`～`058` |

### 11.5 四组/测试体系评审与业主裁定

冻结锚 `11be2c9e09a14f812b57ab46a40e0ce30ad6fff2` 获独立测试门方 PASS、计划门方 NO；整轮按 NO 收拢。计划门方对自身席位去向回避，业主随后明确组内双角色、1-d 负责人兼总集成、独立测试单席及两席组外独立设计评审。

| 发现 | 决定 | 落点 |
|---|---|---|
| 既有评审席全部进入交付组 | 新增 `deepseekv4pro08130ca16c13` 与 `shencedeepseek` 两席独立设计评审，与 1-a～1-d 互斥 | §§3.2、4.0、9.3、10 |
| 总集成负责人不在编组表且身份双解 | 明确由 1-d 设计开发席兼任，并限制不得自签 | §§3.2、4.0 |
| 1-a～1-d 与第一～第四组双名无映射 | 固定等价映射并加入 `DW02-001` 对撞 | §§4.0、8 |
| §3.2 人数建议与 §4.0 点名双轴 | §3.2 只保留能力要求，§4.0 为唯一人选事实源 | §§3.2、4.0 |
| 独立测试单席风险 | 缺席时顺延/INCONCLUSIVE、不代签；24h 由运维记录、独立测试抽验终判 | §§4.0、9.3、10 |
| 需求和完成门缺稳定追踪 ID/测试类别 | 新增 `REQ-P02-*`、`AC-1A-*`～`AC-1D-*` 与最低测试类别，要求机器核验用例映射 | §§1.2、4.2～4.5、9.1、8 `DW02-054` |

### 11.6 新独立设计评审收拢

冻结锚 `343630f1aeb5536a561e6204792bef9f10ae99e4` 获一张 PASS、一张 NO，整轮按 NO 收拢。业主明确本项目按 AI Agent 会话编组，故独立性以稳定 DingWei 会话为执行 principal；不以共享 Git 身份或账号所有者推断自然人同一性。

| 发现 | 决定 | 落点 |
|---|---|---|
| 职责分离未按自然人去重 | 显式裁定会话级独立；禁止代签，业务授权不替代评审/测试证据；自然人互异若成为要求则另增真人资源 | §4.0 |
| 运维单席风险 | 采纳，新增备用 on-call 前置、缺席顺延/INCONCLUSIVE 与禁止代操作 | §§4.0、9.3、10 |
| 测试前五类模板与七类产物口径 | 采纳，M0 直接引用 §9.1 前五类，后两类执行后生成 | §§7、9.1 |
| GOV-lite 范围缺失 | 采纳，限定五类 effect 与明确不做面 | §4.2 |
| 当前执行版锚、1-d 自签、组内测试术语、测试基建否决权、业务人员占位、组名别名、unit 映射 | 全部采纳或显式裁定 | 文档头、§§4.0、4.5、9.1 |

### 11.7 执行版效力

本执行版完成“任务可分派”的结构收口，但不代替 §10 的业主资源与权限确认，也不构成服务器、飞书正式应用、生产、cutover 或不可逆 migration 的授权。只有 `DW02-M0` 退出条件满足后，四个开发小组才进入实现。
