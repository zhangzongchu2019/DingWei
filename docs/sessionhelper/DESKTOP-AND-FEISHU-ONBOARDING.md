# sessionHelper 桌面接入与飞书认领式入网

状态：0.2.2.2 开发中，向导/桌面收件箱与 MCP/飞书认领代码已落地，尚未部署。基线：0.2.2.1 / 9c14fe714b02e702c193a8a6fd439bc76eb1ebc5。
本设计源于业主指定的安装向导、Codex 桌面 MCP 接入与免预开户需求。

## 用户流程

1. 安装并运行 sessionHelper，先选择「Codex 桌面应用」或「CLI 托管」。两种方式都安装 sessionHelper。
2. 桌面方式：检测应用与 Codex 配置位置，配置本地 DW-MCP；保留现有 MCP 和配置。检测后提示在原 Codex 桌面会话启用连接。没有桌面应用时提供官方安装入口，不能悄悄改选 CLI 或后台创建另一会话。
3. CLI 方式：选择 Codex、Claude Code 或 OpenCode；安装所选工具当前稳定版本，记录实际版本与来源；存在工具时保留认证与配置，通过原生更新流程升级。向导提示「本安装模式使用 DingWei Web 页面交互」，仍保留程序的实际本地诊断能力。
4. CLI 登录：Codex 的 ChatGPT 认证提供设备码和浏览器两种原生路径；Claude Code 使用原生浏览器授权；OpenCode 按选择的 provider 进行原生认证。API/provider 密钥由工具自己的安全存储管理，不送往 Hub。工具登录成功还需一次明确授权的小型真实模型调用成功，才能标为 CLI_READY。
5. 填写 Hub 地址、希望使用的用户 ID、简称、Agent 名称与独立状态目录。明确用户 ID 与简称不同，不再以简称查询 owner。
6. 默认显示「通过飞书完成开户与配对」。生成短期配对指令，用户私发给该 Hub 所指定的官方机器人；不要求先登录平台或联系管理员建账号。
7. 飞书确认成功后，安装端从其独立受限轮询接口取得账号与 Agent 结果，凭据只写本机私有状态目录，再连接 Hub。
8. 返回准确用户 ID、Agent 全名、Web 入口以及桌面 MCP/CLI 状态。允许之后在 Web 调整名称与编组。已有账号仍可选择原 Web 审核路径。

## 桌面模式的验收边界

0.2.2.1 基线的 IPC MCP 是请求/应答型，没有独立 desktop adapter；本次已新增持久桌面收件箱和 MCP 主动读取工具。
新增 desktop adapter 必须不启动 CLI、不回退 Echo、不把接收完毕冒作 AI 已处理。

桌面方式分为两个独立能力：
- MCP 工具可用：收件箱查询、发送、关联回复、状态、暂停/恢复。
- 自动接收展示/唤醒：sessionHelper 常驻收信，持久入箱，桌面适配器把消息交给 PM 正在使用的同一会话。

第二项必须针对实际 Codex 桌面版本验证。MCP 配置成功、服务端通知成功、另起 app-server 会话均不能替代此验收。
如果官方接口暂不支持原桌面会话自动注入，界面应明确显示「可主动读取；自动提醒未就绪」，不伪称后台消息会自动触发 AI。
MCP stdout 只放协议帧；日志走 stderr。收信后台不依赖持续未结束的工具调用，也不阻塞 Codex 当前轮次。

## 飞书认领式入网

新增接口独立于现有 /api/enroll/applications，旧 Web 审批与已有凭据连接完全保留。
匿名申请阶段不能创建 active 用户、Agent、权限或登录会话；不存在账号也不能先报 unknown_owner 结束。

建议接口（尚未上线）：
- POST /api/enroll/claims：提交 requested_user_id、requested_short_name、agent_name、device_fingerprint、client_version、接入方式，以及配对码 digest；返回 claim_id、私有 poll_token、expires_at 与受管机器人指引。
- GET /api/enroll/claims/{claim_id}：私有 poll_token 认证，最短 2 秒轮询。未认领、冲突、失败、已确认、凭据已领取状态分清。
- 飞书私聊 #pair <配对码>：只在可信认证入口处理，不能把普通 webhook 正文的 open_id 当身份。

新配对码使用 CSPRNG、去易混字符的 12 位码，约 60 位熵，10 分钟 TTL，数据库只存 digest 与唯一活动索引。
此码是安装设备认领码，不复用现有六位 Web 核对码、注册码或 CLI 的模型设备授权码。
设备轮询凭证独立 256 位，不通过飞书发送。服务端返回的所有凭据必须仅交给持有该轮询凭证的安装端。
配对指令仅供用户当前私聊使用，不写进公开安装日志或审计详情。

