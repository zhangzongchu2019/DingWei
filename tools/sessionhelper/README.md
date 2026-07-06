# sessionHelper

`sessionHelper` 是 DingWei / feishu-bot-bridge 的会话侧适配器。它连接
`/ws/session/{SH_SESSION_NAME}?key_id={SH_KEY_ID}`，使用
`Authorization: Bearer ${SH_SECRET}` 鉴权，并按统一信封 `{id,to,from,body,ts,meta}` 双向收发。

敏感值只从环境变量读取，不写入配置文件或仓库。

## 一条命令启动（Linux / Mac）

本期推荐用源码 bootstrap，不预打二进制：

```bash
cd tools/sessionhelper
./run.sh
```

首次运行会交互引导一次，之后复用本地配置并自动启动：

- 检测 `python3 >= 3.9`、`tmux`、所选 AI CLI（如 `claude` / `codex`）。
- 在工具目录创建/复用 `.venv`，执行 `pip install -r requirements.txt`，不污染全局 Python。
- 把配置写入 `~/.workpulse-sh/config`，权限强制 `600`；`SH_SECRET` 不回显、不打印、不写入仓库。
- 导出 `SH_MODE=cli`、`SH_CLI_LAUNCH=tmux`、`SH_COLLECT=1`、`SH_CLI_READY_TIMEOUT=90`、`SH_CLI_REPLY_WAIT=45` 等环境变量后运行 `python -m sessionhelper`。
- 默认会静默采集本地 AI CLI 会话轮次到 DingWei，用于团队进度佐证；不会发到飞书。需关闭请联系管理员设置成员 `evidence_optout`，或本地把 `SH_COLLECT=0`。
- sessionHelper 断线会自动重连；退出进程时会清理它创建的 tmux session。

重新配置：

```bash
./run.sh --reconfigure
```

缺依赖时按平台提示安装：

```bash
# macOS
brew install python tmux

# Debian/Ubuntu
sudo apt-get install python3 python3-venv python3-pip tmux
```

AI CLI 需要用户自行安装并登录，本工具只托管已可用的 CLI，不代装、不保存模型账号。

本地配置项：

| 配置 | 说明 |
|---|---|
| `SH_SESSION_NAME` | 会话名，对应飞书 `#会话名` |
| `SH_KEY_ID` | DingWei 公开租户标识，进入地址 |
| `SH_SECRET` | DingWei 私密连接 secret，仅用于 `Authorization: Bearer` |
| `SH_CLI` | AI CLI profile，如 `claude` / `codex` |
| `SH_CLI_CMD` | 可选，自定义 CLI 启动命令 |
| `SH_CLI_CWD` | CLI 工作目录 |
| `SH_OPENCODE_DB` | 可选，opencode SQLite 会话库路径，默认 `~/.local/share/opencode/opencode.db` |
| `SH_CLI_READY_TIMEOUT` | CLI 冷启动等待时间，启动器默认 90 秒 |
| `SH_CLI_REPLY_WAIT` | CLI 首答等待时间，启动器默认 45 秒 |
| `SH_WS_BASE` | DingWei WebSocket base URL |
| `SH_COLLECT` | 是否静默采集 transcript 供佐证使用，默认 `1` |
| `SH_ASYNC_REPLY` | CLI 模式可选，`1` 表示只注入指令，不等待模型输出、不发同步回执 |
| `SH_MIRROR_TO` | 可选，默认镜像目标地址 |
| `SH_TARGET_GROUP` | 可选，producer 输出目标飞书群 chat_id；普通会话留空不会向群发 |
| `SH_TARGET_BOT` | 可选，producer 发群时强制使用的 bot_channel；留空则由 DingWei 按群归属解析 |
| `SH_PRODUCER` | 可选，`1` 表示非交互 producer，只从 stdin 推内容到 `SH_TARGET_GROUP` |

## 构建

```bash
python3 -m pip install -r tools/sessionhelper/requirements.txt
make sessionhelper
./dist/sessionhelper
```

产物为单文件二进制：`dist/sessionhelper`。

## 通用环境变量

```bash
export SH_SESSION_NAME=home
export SH_KEY_ID=FB-xxx-20260701-abcd
export SH_SECRET='只在连接鉴权使用的 secret'
export SH_WS_BASE=ws://127.0.0.1:8080
export SH_BOT_NAME=UnifiedRobot
```

可选：

```bash
export SH_OUTBOX=/tmp/sessionhelper.outbox
export SH_RECONNECT_MIN=1
export SH_RECONNECT_MAX=30
```

`SH_OUTBOX` 每行格式为：

```text
目标地址|正文
```

目标地址可以是会话地址 `developer#FB-...`，也可以是飞书地址
`ou_xxx#FB-...#UnifiedRobot` / `oc_xxx#FB-...#UnifiedRobot`。

## Producer 模式：系统任务发群

普通 sessionHelper 不会自动向飞书群发消息。系统任务需要发群时，使用管理员分配给
`SYSTEM-V-TASK-INTERNAL` 的 key，并显式配置目标群：

```bash
export SH_SESSION_NAME=producer
export SH_KEY_ID=FB-system-v-task-internal
export SH_SECRET='管理员单独发放'
export SH_PRODUCER=1
export SH_TARGET_GROUP=oc_xxx
export SH_TARGET_BOT=CC-Connector  # 可选；群归属已在 DingWei 里时可不填
./dist/sessionhelper
```

