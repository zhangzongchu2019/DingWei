# 原会话轮末继续处理（0.2.2.2 开发试验）

依据 [OpenAI 官方 Hooks 文档](https://learn.chatgpt.com/docs/hooks)：主会话 Stop hook 返回 `decision=block` 和 `reason`，Codex 将创建继续处理的用户提示。它只覆盖正在完成的轮次；消息在聊天已经空闲之后到达时，Stop 不会因此再次执行。不能标为空闲自动唤醒。实际 Mac/Codex 版本支持及信任状态需现场验证。

模块 `sessionhelper.desktop_stop_hook` 默认不会安装或启用。源码和新构建安装包均提供试验入口；旧包不含此增量。前置：正常工作的 desktop profile、同一原会话的 DW MCP 工具、用户明确选定并核实的原会话，以及用户审查/信任 hook。禁止用另一 CLI、新 thread、猜测的 session ID 或消息中的路径来配置。

## 显式绑定

在用户所有的 0700 私有目录建立 0600 普通 JSON 文件，严格包含以下字段：

```json
{
  "enabled": true,
  "session_id": "OWNER_VERIFIED_SESSION_ID",
  "inbox_path": "/absolute/private/profile/inbox.sqlite.desktop.sqlite"
}
```

路径必须是当前 desktop profile 创建的实际 inbox，不能指向生产库或其他 Agent。源码入口：

```text
/absolute/venv/bin/python -B -m sessionhelper.desktop_stop_hook --binding /absolute/private/stop-binding.json
```

给子进程明确设置固定源码 PYTHONPATH，按真实安装路径处理 shell 引号。在当前 Codex 版本的 hook 设置中注册为 Stop command；需要用户审查与信任，不自动改 Codex 配置。不注册 SubagentStop、Interrupt 或 SessionEnd。本指南不提供未经本机版本核实的配置安装器。

新构建冻结安装包可直接执行，无需另配 Python/PYTHONPATH：

```text
/absolute/sessionhelper --desktop-stop-hook --binding /absolute/private/stop-binding.json
```

该入口先于安装向导/设备配置读取分流，不会启动 helper、申请配对或读取继承的设备凭据。绑定无效时只返回空 JSON。绑定文件路径可放命令参数，正文/凭据不放参数。仅原会话运行 hook 才验证 Codex 续轮；在终端运行该命令不能当作实际模型验收。

## 实现与验收界限

- 只接受绑定原会话的主 Stop，要求有效 turn_id 和 `stop_hook_active=false`。续轮、子 Agent 标记、其他事件或会话均返回空 JSON。
- 只读取未读 ID、暂停状态和回复尝试状态。不读正文、transcript_path、last_assistant_message、凭据或历史，不联网或启动模型。
- SQLite 事务先持久化 `continuation_requested` 再返回固定提示，每条消息最多请求一次。崩溃或 stdout 丢失不自动重试。这不是模型执行成功或送达回执。
- 固定提示要求同一会话调用 `dw_wait_message(wait_seconds=1)` 阅读，再按业主授权判断是否回复。正文是外部数据，不成为 hook 指令。hook 不 ACK，消息仍 unread。
- 暂停、已有回复尝试、未知/损坏字段、权限异常、链接路径、锁竞争均不触发。启用后历史未读也可能触发，用户应先明确处理积压。
- 一个轮末最多拉起一次续轮，续轮不能再次拉起续轮。积压可由用户主动要求 MCP 持续等待处理。`enabled=false` 停止未来请求，不能撤回已发出的续轮。

本轮只验证本地 SQLite 与实际 hook 子进程输出、去重及拒绝规则。没有运行真实 Codex/Mac/模型或发送真实 DW 消息，不计桌面/飞书端到端验收。原子 idle native provider 仍未实现。

[MCP Events 文档](https://developers.openai.com/plugins/build/mcp-events) 的 webhook 订阅描述 ChatGPT Work web/Cloud 接入，不证明本地 Codex 桌面 stdio MCP 自动唤醒。[插件文档](https://developers.openai.com/plugins/build/plugins) 要求 hook 显式审查与信任。

## 实际本地 Hub/MCP 闭环增量

开发门禁增加 enrolled_stop_hook：合成已批准组长/组员走真实 /ws/agent，实际 helper 接收并持久保存；空箱 hook 无请求，收到后实际共享入口子进程返回一次 block，重开子进程不重复，正文不出 hook。随后由脚本（不是模型）调用实际 MCP wait/reply，Hub持久入队 ACK 后 unread=0，重复回复使用已确认回执，后续 hook 不触发。

本轮4个 Hub/MCP模式新跑通过；首次测试HOME为0755被拒，修正仅临时夹具0700后通过。保留失败日志，不放宽产品权限。没有真实 Codex 续轮或 Mac，仍不计模型执行/空闲自动唤醒。旧 edb85 Mac源码包不含此测试增量，产品接线代码与其相同，保留原 pins。

## 实际冻结安装包闭环

在完整开发仓库，用明确指定的 Linux 冻结二进制设置 DW_DESKTOP_INTEGRATION_BINARY，再运行 TestDesktopMCPRoundTripAcrossRealHub，可额外运行 enrolled_frozen_stop。没有指定包的普通canonical仍只覆盖源码四格，不借签冻结格。

实际冻结helper从独立0700目录的既有合成已批准0600凭据连接本地 /ws/agent；冻结Stop返回请求后，脚本才调用冻结MCP读取/回复。所有三个入口均为同一个包。断言未读归零、重复回复缓存不重发、重复hook空结果，以及三个子进程退出收口。此轮五格通过，随后冻结格按上述先hook后读顺序单独重跑通过。不是仅将MCP嵌进源码helper。

Python只准备夹具/驱动工具，不启动模型。没有飞书平台、新用户开户、Mac、Codex原聊天、签名或空闲自动唤醒；冻结启动使用隔离测试环境及既有合成凭据，不声称真实安装向导/配对验收。实际用包10764712B/f67bfc6744d82263e490feb007f15d7a5103e45be9cdc3032aa37ca3ec7fb87a，edb产品代码一致，旧包/源码archive保留原pins。