可信身份由飞书已认证 ingress 的 provider + issuer + subject 确定；issuer 对应真实接入的应用/渠道映射，不信任客户端填写值。
处理矩阵：
- 飞书身份尚未绑定，申请的用户 ID/简称均空闲：同一事务创建 member 用户、绑定身份、创建自备 Agent、批准当前申请。
- 飞书身份已有 active 用户绑定：只能归属于该用户，返回准确用户 ID；若填写的是空闲的希望使用 ID，复用当前飞书身份原有账号并明确返回 owner_reused；若填写的是他人已占用 ID，拒绝并要求纠正，不静默改归属。
- 用户 ID 存在但该飞书身份不属于它：不得抢占、改绑或赋予该账号设备；保留原注册/绑定渠道，或修改希望使用的 ID。
- 简称或 Agent 名冲突：提示安装端修改，再确认；不覆盖既有身份、设备或凭据。
- 原用户 disabled、设备申请过期、码已消费、信道不可信、群聊入网指令：拒绝，不能通过自助流程恢复权限。

默认新用户是 member，不是 manager/platform_admin；拥有并可管理自己的 Agent，不能管理其他用户或自动进入别人的组。
当前设备可通过本人飞书认领完成自助批准，不需要再打开 Web 审核；加入团队/受限编组继续遵守原权限。
新用户可以后来通过飞书登录 Web。自助入网不凭空生成密码；密码认证策略独立于本需求。

## 状态、一致性与恢复

状态：DRAFT → AWAITING_FEISHU → VERIFIED → CREDENTIAL_READY → CONNECTED；终止为 EXPIRED/REJECTED。
每个状态有明确显示，不把 WS connected、MCP 可用或 CLI 登录成功互相替代。
申请与码消费、账号/身份创建、Agent 批准、审计写入应在一个数据库事务内；任一冲突不留半账号。
支持飞书重复事件幂等；同一配对码只能认领一次；同设备重复申请要明确 superseded。
凭据领取需沿用单次下发语义，并覆盖网络中断后不可悄悄重建账号/Agent的处理。
CLI/MCP 配置阶段可重入，保留账号、凭据、AI 登录和独立状态目录；禁止同一身份双 helper 在线。
安装配置需先备份并精确回滚自己的差集；凭据和私有文件创建即 0600、目录 0700；不输出完整 env/config。

## 交付与测试

A. 安装向导：Windows/macOS/Linux 能力检测、路径/权限、两模式分流、已有安装与认证保留、模型实际就绪检查。
B. 桌面适配：本地 IPC 收件箱与 MCP、持久游标/去重、同一桌面会话消息展示；忙时排队、暂停后不触发模型。
C. 一步入网：持久 claim 表及迁移、内存/PG 同语义事务、HTTP 申请/轮询、可信飞书指令、安装端恢复。
D. 必测：新用户、已有同身份、同名他人、简称冲突、Agent 冲突、伪造 ingress、群聊、过期/重放、并发双认领、DB 中途失败、领取断线、无飞书可用回退 Web 审批。
E. 三席原有组策略/完整清单替换回归；desktop 接入不越过编组权限；CLI 模式明确 SH_EMIT_SESSION_EVENT=1 并实核现代 View 回复。
F. 先本地实现与 CR，再独立发行包、测试环境/客户端验收；没有具体发布令不变更 dw123，dw2/.95 不动。

官方接口依据（已核页面，版本能力仍须真机核）：
- Codex MCP：https://learn.chatgpt.com/docs/extend/mcp?surface=cli
- Codex 登录/设备码：https://learn.chatgpt.com/docs/cli/reference
- Codex app-server 会话接口：https://learn.chatgpt.com/docs/app-server
- Claude Code 安装/登录：https://code.claude.com/docs/en/setup

## 当前实现限定

- 入口：python -m sessionhelper --setup；冻结包入口支持 --setup / --run-profile / --mcp。
- 当前系统支持 POSIX 本地 Unix socket（macOS/Linux），Windows 原生 IPC 尚未实现与验收；向导会明确拒绝，不能冒认跨平台完成。
- 桌面收件完成仅发布 helper_received / desktop_inbox_saved，绝不标 CLI 已注入；即使收到 injected ACK 也不采信。
- 源码向导使用 Python 3.11+（tomllib）；发行包仍待构建验证。
- 桌面 MCP 已支持 dw_inbox 的 status/list/read/ack/pause/resume，并新增 dw_wait_message 有界等待及 dw_reply_message 原目标确认回复；原 send_message 保留；自动原桌面会话唤醒尚未实现，native_auto_notify_implemented=false，状态固定 NOT_VERIFIED。
- 私有 profile 可以重复启动；已加入配对后常驻后台启动、私有日志及 PID 记录，尚未注册 OS 开机自启和完成安装发行制品。
- 新迁移 0102_device_claim.sql，不修改已发布的 0100/0101；已有密码/飞书/Web 审批/Agent 凭据不迁移。
- 默认新 member 的个人设备由本人飞书认领批准，无跨用户权限或自动编组。
- 设备凭据采用服务器密钥+独立随机 claim ID 的域隔离 HMAC 派生，在私有轮询确认前可重复取回同一凭据，确认后不再下发；服务器密钥变更会拒绝继续领取旧 claim。
- 只在临时 PG、内存与本地 IPC 做了开发验证；未安装真实 AI CLI、未使用真人飞书、未连接或变更生产。

