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
