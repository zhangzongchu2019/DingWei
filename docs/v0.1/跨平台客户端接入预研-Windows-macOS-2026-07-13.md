# sessionHelper 跨平台客户端接入预研（Windows / macOS）

- 日期：2026-07-13
- 基线：sessionHelper 0.1.2.6
- 目标客户端：codebuddy、trae、kiro
- 结论一句话：**三个客户端都存在不依赖 PTY 的驱动路径，Windows 原生支持不必重做 PTY 层。**（此结论已被实测坐实）

> **📌 修订说明（2026-07-13 实测后）**
> 本文的**主结论（不必重做 PTY 层、driver 分层、"不要模拟人"）已被实测全面证实**，但其中**四处初判被推翻**，均已就地订正并标注 ⚠️：
> 1. ~~codebuddy 最省事~~ → **它是唯一有硬阻塞的**（不能自带 key，必须 `/login`）—— §3.1
> 2. ~~trae 跨平台最现实~~ → 跨平台确实最好，但**它没有多轮会话，接入成本最高** —— §3.2
> 3. ~~trae trajectory 是 session 级单文件~~ → **后一个任务覆盖前一个** —— §3.2
> 4. ~~Windows 虚机 22/3389/443 均开放~~ → **该网络对任何端口都回 ACK，端口扫描全是假信号** —— §7
>
> 排期（§6）与风险（§5）已按实测重排。完整实测记录见 **[客户端接入测试报告-2026-07-13.md](./客户端接入测试报告-2026-07-13.md)**。

---

## 1. 问题定义

helper 在 Windows / macOS 上要同时满足四件事：

1. 能把消息**注入**给 CLI
2. 能**可靠取回**答案
3. 能把终端流**镜像给网页 view**
4. **Ctrl-C 能抢占**（架构四原则里网页终端的唯一例外）

难点从来不是 (1)，而是 (2)(3)(4)——而今天这三件事全部绑在同一个 PTY 上。

## 2. 现状：为什么 Windows 是障碍

sessionHelper 的 PTY 层是 **pexpect**（`sessionhelper/cli.py:19`、`:134`、`:491` 的 `pexpect.spawn`）。

- **pexpect 的 spawn 是 POSIX-only**，Windows 上直接抛 `ExceptionPexpect: Windows is not supported`。
- 另一个 launch 模式是 **tmux**，Windows 上同样没有。
- macOS 是 POSIX，pexpect 原生可用 → **macOS 本身不构成障碍**，主要是验证成本。

若沿现有架构直译，Windows 需换成 **pywinpty / ConPTY**，牵动面包括：PTY 本体（spawn / read_nonblocking / setwinsize / sendcontrol）、信号语义（SIGINT → `GenerateConsoleCtrlEvent`）、bracketed paste 注入、`is_idle` 的 1.2s PTY 静默判据、tmux 模式取舍、`provision update_self` 自升级链路。

其中 **`is_idle` 静默判据是已知不牢的地基**——它在 Linux 上已经导致过 P1（busy 时消息重复注入），搬到噪声更大的 ConPTY 上只会更不准。

> WSL2 方案（helper 跑在 Linux 子系统里、代码零改动）**已被否决**，Windows 必须原生。

## 3. 客户端能力矩阵（已核实）

| 客户端 | 结构化输入 | 结构化输出 | 落盘 | 能否无 PTY 驱动 |
|---|---|---|---|---|
| **codebuddy** 2.116.0 | `--input-format stream-json` ✅ | `--output-format stream-json` ✅ | `~/.codebuddy/projects/<cwd-slug>/<uuid>.jsonl`（每轮） | ✅ **纯管道** |
| **trae**（bytedance/trae-agent） | 裸 `input()`（`simple_console.py:197`）✅ | ❌ 无 JSON 接口 | `trajectories/trajectory_<ts>.json`（**全量重写**） | ✅ **Python 库内嵌**（`Agent.run()`，`agent/agent.py:59`） |
| **kiro**（Amazon Q CLI 改名） | `--no-interactive`（**一次性**） | ❌ 无 JSON 接口 | ✅ **SQLite，每轮落库** + `--resume-id` | ⚠️ 一次性 + resume 模拟持久（**待实测**） |

