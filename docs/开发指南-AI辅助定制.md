# DingWei 开发指南（面向 AI 辅助定制开发）

> 目标：让你（或你的 AI 助手 claude/codex）快速吃透 DingWei 代码，做二次开发/定制。
> **用法建议**：把本文 + 相关源码目录一起丢给 AI，说清你要加的功能，让它照"扩展点"和"约定"改。

---

## 1. DingWei 是什么
**团队协作平台**，四大能力：
1. **团队内部消息总线** —— 飞书用户、AI 会话、系统服务之间统一寻址路由；
2. **成员间 AI 协调通讯** —— `@符坚#fulei` 跨成员找别人的 AI 会话协作；
3. **工作记录感知** —— 采集成员 AI 会话，对账日程执行（佐证）；
4. **日程更新** —— deepseek 调度器综合协调排期 + 定时佐证通报。

技术栈：**Go 后端**（单二进制 `bin/dingwei`）+ **Python sessionHelper 客户端**（成员端接入 AI CLI）+ **SQLite** + **飞书长连接**。

---

## 2. 架构总览
```
飞书用户 ──长连接──┐                              ┌── 成员的 AI CLI(claude/codex)
                   ↓                              │        ↑ tmux 注入/读transcript
              ┌─────────────────────────┐        │
   Web后台 ── │   DingWei 后端(Go)        │ ──WS── sessionHelper(Python客户端)
  /dingwei/   │  m8 Hub: 寻址/信封/路由    │        (成员各自机器,一条命令 ./start.sh)
              │  worker/router/schedule   │
              │  scheduler(deepseek调度)  │
              │  store(SQLite) admin后台   │
              └─────────────────────────┘
                   ↑ nginx 反代 /dingwei/ + /dingwei/ws/
```
- **后端**：收飞书消息 + 提供 WS 端点给 sessionHelper + Web 后台 + 系统调度器。
- **sessionHelper**：跑在成员机器上，把飞书消息注入成员的 AI CLI、读回答、采集会话。

---

## 3. 目录/包地图（改哪里找哪里）
### 入口
- `cmd/workpulse/main.go` —— **主入口**：装配所有组件、起飞书网关、WS、后台、cron。改启动逻辑/接线看这。