`SH_PRODUCER=1` 时不启动交互 CLI，进程从 stdin 按行读取内容并经 DingWei 发到
`SH_TARGET_GROUP`。发送信封带 `no_mirror`，不会再进入会话镜像链路。

## 模式 A：直连模型 API

```bash
export SH_MODE=llm
export SH_PROVIDER=deepseek
export SH_API_KEY='sk-...'
export SH_MODEL=deepseek-chat
./dist/sessionhelper
```

支持 provider：

| provider | 协议 | 默认 base_url | 默认模型 |
|---|---|---|---|
| `deepseek` | OpenAI compatible | `https://api.deepseek.com/v1` | `deepseek-chat` |
| `qwen` | OpenAI compatible | `https://dashscope.aliyuncs.com/compatible-mode/v1` | `qwen-plus` |
| `kimi` | OpenAI compatible | `https://api.moonshot.cn/v1` | `moonshot-v1-8k` |
| `minimax` | OpenAI compatible | `https://api.minimax.chat/v1` | `MiniMax-Text-01` |
| `glm` | OpenAI compatible | `https://open.bigmodel.cn/api/paas/v4` | `glm-4-flash` |
| `openai` | OpenAI | `https://api.openai.com/v1` | `gpt-4o-mini` |
| `claude` | Anthropic Messages | `https://api.anthropic.com` | `claude-3-5-sonnet-latest` |
| `gemini` | Google Generative Language | `https://generativelanguage.googleapis.com/v1beta` | `gemini-1.5-flash` |

可用 `SH_BASE_URL` / `SH_MODEL` 覆盖默认值。按发起方地址维护简单对话历史，历史长度由
`SH_HISTORY_TURNS` 控制。

## 模式 B：托管终端 CLI

```bash
export SH_MODE=cli
export SH_CLI=claude
export SH_CLI_CWD=/home/ai-dev/content-center
./dist/sessionhelper
```

支持 CLI profile：

| SH_CLI | 默认命令 |
|---|---|
| `claude` | `claude --dangerously-skip-permissions` |
| `codex` | `codex` |
| `aider` | `aider` |
| `cline` | `cline` |
| `gemini` | `gemini` |

可用 `SH_CLI_CMD` 覆盖启动命令，例如：

```bash
export SH_CLI_CMD='codex --dangerously-bypass-approvals-and-sandbox'
```

sessionHelper 可用 `SH_CLI_LAUNCH=tmux|pty` 选择启动方式。`claude` / `codex` 这类全屏
TUI 默认走 tmux：sessionHelper 创建独立 tmux session，等待界面稳定后用 `tmux send-keys`
注入正文和 Enter，并通过 transcript 中出现对应 user 轮来确认消息确实被 CLI 接收。
`aider` / `cline` / `gemini` 等行式工具默认保留自有 PTY。

可调参数：

```bash
export SH_CLI_SETTLE_SECONDS=1.2
export SH_CLI_USER_ACK_WAIT=10
export SH_CLI_REPLY_WAIT=60
export SH_CLI_LAUNCH=tmux
export SH_CLI_LAUNCH_RETRIES=3
```

`claude` / `codex` / `opencode` 属于全屏 TUI。sessionHelper 对这几类 CLI **只用 tmux/PTY 做注入**，
回复和镜像改从 transcript 读取，避免把启动框、ANSI 重绘、输入回显发回飞书：

- `claude`：自动定位 `~/.claude/projects/<cwd-slug>/*.jsonl`，解析 assistant text。
- `codex`：自动定位 `~/.codex/sessions/**/rollout-*.jsonl`，解析 `event_msg/agent_message`。
- `opencode`：只读打开 `~/.local/share/opencode/opencode.db`（可用 `SH_OPENCODE_DB` 覆盖），注入后先找晚于注入时刻且文本匹配的 user message，以其 `session_id` 定位当前会话，再等同会话下 `role=assistant` 且 `finish=true` 的 message，拼接 `part.data.type=text` 的文本作为回复。

自动定位只接受本次启动后新建/首写的 transcript，避免抓到旧 session 文件。可用
`SH_CLI_TRANSCRIPT=/path/to/session.jsonl` 手工指定 transcript。若 transcript 未出现对应
user 轮，会返回“注入未被 CLI 接收”；若 user 轮已落盘但未出现回答，会返回“答案超时”，
不会返回原始 TUI 画面。

CLI 采用懒启动：sessionHelper 会先保持 WS 在线，收到消息时启动/重试 CLI。若 tmux/Claude
因 MCP/auth/网络等原因慢启动或未就绪，进程不会退出，会向飞书返回“CLI未就绪”，后续消息仍可
继续触发有限次重试。

长耗时命令接收器可设置 `SH_ASYNC_REPLY=1`。此模式下 CLI 收到指令后只做注入和 user 轮落盘确认，
不等待 assistant 输出，也不发送同步回执；回执由 CLI 内的 wrapper/agent 自行异步发送。

## 镜像控制

DingWei 侧通过 DM 指令：

```text
mirror on home
mirror off home
```

下发 `meta.type=mirror_control` 信封。sessionHelper 收到后只改变镜像状态，不把控制消息注入模型。
镜像开启后，CLI 输出会同步到 `meta.mirror_to` 指定的飞书地址；关闭后停止。

## 开发检查

```bash
python3 -m compileall -q tools/sessionhelper/sessionhelper tools/sessionhelper/sessionhelper.py
python3 -m unittest discover -s tools/sessionhelper/tests
```