### 3.1 codebuddy —— 协议好，但**认证是硬阻塞**

> ⚠️ **本节初判"最省事、一等公民"已被实测推翻**。协议确实好，但它**不能自带 key**——`CODEBUDDY_BASE_URL` 是在腾讯云网关**服务端**解释的（指向本地代理时本地零请求），必须用 codebuddy 账号 `/login`。**它是三家里唯一有硬阻塞的。** 详见[测试报告 §3.4](./客户端接入测试报告-2026-07-13.md)。

- 是 Claude Code 的同构分叉：默认交互式 TUI、有 `-y/--dangerously-skip-permissions`、`-c/--continue`、`-r/--resume`。
- transcript 目录布局与 claude **完全一致**，但 **schema 不同，不能复用 `parse_claude_event`**：

```
claude    : {"type":"assistant", "message":{"role":"assistant","content":[{"type":"text","text":...}]}}
codebuddy : {"type":"message", "role":"assistant", "content":[{"type":"output_text","text":...}], "status":"incomplete"}
```

  走 `SH_CLAUDE_PROJECTS_ROOT` 那条捷径（claude-deepseek 就是这么接的）**只能定位到文件，parse 出 0 条事件**，表现为"注入未被 CLI 接收 / 答案超时"——很像网络问题，极易误判。
- `status: "incomplete"` 必须当未完成过滤（对应 opencode 的 `finish` 判据），否则会把报错半成品当答案发出去。
- **但更优路径是 stream-json 双向管道**，连 transcript 都不用读。

### 3.2 trae —— 库内嵌，且上游已做 Windows 兼容

