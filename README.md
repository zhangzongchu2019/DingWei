# DingWei 飞书与Agent网络协同工作平台

一个**通用**的飞书协同平台：统一管理团队的**工作排期、进度提交、动态调整**，从成员与 AI 助手的对话中**佐证进度**；同时是一个**人 + Agent 网络混合的消息路由与协作中枢**——不仅多机器人管道收发，更让**接入的多个 AI CLI 会话（Codex / DeepSeek / Aider 等）构成一张 Agent 网络：互相寻址、路由消息、协同工作**。

> 完整设计见 [`docs/产品规范.md`](docs/产品规范.md)。本仓为 **Go 实现 + Docker 交付**的工程脚手架。

## 名字由来

**DingWei** 取自北宋名臣 **丁谓（丁晋公）**。宋真宗时皇宫失火需重建，丁谓一策解三难——先在宫前大街掘沟、取土就地烧砖，再引汴水入沟、以舟船运送建材，工毕又以废砖瓦填沟复街；一举而"取土、运料、清废"三事俱办。这种**以一套系统性方案整合多个环节、一举多得**的智慧，正是本平台的内核：把**消息总线、排期进度、AI 会话协同**收拢进**一套统一的寻址与路由**中。

同时 **DingWei 谐音「定位」**——平台的核心，正是为团队成员与各 AI Agent **定位、寻址、路由消息**，让"人 + Agent 网络"各就其位、协同工作。

## 定位与规模
- 小团队应用：**≤ 100 人**，消息低频，**峰值写入 ≤ 200 次/秒** → SQLite（WAL）足够，不需 Redis/PG。
- 全部**配置 + 消息**入 **SQLite**；后台（M9）改配置 → 服务**热更新**；消息按月归档。

## 模块（M0–M9，全量不分期）
- **M0** 消息总线：多机器人管道收发 + 按会话主体（个人/群）分收/发队列（持久化/幂等/重试/死信）
- **M1–M3** 排期管理 / 进度提交 / 动态调整（diff+确认、顺延级联、管理者代改→确认）
- **M4** AI 对话佐证（抽取→映射→对账，默认 opt-out 全开 + 五态；human-in-loop 人工关联）
- **M5** 查询与门户 · **M6** 风险 · **M7** 通知提醒（含影响变更通知所有当事人）
- **M8** 服务注册转发：字头路由（通配符/多字头）+ WS 长连接（至多一次）+ API key 绑定账号 + 唯一性/覆盖检测
- **M9** 后台管理 Web：运行监控 / 机器人配置 / API key 管理 / 字头列表 / 最近 100 条 / 按时间清理；**必须密码登录**

## AI CLI 协同（跨会话消息路由与协作）
DingWei 不只连接飞书真人，还让**多个 AI CLI 会话接入同一平台互相协作**：
- **接入**：各 AI CLI（Codex / DeepSeek / Aider / OpenCode 等）经 **SessionHelper** 以「会话」身份接入 DingWei，携带工具名 / 模型名注册。
- **跨会话寻址与消息路由**：会话之间可直接寻址——同账号下 `#会话名`（如 `#developer`），跨成员 `@成员 #会话名`；一个 AI 可把任务 / 问题**路由**给另一个 AI，实现多智能体分工（如 manager 统筹、developer 实现、tester 验证）。
- **在线会话清单**：成员上下线时，按账号广播当前在线 AI 会话清单（工具 / 模型 / 寻址方式），供彼此发现与协作寻址（并每 4 小时刷新）。
- **人机混合协作**：飞书真人可 `@` 唤起 AI 会话下达任务；AI 会话的产出可镜像回飞书。**广播消息按来源去重**——同一条广播发给 N 个会话时，飞书上只镜像一条，避免刷屏。
- **系统任务通道**：虚拟成员承载「只发群」的系统生产者任务（如安全告警、代码分析），与人类协作隔离。

> 底层依托 **M8 服务注册转发**（字头路由 + WS 长连接 + API key 绑定账号）：AI CLI 即「注册方会话」，跨会话寻址与消息路由即在此之上实现。

## 技术栈（Go）
- 语言 **Go**；SQLite = **`modernc.org/sqlite`（纯 Go 无 cgo）**
- 并发 goroutine + channel（每会话主体一队列 + 单写者）
- 飞书官方 SDK `larksuite/oapi-sdk-go`；WS `coder/websocket`；HTTP stdlib；调度 `robfig/cron`
- 正则 Go RE2（线性时间，天然无 ReDoS）

## 目录
```
cmd/workpulse        入口（装配 + HTTP）
internal/model       领域类型
internal/clock       可注入时钟
internal/config      bootstrap(env) + 配置快照(热更新)
internal/store       Repository 抽象 + SQLite 实现 + 内嵌迁移
internal/bus         M0 消息总线/队列抽象
internal/feishu      飞书网关抽象（接口 + stub；待接 oapi-sdk-go）
internal/llm         LLM Provider 抽象 + 双 provider 故障转移
internal/router      三级路由 + M8 字头匹配/重叠检测
internal/admin       M9 后台（登录/会话/状态）
internal/store/migrations  SQL schema（内嵌迁移）
```

## 快速开始
### 本地
```bash
cp .env.example .env          # 至少设 WP_ADMIN_INIT_PASSWORD
go mod tidy
make build && ./bin/workpulse # 默认 :8080；/healthz、/admin/login
```
### Docker（交付形态）
```bash
cp .env.example .env          # 设 WP_ADMIN_INIT_PASSWORD 等
docker compose up -d --build  # 数据落持久卷 workpulse-data:/data
```

## 配置与数据
- **bootstrap 走环境变量**（见 `.env.example`）：`WP_DB_PATH / WP_DATA_DIR / WP_ADDR / WP_ADMIN_USER / WP_ADMIN_INIT_PASSWORD`。
- **其余配置经 M9 后台写入 SQLite**；可配项结构见 `config/app.example.yaml`（仅占位，无真值）。
- 数据目录 `/data`：SQLite 主库 + 月度归档库（持久卷，重启不丢）。

## 排期数据组织与联动
- 排期 = **每个成员的个人排期 + 一份全局日程管理文件（位置可配置）**；SQLite 为事实源，按配置同步导出到全局文件，供门户/人读/既有流程衔接。
- 任何**排期/进度的提交或修改**自动触发个人排期更新 + 协调全局日程；产生**明显影响**时向**所有当事人**发通知（详见规范 §M3/§M7）。

## 安全
- 敏感信息（个人姓名 / 企业名 / 飞书凭证 / LLM key / 管理员密码 / DB / 留存数据）**一律不入 git**（见 `.gitignore`）。
- 后台必须密码登录（哈希存库）；注册方 API key 绑定账号、账号范围 ⊆ 绑定集合。

## 状态
工程脚手架（接口/分层/SQLite/迁移/后台登录/Docker 就绪）；业务与平台细节按 `docs/产品规范.md` 逐模块实现。
