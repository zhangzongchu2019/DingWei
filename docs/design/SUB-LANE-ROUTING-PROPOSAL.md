# 同址分道：一个 DingWei 地址下的子 Agent 消息路由（提案 v0.3，业主已定 〔51〕-补2）

起草：opus50tc2k（组长），2026-11-08。基线 opus50tc2k/r54-integration 6e2c8364。依据 rulings 〔51〕：业主不赞成「一个会话一个地址」，问能否在现有路由体系上对子 Agent 路由。本件只是提案，业主定前不写代码。

## 1 问题与两个现场样本

- 地址＝`会话名＋key`（`internal/m8/hub.go` parseAddress，两段式）。一个地址只对应一个 helper，helper 只驱动一个模型会话（Codex：`codexDriver.py` 单个 `_thread_id`，`SH_CODEX_RESUME_THREAD_ID` 固定绑定）。
- 样本一 ts3dev55（2026-11-08 自查报告）：地址背后两个 Codex 会话 01a1b202／01a1b6fa，helper 只绑 01a1b202；正文写「致 01a1b6fa」不起路由作用 ⇒ 5 条错路由已执行；高频来信与长回合共用一条串行队列 ⇒ 入场超时 101 次。
- 样本二 coder56solzzc（同日）：派单点名会话 01a19c8c，实际投到 01a1b9ec，后者按规矩只读拒接；靠写锁规则绕过。
- 共因：路由目标写在正文里（或靠约定），系统不认；一个地址只有一条队列。

## 2 方案：地址不变，加结构化「子目标」（lane）

1. **能力协商**：helper 在既有 `session_capabilities` 握手（hub.go handleSessionCapability，与 delivery_ack_v1 同一处）新增 `lanes_v1=true` 与分道清单 `lanes=[{name, kind, state}]`。name 为短名（如 main、r2），kind＝codex_thread 等；模型会话 ID 只留在 helper 本地，不上报 Hub。
2. **信封字段**：`meta.lane`。不带 ⇒ 默认分道（与今天完全相同，旧发送方零改动）。
3. **Hub 只核对、不猜**：带 lane 时，对端未报 `lanes_v1` 或 lane 不在清单 ⇒ 投递失败并明确回告发送方（`lane_unknown`／`lane_unsupported`），不回落默认分道。Hub 不解析正文。
4. **helper 分道驱动**：每个分道一条队列、一个模型会话；分道之间互不阻塞。Codex：同一 app-server 连接上对每个 thread 分别 resume／turn/start（协议本身支持多 thread）。账本（inbox、ack 游标）主键加 lane。
5. **回程**：分道内 Agent 发出的信带 `meta.from_lane`；对方回复时 Hub 自动把 `lane` 置为它 ⇒ 从哪来回哪去。
6. **界面与工具**：在线清单显示「ts3dev55.zzc（子：main、r2）」；send.py 加 `--lane`；View 输入框旁加分道下拉，投递状态按分道显示。
7. **提及（AMR）**：第一期不扩展正文提及语法（不引入新的半角符号写法，避免整封被吞）；分道只经结构化字段指定。

## 3 不做／边界

- 不改地址语法、不改权限与编组模型：分道继承地址的全部权限，不是新身份。
- 不让 Hub 持有或转发模型会话 ID；分道名到会话的映射只在 helper 本地，由席位主人配置。
- Claude 席第一期不做（单进程单会话；可行做法是一分道一进程，另行设计）。
- 与正在进行的 S-HELPER-CODEX-DELIVERY-RECONCILE（终态事件与入场对账）同一块驱动代码 ⇒ 建议排在其后。

## 4 改动面与估算（未调研，只给区间）

| 面 | 内容 |
| --- | --- |
| Hub | 能力字段、lane 校验与失败原因、回程 from_lane、在线清单展示 |
| helper | 分道配置、每分道队列与驱动实例、账本加 lane、握手上报 |
| send.py | `--lane` 参数 |
| View | 分道下拉与按分道的投递状态 |

粗估 5～12 人日（Hub、helper、send.py、View 四处；未调研）。先由 coder56 在 helper 修复第一步分析后出详细设计与估算，再请业主定。

## 5 待业主定

1. 方向是否可行（地址不变＋结构化分道）。
2. 第一期是否只做 Codex 席。
3. 分道配置由谁维护（席位主人在 Console 配，或 helper 本地配置文件）。

## 6 v0.2：业主提出的分层全名写法（2026-11-08T10:34:55Z，delivery_id dw-dlv_9fdd06a98e071a1a284b98707d826459）

