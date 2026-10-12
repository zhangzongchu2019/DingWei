# macOS＋Codex 桌面：0.2.2.2 开发验收

首轮 PM 目标由业主确认：MacBook / macOS / 最新 Codex 桌面应用。准确版本、处理器架构由本机只读探测取得，不把“最新”作为固定版本号。
这是开发验收包，不是正式发布。现网 Hub/helper、飞书与 .95 不因本流程改变。

## 1. 在 Mac 本机构建

检查收到的源码验收包完整 SHA，解压到永久目录；归档内的 SOURCE-MEMBER-PINS.json 是逐文件固定清单。需要 Python 3.11+。没有合适 Python 时脚本会停止，用户从 https://www.python.org/downloads/macos/ 安装后重试；不自动安装系统 Python 或 AI CLI。

从解压目录运行（选择从未使用的新输出目录）：

```sh
sh tools/sessionhelper/deploy/macos/build-desktop-candidate.sh \
  "$HOME/Applications/dw0222-mac-pilot" \
  "/Applications/Codex.app"
```

实际应用若是 ChatGPT.app，应把第二个参数改为该 .app 的实际路径。不传第二个参数时自动探测 /Applications 和 ~/Applications；存在多个应用时拒绝自动选择。该名称兼容依据见[官方桌面文档](https://learn.chatgpt.com/docs/app)和[官方 MCP 文档](https://learn.chatgpt.com/docs/extend/mcp?surface=cli)。

此脚本只核源码、创建独立私有 venv、构建本机冻结包及做本地测试。使用下载依赖，需要网络；不关闭 Gatekeeper/SIP、不读取钥匙串、模型认证、桌面历史、cookie、设备凭据，不连接 Hub。没有登录或模型调用。
生成 desktop-metadata.json 的架构/应用名/准确版本/build 仅是本机元数据，不是签名、登录或自动提醒证明；不会启动 Codex CLI 或另建会话。

## 2. 配置测试专用 MCP

```sh
"$HOME/Applications/dw0222-mac-pilot/bin/sessionhelper" \
  --setup --configure-only --desktop-app "/Applications/Codex.app"
```

按提示选择：
- 接入模式：1（Codex 桌面应用）；Hub 可以保留默认地址，本步骤不访问它。
- 用户 ID：zhangzongchu，简称 zzc；Agent 名建议 macpilot。
- 状态目录：~/.dw0222-pilot；工作目录可留空。
- 配对方式可填 Web；configure-only 不申请账号、不生成/提交配对码、不启动真实 helper。

不要使用已有 Agent 的状态目录或凭据。MCP 配置按确定差集增量追加，保留原配置与私有备份；已有不同配置的同名条目拒绝覆盖。在当前桌面应用的 MCP 设置启用并重启该 MCP 服务；不另开 CLI 会话。确认出现 dingwei-macpilot 及 dw_inbox/send_message 工具。

## 3. 测原桌面会话的显式读取与回复

保持以下命令所在终端打开：

```sh
"$HOME/Applications/dw0222-mac-pilot/bin/sessionhelper" \
  --desktop-pilot --profile "$HOME/.dw0222-pilot/setup-profile.json"
```

该模式只向独立测试 inbox 写一条已知合成消息，并打印 message ID。它拒绝使用有凭据、待申请、已有进程或 socket 的目录，不碰真实 Agent。只有固定本地测试收件地址及固定测试回复可通过，其余发送拒绝；无 Hub/飞书/其他 Agent 的消息。

在你原来使用的 Codex 桌面聊天中输入：

> 请仅用 dingwei-macpilot 的 dw_inbox 查未读列表并读取那条 desktop-pilot 测试消息，然后用 send_message 回复：to=desktop-pilot#LOCAL，body=DW_DESKTOP_OK，in_reply_to=刚读到的 message_id。不要读取文件或使用其他工具。

这是业主主动发起的一次真实桌面模型交互。程序本身不调用模型；使用的是桌面中原有模型登录。
完成后 Ctrl+C 停止本地测试服务器；只清理本轮自己拥有的 socket/lock，不删除已有真实状态。测试私有报告与合成 inbox 保留；报告不存输入/回复正文或凭据，只记录 ID/count/state。
不自动删除 MCP 条目或覆盖用户配置；如仅做试验，可在 MCP 设置禁用本轮 dingwei-macpilot。

## 4. 回报与通过口径

只回报 desktop-metadata.json 中的应用名称、版本/build、架构，以及测试报告中的 read_count/reply_count 和你是否在原桌面会话看到结果；不要发送 config.toml、私有日志、密码、cookie 或凭据。

- frozen 本地测试 PASS：仅协议、私有配置、合成 IPC 读/回复通过。
- 原桌面显式交互 PASS：需要业主确认原会话真正展示并调用了工具；本地 count 不是独立 UI 证据。
- 自动提醒/唤醒：尚未实现（native_auto_notify_implemented=false），仍为 NOT_VERIFIED。本模式没有自动向原会话注入消息，也不假称主动提醒已实现。
- 真实路由与飞书一步开户：需同版 Hub 上线与独立权限/恢复验收后另测；不要用现网 .221 冒认 .222 接口已部署。

普通系统通知、MCP 配置成功、显式工具调用与原桌面会话自动启动模型是四个不同结果。自动能力未闭合前，0.2.2.2 仍不计完整发布就绪。

端点增量（后续开发分支）：如有明确的原桌面已有 Unix 控制入口，可用 `--desktop-endpoint-probe --socket <明确路径> --thread-id <业主选定线程ID>` 只读验证。不要为此启动新 daemon/CLI，不要提供 pairing code、token 或聊天正文。该探针尚不在先前 43a03e2f4 冻结源码包内。仅 loaded=true 不能证明原桌面绑定或自动通知可用。


## 5. 原会话先等待，再收到合成消息（当前开发增量）

此模式需要最新开发源码/本机重新构建的包，旧43a/b695冻结包不含本轮入口。停止前一个 pilot 并确认其 socket/lock 清理后，可复用无凭据的测试 profile：

```sh
"$HOME/Applications/dw0222-mac-pilot/bin/sessionhelper" \
  --desktop-pilot --message-after-wait \
  --profile "$HOME/.dw0222-pilot/setup-profile.json"
```

此时报告 fixture_posted=false，私有测试收件箱为空。只有原聊天发起 MCP 等待并实际挂起后，试验程序才投放一条合成消息，无需把握倒计时，也不连接 Hub、飞书或真实 Agent。
在原来使用的 Codex 桌面会话输入：

> 请只用 dingwei-macpilot 的 dw_wait_message，wait_seconds=5，等待本地合成测试消息。收到后确认 message_id，然后用 send_message 回复：to=desktop-pilot#LOCAL，body=DW_DESKTOP_OK，in_reply_to=这条 message_id。不要调用其他工具，不读文件，不另开AI会话。报告这次收到和回复的结果，然后停止试验。

这一 pilot 的 send_message 只认可该固定合成回复和原 ID，结果标 sent_to_hub=false。**不要用 dw_reply_message 来伪装 Hub 确认**：该工具在 pilot 中明确拒绝，因为没有真实 Hub 及 durable queue。真正 Agent 回复须在同版测试 Hub/helper 的原目标确认通道另验。

试验报告只回传 fixture_posted、wait_return_count、reply_count、hub_connections=0 和你是否在原聊天看到结果。它不能证明空闲自动唤醒，也不能证明真实消息端到端；取消/退出后不自动建立新的模型轮次。Ctrl+C 停止后保留合成报告/DB，仅清理本次 owned socket/lock。

当前自动冻结包验证脚本同时覆盖即时显式读和延迟等待两种本地模式，并检查 pilot 不伪造 Hub 确认；Linux 验证只证明冻结入口，不代替 Mac 应用 UI。