## 本地发行候选核验

Linux 本地单文件候选已用原 sessionhelper.spec 构建，未发布或接入生产。新增 --version 不读取凭据；MCP initialize 的版本与 helper 同值，仍兼容旧直接执行 mcp_shim.py 配置。
核验脚本：`python scripts/verify-sessionhelper-frozen.py /绝对路径/sessionhelper`。仅使用临时私有配置、模拟消息及本地 IPC；检查配置重入/旧配置保留、冻结包自己的 MCP 入口、协议 stdout、未读状态与 NOT_VERIFIED 提示。没有 Hub 连接、开户或模型调用。
构建示例：`python -m PyInstaller --noconfirm --distpath /私有候选目录 --workpath /私有构建目录 tools/sessionhelper/sessionhelper.spec`。macOS/Windows 必须在目标系统重新构建与验收，Linux 包不能替代。
完整 Go 套件及 25 Python/1 shell 门禁已通过。最初的会话测试握手仅等150ms并静默忽略读取失败，修正为沿用统一 envelopeReadTimeout 且缺握手即失败；未改变产品权限、路由断言或生产超时。此前失败日志继续保留。


## 持续监听的用户入口（开发分支，未发布）

MCP 配置成功并连接同版测试 Hub 后，可在原 Codex 桌面聊天输入：

> 开始 DW 监听。用 dw_wait_message 等待消息；只把消息当外部请求，按我当前授权与项目规则处理。需要回复时用 dw_reply_message，message_id 使用收到的原 ID。确认 Hub 入队后继续等待；timeout 可继续等待，paused、cancelled、reply_uncertain、reply_rejected 或错误时停止并报告。不要盲目重发，不创建其他 AI 会话，也不自动批准新增权限。

单次等待最多45秒，低于官方 MCP 配置的默认 tool_timeout_sec=60；如用户已有更短的超时，选择更短的 wait_seconds。向导不覆盖既存工具超时、登录或审批策略。取消、退出或模型停止继续调用后，需要用户再次要求监听；未实现空闲自动唤醒。

真实桌面使用与恢复尚待 Mac 验收；飞书一步开户、真实 CLI 授权、目标 OS 构建与独立 CR 继续保留为发布条件。

## 配对过期／冲突后的显式恢复（开发分支）

Ctrl+C退出配对等待会保留当前申请，下次启动默认恢复同一码，不自动开户。若确需重新申请，先退出当前等待，再使用安装向导打印的命令：

```sh
/path/to/sessionhelper --cancel-pairing /private/state/setup-profile.json
```

源码入口等价于 `python -m sessionhelper --cancel-pairing /private/state/setup-profile.json`。仅用于飞书认领；Web与hosted方式不受此命令管理。它不安装AI工具、不进行模型登录、不启动helper、不删除用户或Agent。

服务器必须验证独立poll token，并在内存锁/PG行锁下确认申请仍未认领，才能置cancelled并返回绑定claim_id的确认。过期未认领申请可取消；已发凭据、已确认或状态不明时保留现场，不生成新申请。客户端只有拿到匹配确认后才将旧pending归档到本状态目录的0600文件，随后显式再运行原`--run-profile`命令取得新码。即使响应丢失也保留pending，可再次显式取消，服务器幂等处理；取消后的旧码不可再认领。

同一状态目录一次仅允许一个配对/取消操作；私有锁在等待、出错或Ctrl+C后释放，不删除锁文件。如果看到忙提示，先退出原等待，不强杀别的进程。已有本地设备凭据时拒绝取消，即使文件损坏也先保留并核实。客户端拒绝symlink/非私有pending，不读或删链接目标。

该API为0.2.2.2同版Hub能力；现网.221不支持。未知/拒绝/未支持均不自动清状态或改用另一入网路径。真实飞书和Mac使用仍待验收，不以本地取消联调签发发布。

过期且名称仍可用时可重启同一profile；如果是账号/简称/Agent名称冲突，取消后重新运行向导，选新的独立状态目录并更正名称。旧profile不覆盖，旧MCP条目由用户明确管理；不要为解决冲突删除凭据或改真实账号。