业主写法（从右往左逐级细化）：
1. `ts3dev55` ⇒ 发送方所属用户下的主 Agent，发出时自动补成 `ts3dev55.zzc`
2. `ts3dev55.zzc` ⇒ zzc 用户下 ts3dev55 主 Agent（即今天的全名）
3. `r2.ts3dev55.zzc` ⇒ 其 r2 子 Agent
4. `r21.r2.ts3dev55.zzc`、5. `r211.r21.r2.ts3dev55.zzc` ⇒ 子子、子子子 Agent
- 提及（人机交互默认规则）只支持 1、2 两种；子 Agent 由 Agent 感知后寻址，例如 01a1b202、01a1b6fa 为 ts3dev55 的子 Agent，01a1b6fa 派生的 14a3c7eb 记作 `14a3c7eb.01a1b6fa.ts3dev55.zzc`。

### 6.1 与现有代码的相容性（基线 6e2c8364 核）
- Agent 名 `identity.ValidAgentName`、用户短名 `identity.ValidShortName` 均只允许字母数字（无点号）⇒ 点号分段无歧义；最右段＝用户，次右段＝Agent，其余＝子路径。
- 现有全名解析（如 `internal/m8/console_mux.go` 取最后一个点，左侧须是合法 Agent 名）遇到三段以上会**拒绝**而非误投 ⇒ 旧 Hub／旧客户端对新写法天然 fail-closed。实现时须全仓列出所有全名解析处逐一改（含 send.py、View、AMR）。
- 子 Agent 段沿用字母数字规则；01a1b6fa 这类 8 位十六进制会话号前缀合法。

### 6.2 Hub 侧路由规则（v0.2 取代第 2 节第 2～3 条的 meta.lane 外显形式；内部仍以结构化字段传给 helper）
1. Hub 解析目标：最右两段定位端点（与今天同一条投递链路、同一权限判定）；其余段按从近到远存为 `meta.sub_path`（如 `["r2","r21"]`），**不改动正文**。
2. 只核对、不猜：端点未报 `sub_agents_v1`，或子路径不在 helper 上报的树内 ⇒ 投递失败并回告发送方（`sub_agent_unsupported`／`sub_agent_unknown`）；**不回落到父 Agent**。
3. 回程：子 Agent 发出的信，发件人写完整路径（如 `r2.ts3dev55.zzc`）；对方回复即自动回到该子 Agent。
4. 层数上限：子路径最多 3 段（即业主示例第 5 种为最深），超出即拒。
5. 权限：子 Agent 继承主 Agent 的全部权限与编组，不是新身份；Console／在线清单以树形展示。

### 6.3 Agent 感知子 Agent
- helper 在握手与变化时上报子 Agent 树（名字、父、状态、类型），并把「本会话完整路径＋直接子 Agent 清单」告知每个会话（回合开头注入一行系统说明），使 Agent 能按全名寻址自己的子 Agent 与兄弟。
- 命名：默认用模型会话号前 8 位（碰撞时加长）；可另配别名（如 r2）指向同一会话，两种名字都可寻址。
- 生命周期：子 Agent 关闭后，发往它的信明确失败；不自动转父。

### 6.4 提及（AMR）
- 只认 1、2 两种（与业主定一致）。正文里出现三段以上的提及写法 ⇒ 不投递该提及，并给发送方明确提示「子 Agent 请用结构化目标」，不整封丢弃（须与现有 fail-closed 规则一起设计并测）。

### 6.5 未核实、须实测
- Codex：同一 app-server 上多 thread 并行驱动可行（协议有 threadId 参数）；但 Agent 自己派生的子会话（第 4、5 种）helper 能否观察到、能否向其投递，须实测。
- Claude 席：模型内的子 Agent 只在一个回合内存在、不能单独收信 ⇒ 第一期 Claude 席只支持第 3 种（一分道一进程）或不支持，须设计。

### 6.6 待业主定（替换第 5 节）
1. 采用分层全名写法（6.2）＋子 Agent 不存在即拒、不回落父。
2. 层数上限 3。
3. 第一期范围：Codex 席的第 3 种（helper 管理的并列会话，即 01a1b202／01a1b6fa 这类）；第 4、5 种待实测后第二期。
4. 估算仍为未调研区间（第一期约 6～14 人日，Hub＋helper＋send.py＋View＋AMR），由 coder56 在 helper 修复第一步后出详细设计再报。

## 7 业主已定（2026-11-08T10:41:28Z，delivery_id dw-dlv_f0dd4b425ce327a74d8c530c9f4e5dfa）

1. 子 Agent 不存在 ⇒ 拒收，同时提醒发送方「子 Agent 不存在」。
2. 层数上限 3，写入 DingWei 平台规则 ⇒ openspec/changes/sub-agent-addressing（AA-002）。
3. 第一期只做 Codex 席第 3 种（helper 管理的一级子 Agent）。