### internal/（后端核心）
| 包 | 职责 | 常改场景 |
|---|---|---|
| **`m8`** | **核心 Hub**：寻址/信封/路由/分发（`hub.go`）。`dispatchSystem`(系统关键词)→`dispatchMirrorCommand`→`dispatchSelector`(#会话名/@成员)→回落；`RouteEnvelope`投递；`HandleSessionWS`(WS接入);跨成员寻址 | 加寻址语法/路由规则 |
| **`worker`** | 消息 worker（`schedule_worker.go`）：入站消息处理，先 `Prefix.Dispatch` 未命中再 `router.Decide` | 改入站处理流程 |
| **`router`** | 三级路由判定（`router.go` `Decide`）：结构化指令/唤起符号→LLM/静默 | 改判定规则 |
| **`schedule`** | 排期指令解析（`parse.go`：`+/-/改/顺延/全量`）+ 处理（`service.go`：diff/确认） | 改排期指令 |
| **`scheduler`** | **R8 系统级 deepseek 调度器**（`scheduler.go`）：`Coordinate`(协调排期)/`RunEvidence`(佐证)/`Notify`(群通知)/cron | 改调度/佐证逻辑 |
| **`admin`** | **M9 Web 后台**（`admin.go`）：登录 + 各页 CRUD（成员/租户/会话/机器人管道/运行配置/门户/成员会话目录） | 加后台页/字段 |
| **`store`** | **SQLite 存储**（`sqlite.go` + `store.go` 接口 + `migrations/`）：所有 DB 操作 | 加表/查询 |
| **`feishu`** | 飞书 SDK 封装：`LarkGateway`(长连接/发消息/tenant_access_token/群成员/@渲染) | 改飞书交互 |
| **`bus`** | 出站消息队列（enqueue/claim/done） | 改投递队列 |
| **`model`** | 数据类型：`Message`/`Envelope`/`Member`/`Direction`(in/out/collect)/`SessionEndpoint` 等 | 加字段/类型 |
| **`secretbox`** | AES-GCM 加解密（`WP_SECRET_KEY` 派生 key，加密 bot secret 入库） | 密钥/加密 |
| **`config`** | bootstrap 配置（env→`Config`，`config.go`） | 加启动配置 |
| **`redact`** | 敏感信息脱敏（日志/输出去密钥/手机号） | 脱敏规则 |
| `evidence`/`coordination`/`reminder`/`llm`/`clock` | 佐证/协调/提醒/LLM客户端/时钟抽象 | 按需 |

### tools/sessionhelper/（成员端 Python 客户端）
| 文件 | 职责 |
|---|---|
| `app.py` | 主循环：连 WS、收信封、分发到 mode、回复/镜像/采集 |
| `cli.py` | **Mode B（托管 AI CLI）**：tmux 起 claude/codex、send-keys 注入、读 transcript、注入重试 |
| `llm.py` | **Mode A（直连 LLM）**：deepseek/qwen/kimi/glm/openai/claude/gemini providers |
| `config.py` | 全部 `SH_*` 环境变量 → Config |
| `protocol.py` | To/From 信封协议 |
- `run.sh` —— 一条命令 bootstrap（检测依赖/venv/装依赖/读config/起）。

---

## 4. 核心概念 & 数据模型
- **租户(tenant/service)** = 一个人/空间，有 `key_id`（如 `FB-fulei-…`，公开，进地址）。表 `registered_service` + `service_api_key`(key_hash + 绑定飞书账号 `api_key_account`)。
- **会话(session)** = sessionHelper 连接，表 `session_endpoint`(key_id + 会话名 + 在线 + client_ip + 镜像)。
- **成员(member)** = `member`(owner_key/display_name/feishu_open_id/role/evidence_optout)。链：`member.open_id → api_key_account → key_id → session`。
- **寻址**：
  - `#会话名`（本租户）· `@成员` / `@成员#会话名`（跨成员）· `sys:调度`/`#四字`（系统关键词→scheduler）· 飞书地址 `open_id#key_id#bot_name`。
- **信封 Envelope**：`To/From` + body + meta（`type=collect`静默采集 / `mirror` / group_chat_id / at）。
- 表清单见 `internal/store/migrations/`（0001~0007）。

---

## 5. 关键数据流（读代码顺着走）
**A. 飞书消息进来** → `feishu.LarkGateway` 收 → `worker.schedule_worker` → `m8.Hub.Dispatch`：
1. `dispatchSystem`：头部是 `sys:调度`/`#四字` → 系统服务(scheduler)；
2. `dispatchMirrorCommand`：`mirror on/off`；
3. `dispatchSelector`：`#会话名`(本租户) / `@成员#会话名`(跨成员) → `RouteEnvelope` 投递到会话；
4. 都不中 → 静默（排期已改为只由系统关键词触发）。

**B. sessionHelper 接入** → WS `/ws/session/{会话名}?key_id=` + Bearer secret → `m8.HandleSessionWS` → 注册 `session_endpoint` → 收信封走 Mode A/B。

**C. 系统调度器** → `sys:调度 <变更>` 或 cron(0/6 UTC) → `scheduler.Coordinate`(读排期文件→deepseek→写新版+备份) / `RunEvidence`(读排期+会话采集→佐证报告→群通知)。

---

## 6. 扩展点（怎么加功能）
- **加一个寻址语法/路由**：`m8/hub.go` 的 `parseSelector` + `dispatchSelector`（照 `@成员` 的写法）。
- **加一个系统关键词→动作**：`system_route` 表加关键词 + `scheduler.HandleSystemRequest` 加分支（照 `record`/`coordinate`）。
- **加后台页/字段**：`admin/admin.go` 加 handler + `store` 加查询 + `migrations` 加表/列（**新加迁移文件 `0008_*.sql`，别改旧迁移**）。
- **加 sessionHelper 能力**（新 CLI/新 provider）：`cli.py` 的 `CLI_PROFILES` / `llm.py` 的 provider。
- **改调度/佐证**：`scheduler/scheduler.go` 的 prompt/流程。
- **约定**：敏感值走 env 不入库明文（secret 存 sha256 或 secretbox 加密）；`WP_`前缀保留；飞书消息纯文字无 markdown/表格；新迁移不改旧文件；改完 `go build/vet/test`。

---

## 7. 构建 / 运行 / 测试
```bash
go build -o bin/dingwei ./cmd/workpulse   # 编译
go test ./...                              # 单测
go vet ./...
sudo systemctl restart dingwei             # 重启服务
bash deploy/smoke.sh                        # 冒烟
```
- 配置：`.env`（`WP_SECRET_KEY` 必填，`openssl rand -hex 32`；见 `.env.example`）。
- 部署：`deploy/install.sh`（一键）；`deploy/` 有 systemd/nginx 模板。
- sessionHelper：`cd tools/sessionhelper && ./run.sh`。

---

## 8. 让 AI 帮你定制开发（推荐姿势）
1. **给 AI 上下文**：把本文 + `docs/产品规范.md` + 你要改的那个包（如 `internal/m8/`）一起给它。
2. **说清意图 + 约束**：例如"在 `m8/hub.go` 加一个 `#广播` 关键词，把消息发给本租户所有在线会话，回复各自带会话名头；照 `dispatchSelector` 的写法；新迁移别改旧的；改完能 `go build`"。
3. **让它先读后改**：要求它先定位相关函数（`dispatchSelector`/`RouteEnvelope`/`ListSessionEndpoints`）再动手。
4. **验证闭环**：让它 `go build/vet/test` + 给一条冒烟命令（如 `curl POST /webhook/unifiedrobot` 造消息）自测。
5. **小步提交**：一个功能一个 commit，message 说清。

> 参考交办文档 `docs/交办-*.md`（R1-R13）——里面有每个功能的设计+验收，是最好的"怎么改"范例。

---

## 9. 常见坑
- **飞书 app 长连接单实例**：同一 app 多实例会抢事件。
- **Mode B TUI（claude/codex）用 tmux**，不用裸 PTY（注入丢键）；Windows 无原生 tmux 本期不支持。
- **首条冷启动**：claude 懒启动 + 慢，注入已自动重试、`SH_CLI_REPLY_WAIT` 放宽。
- **改 env 值带空格**（如 `WP_SCHEDULER_CLI="claude -p"`）：systemd 能读，但 bash `source .env` 会崩——脚本别 source 整个 .env。
- **跨成员寻址**是租户外的，`#会话名` 只在本租户内。