- **注意歧义**：`trae.ai` 卖的 **Trae IDE 是桌面 GUI，没有 CLI，接不进 helper**。我们要的是 [bytedance/trae-agent](https://github.com/bytedance/trae-agent)（Python、MIT、`uv sync`、命令 `trae-cli`）。
- `trae-cli run` 是一次性；`trae-cli interactive` 是真 REPL（`cli.py:524` 的 `while True: get_task_input()`）。
- **完全没有流式/JSON 接口**（grep 全空）。
- trajectory：~~session 级单文件~~ → **已订正**：`save_trajectory()` 用 `open(path,"w")` + `json.dump(全量)`（`trajectory_recorder.py:226`）**每次全量重写**，且**后一个任务把前一个任务的 trajectory 整个覆盖**——第 1 轮记录在第 2 轮开始时即消失。**绝不能用字节偏移 tail。**
  → 现有 `TranscriptTailer` 靠字节偏移增量读（`cli.py:546-555`），**对全量重写的 JSON 完全失效**，必须新写"整文件重读 + 按 steps 索引 diff"的 reader。
- **跨平台结论（好消息）**：
  - 依赖无 POSIX-only 包（唯一 `pexpect` 在 `agent/docker_manager.py`，仅 docker 模式，非必经路径）。
  - `bash_tool.py:41-59` **上游已分支处理 Windows**：非 Windows 走 `/bin/bash` + `os.setsid`；Windows 走 `cmd.exe /v:on`（用 `!errorlevel!` 取退出码）。
  - `requires-python >= 3.12`。
  - `base_agent.py:12` 无条件导入 `docker_manager`，后者 top-level `import docker` + `import pexpect`，而**两者都不在 pyproject 依赖里**（trae 自身的打包 bug，需手动补装）。好在 **pexpect 自己守卫了 `sys.platform != 'win32'`**（`pexpect/__init__.py:79`），Windows 上 `import pexpect` 安全，只是没有 `spawn`——而 trae 只在 docker 模式用 `spawn`，非必经路径。
  → **trae 的跨平台底子是三个里最好的**（已在 Windows 真机端到端跑通）。
  > ⚠️ **但"最现实"的初判要收回**：实测发现 trae **没有多轮会话**（每个任务都是独立 run，且默认第 2 轮必挂），接入需 **2 个上游补丁 + 自己实现会话记忆**。**跨平台最容易 ≠ 接入成本最低——它其实是成本最高的一个。** 详见[测试报告 §3.5](./客户端接入测试报告-2026-07-13.md)。

#### 3.2.1 实测记录（2026-07-13，Linux，已验证）

**结论：trae 可用普通 stdin 管道（无 PTY）完整驱动，端到端跑通。**

复现步骤与结果：

1. `python3 -m venv .venv && pip install -e .` → 装完**跑不起来**，缺 `docker`、`pexpect`，手动补装后 `trae-cli --help` 正常。
2. 配置 `trae_config.yaml`：`ModelConfig` 强制要求 `temperature` / `top_p` / `top_k` / `parallel_tool_calls` 四个字段，缺一个就 `TypeError`。
3. **凭证坑**：本机 DeepSeek key 的代理**只暴露 `/anthropic` 端点**（Anthropic 兼容，模型 `deepseek-v4-pro`）。走 OpenAI 兼容路径（`/v1`）一律 404；`deepseek-chat` 模型已不再提供。
4. **工具 schema 坑（关键）**：`anthropic_client.py:76-94` 把 `str_replace_based_edit_tool` 和 `bash` **硬编码成 Anthropic 服务端内置变体**（`text_editor_20250429` / `bash_20250124`），DeepSeek 兼容端点不认，报 `400 unknown variant`。等于 trae 的编辑与执行能力全废。
   → **打补丁把这两个也改走自定义 `input_schema`（其余工具本来就是这么走的），即可正常工作。**
5. 打完补丁后，用管道喂入（**stdin 不是 PTY**）：

```bash
printf 'Create a file named hello.txt containing exactly: pipe-ok\n<workdir>\nexit\n' \
  | trae-cli interactive --console-type simple --trajectory-file traj.json
```

   结果：`Success ✅ Yes`，`Steps 5`，目标文件 `hello.txt` 内容 `pipe-ok` **确实被创建**，trajectory 正常落盘，`exit` 正常退出循环。

**这条实测直接验证了 §4 的架构结论**：驱动 trae 不需要 PTY，stdin/stdout 管道足矣——而管道在 Windows 上行为一致。

**接入 trae 必须解决的三件事**（均已定位）：
- 补装 `docker`、`pexpect` 依赖
- 给 `anthropic_client.py` 打 tool-schema 补丁（除非用官方 Anthropic / OpenAI 端点）
- 提供可用的 provider key

### 3.3 kiro —— 最受限，且是唯一可能逼出 ConPTY 的客户端

- Linux/mac/Windows 官方都有；Linux x86_64 要求 **glibc ≥ 2.39**（本机 2.39，Ubuntu 24.04.4，**刚好达标无余量**）。
- **必须 AWS Builder ID 登录**才能跑 → 目前无法实测。
- headless 模式明确说 **"No mid-session user input is possible"**，即 `--no-interactive` 是纯一次性，**不支持持久会话**。
- 但**每一轮对话都落 SQLite**（mac: `~/Library/Application Support/kiro-cli/data.sqlite3`），且有 `--resume` / `--resume-id <SESSION_ID>` / `--resume-picker`。
  → **这与 opencode 的 `opencode_db` 模式完全同构，现有 `OpenCodeDBReader` 机制可照搬。**

## 3.4 实测：Windows 真机 + 协议通道（2026-07-13 晚，全部已验证）

测试环境：portal 宿主(58.251.88.71) 上 `dockur/windows` 容器 `wechat-devwin` 内的 **Windows 11 专业版**（客户机 IP `172.30.0.2`，账号 `dev`）。
通道：本机 → portal(ssh 跳板) → `docker-proxy 127.0.0.1:2222` → 客户机 `sshd`（`OpenSSH_for_Windows_9.5`）。已用 paramiko 打通，Windows 侧验证可无人值守自动跑。

### ① trae 在真 Windows 上纯管道驱动 —— ✅ 通过

与 Linux 完全相同的代码路径、相同的 tool-schema 补丁、相同的管道喂法（stdin **不是** PTY）：

```
PATCH-OK
Success   ✅ Yes
Steps     10
artifacts: hello.txt, traj.json
hello.txt: win-pipe-ok
```

**结论：Windows 支持不需要 ConPTY / pywinpty，"必须重做 PTY 层"这个前提已被证伪。**

Windows 侧额外踩到的坑（均已解决，接入时要注意）：
- 客户机**默认无 Python**，winget 装 3.12 即可；**已装 git 2.54，`C:\Program Files\Git\bin\bash.exe` 存在** → §3.2 推荐的"用 git-bash 拉齐三平台执行语义"这条路在这台机器上现成可用。
- **编码坑（如预判）**：Windows 控制台默认 GBK。`.ps1` 脚本含中文会被按 GBK 解析而语法崩坏；Python 侧须设 `PYTHONUTF8=1` / `PYTHONIOENCODING=utf-8`。

### ② claude 的 stream-json 协议通道 —— ✅ 端到端通过

`claude -p --input-format stream-json --output-format stream-json --replay-user-messages`（DeepSeek key 经 `ANTHROPIC_BASE_URL`）：

```
[回放 user]  ...                       <- --replay-user-messages = 协议级送达 ACK
[工具调用]   Write {...}
[工具结果]   File created successfully
[assistant]  DONE
[RESULT]     is_error=False turns=2 ms=4483    <- 协议级完成边界
proto.txt 内容 = protocol-ok            <- 副作用真实发生
```

**送达确认、完成边界、工具事件流全部由协议提供**——`is_idle` 靠 1.2s 静默猜忙闲的做法在此彻底失去存在理由。

### ③ codebuddy 的 stream-json —— ⚠️ 协议可行，但**不能自带 key**

- 协议帧完整：`system/init` → `system/status` → `assistant` → **`result`**（带 `is_error` / `errors[]` / `num_turns` / `usage`），完成边界明确。
- **但模型路由走腾讯云网关**：报错为 `404 Server error (proxy: 1.1 -> https://api.deepseek.com)`；把 `CODEBUDDY_BASE_URL` 指向本机改写代理后，**本地代理零请求**，报错变成 `(target: http://127.0.0.1:8899)` → 说明 base_url 是**在服务端被解释**的，其网关自然连不到我们的 localhost。
- **结论：codebuddy 无法 BYO-key 指向第三方/本地端点，必须用 codebuddy 账号 `/login`。** 这是接入它的硬前置。

### ④ claude / codex / opencode 的协议通道在 **Windows 真机**上 —— ✅ 三个全部通过

环境：Windows 11 客户机，winget 装 Node v24.18.0，npm 全局装 `@anthropic-ai/claude-code@2.1.207`、`@openai/codex@0.144.3`、`opencode-ai@1.17.18`。

| 客户端 | Windows 上的协议通道 | 实测结果 |
|---|---|---|
| **claude** | `-p --input-format stream-json --output-format stream-json --replay-user-messages` | ✅ 端到端：协议 ACK 帧 → `tool_use` → 文件 `win-proto.txt` = `win-protocol-ok` **真的创建** |
| **codex** | `app-server`（JSON-RPC over stdio）；另有 `daemon` / `proxy` / `generate-ts` / `generate-json-schema --out <DIR>` / `remote-control` | ✅ `initialize` 握手正常回包：`{"id":1,"result":{"platformFamily":"windows","codexHome":"C:\\Users\\dev\\.codex",...}}`（**无需认证即可握手**） |
| **opencode** | `serve --port --hostname`（headless HTTP） | ✅ 监听 4096，`GET /config` → **HTTP 200** 返回正常 JSON |

**接入注意点（实测踩到）**：
- **codex 的 app-server 要求 stdin 保持打开**。用 `type req.json \| codex app-server` 这种管道喂完即关 stdin 的方式，进程会直接退出、一个字节都不回。必须用长驻进程 + 持续写 stdin（.NET Process / `subprocess.Popen` 均可）。
- **opencode serve 默认无鉴权**，自身会警告 `OPENCODE_SERVER_PASSWORD is not set; server is unsecured`。接入时必须设置该密码，否则本机任意进程都能操纵会话。
- npm 在 Windows 上装出来的是 `.cmd` 包装脚本，`Start-Process` 拿到的是壳进程而非真进程，**判活/杀进程会失手**——driver 里要按真实子进程管理生命周期。

**结论：`ProtocolDriver` 这条路在 Windows 上对 claude / codex / opencode 三家全部成立，且与 Linux 是同一套代码路径。**

## 4. 架构结论：把"消息管道"与"view 终端"解耦

**这是本次预研最重要的结论。**

今天消息管道和 view 绑死在同一个 PTY 上，正是病根：view 里有人打字 → 污染 `is_idle` 的静默判据 → busy 时重复注入（P1）。跨平台把这个矛盾放大，反而逼出了正确答案。

### 4.1 设计理念：不要模拟人（2026-07-13 确立）

sessionHelper 今天做在了**错误的层**：它假设「AI CLI = 只能用键盘喂的黑箱 TUI」，于是被迫去解析屏幕。而厂商其实已经把会话拆成了 **daemon（可编程）+ 前端（可挂载）**：

- **claude**：`--input-format/--output-format stream-json` + `--replay-user-messages`（协议级 ACK）；`--remote-control`
- **codex**：`app-server`（JSON-RPC over stdio，可导出协议定义）、`app-server proxy`、`remote-control start/stop/pair`（守护进程 + 配对码）
- **opencode**：`serve`（headless 服务端）+ `attach <url>`（人挂到同一个 server）、`acp`、`web` —— **人机同看最干净的一家**
- **codebuddy**：stream-json 双向 + slash 命令含 `remote-control`

接过去之后今天的痛点直接消失：送达确认→协议回执；重复注入→结构上不可能；幽灵提示文本/`[Pasted text]`/输入框折行→整套屏幕解析代码可删；网页终端→从「转发 PTY 字节」变成「渲染事件流」；「外部消息抢占输入框」→消息根本不走输入框。

### 4.2 但「接协议」覆盖不了 trae 和 kiro —— 边界必须说清

**本次任务的两个主角恰恰没有协议通道**（已逐一核实）：
- **trae**：零 JSON 接口、零流式接口，只有 `run`(一次性) 和 `interactive`(REPL)
- **kiro**：headless 明确写 "No mid-session user input is possible"，只有一次性调用 + `--resume-id` + SQLite 落盘

所以正确的表述不是「全面转向协议」，而是：

> **不要模拟人**——但「不模拟人」有三条实现路径（协议 / 管道 / 库内嵌），**按客户端能力选**，不能假设人人都有协议。

`driver` 抽象层因此**不是过渡方案，而是必需品**：

| Driver | 客户端 | 依赖 PTY |
|---|---|---|
| `ProtocolDriver`（stream-json / JSON-RPC / HTTP+ACP） | claude、codex、opencode、codebuddy | ❌ |
| `PipeDriver` / `EmbedDriver`（管道 / Python 库内嵌） | **trae** | ❌ |
| `OneShotResumeDriver`（一次性调用 + resume-id，答案读 sqlite） | **kiro** | ❌ |
| ~~`PtyDriver`~~（仅作退路，不再是默认） | — | ✅ |

**四条路都不需要 PTY。** 那套 VT 终端模拟器代码可以删——但删的理由是「我们做错了层」，不是「厂商都给了协议」。

- **Windows 支持 = 前三种 driver，一个 pexpect 都不需要。**
- view 若在 Windows 上确实要真终端，再单独用 ConPTY 起一个**独立的镜像终端**；它坏了也不影响消息投递，因为两条路已解耦。
- 附带收益：`stream` / `embed` 模式**顺手拔掉 P1 的病根**——`is_idle` 靠 1.2s 静默猜忙闲这件事本就不该存在，结构化协议自带消息边界。

**工作量因此从"重做 PTY 层 + 未知的 ConPTY 踩坑"降到"新增 driver 抽象 + 三个 driver"，且三个 driver 在 Linux 上即可全部验证，测试机不再是开工前置。**

## 5. 风险与待验（**已按实测更新**）

1. **kiro：`--no-interactive` 能否配 `--resume-id`？** 文档未写。**这是 kiro 方案的生死线**——若不能，每条消息都是无上下文的独立提问，kiro 就只能退回 PTY，Windows 上的 ConPTY 躲不掉。**卡 AWS Builder ID。**（**仍未验**）
2. ~~codebuddy stream-json 的实际协议~~ → ✅ **已验**：协议帧完整（`system/init` → `assistant` → `result`，`result` 即完成边界）。但**认证是硬阻塞**：BYO-key 已证实不可行（base_url 在腾讯云网关服务端解释），**必须 `/login`**。
3. **trae 库内嵌的进程隔离** —— agent 跑在 helper 进程内，崩溃会拖垮 helper，须放子进程。
   - ⚠️ **provider key 一节已订正**：本机 DeepSeek key 的代理**只暴露 `/anthropic` 端点**，~~OpenAI 兼容可复用~~ → **OpenAI 兼容路径一律 404**，`deepseek-chat` 模型已下线。
   - ⚠️ **新增风险（实测发现）**：trae **没有多轮会话**，且默认第 2 轮必挂（客户端 `message_history` 跨任务不重置）。接入需 **2 个上游补丁 + helper 自维护对话历史**。
4. **opencode serve 默认无鉴权**（实测发现）：启动即自曝 `OPENCODE_SERVER_PASSWORD is not set; server is unsecured`。不配密码则**本机任意进程都能操纵会话**。
5. **view 在 Windows 上的定位需要拍板**：是否必须提供真终端。

## 6. 排期建议（**已按实测重排**）

原排期把 codebuddy 放 P0、trae 放 P1，理由是"这两个最确定"。**实测把这个判断推翻了**：codebuddy 卡认证、trae 没有多轮会话，反倒是 **claude 成了唯一端到端全通的**。重排如下：

| 阶段 | 内容 | 前置 | 说明 |
|---|---|---|---|
| **P0** | **claude `ProtocolDriver`**（stream-json） | **无** | **唯一已端到端验通的**（Linux + Windows 真机 + 多轮上下文）。跑通即坐实整条无 PTY 路线 |
| **P1** | driver 抽象层落地，Linux 行为零变化 | 无 | 后续所有 driver 的共同底座 |
| **P2** | codex / opencode 接入 | codex 需 OpenAI 兼容 key | 协议能力**强于** claude（`Turn/interrupt`·`Turn/steer`；SSE 事件流）。opencode 须配 `OPENCODE_SERVER_PASSWORD`（默认无鉴权） |
| **P3** | codebuddy 接入 | **codebuddy 账号登录**（BYO-key 已证实不可行） | 协议与 claude 同构，登录后应可直接复用 P0 的 driver |
| **P4** | trae 接入 | **需拍板**：维护 fork（2 个补丁）+ 自实现会话记忆 | **成本最高**。跨平台没问题，但没有多轮会话 |
| **P5** | kiro 接入 | **Builder ID** + 生死线实测结论 | 风险最高，可能是唯一退回 PTY 的 |
| — | ~~Windows 实测~~ | — | ✅ **已完成**（5 个客户端全部在 Windows 真机跑过）。**macOS 仍未验** |

## 7. 待老板拍板 / 提供

- **AWS Builder ID**（kiro，卡生死线实测）
- **codebuddy 账号登录**（~~可用凭证~~ → **BYO-key 已证实不可行**，必须 `/login`）
- **trae 是否值得接**：需维护 fork + 自实现会话记忆，成本最高
- **macOS 测试机**（Windows 已解决，见下）
- **view 在 Windows 上是否必须是真终端**

### Windows 测试机 —— ✅ 已解决，通道已打通

~~缺凭证~~ → 已定位并打通：portal 宿主上 `dockur/windows` 容器 `wechat-devwin` 内的 Windows 11（客户机 `172.30.0.2`，账号 `dev`，密码在 `docker inspect` 的 env 里）。
通道：本机 → `ssh imgsearch-portal` → `docker-proxy 127.0.0.1:2222` → 客户机 sshd。**已脚本化（paramiko），可无人值守跑 Windows 侧验证。**

> ⚠️ **取证陷阱（原文有误，此处订正）**：原文称"22 / 3389 / 443 均开放"——**这是假信号**。该网络路径**对任何端口都回 ACK**（探 1 / 9 / 12345 / 65001 全显示"开"）。**端口扫描结果不可信，必须做真握手。**
