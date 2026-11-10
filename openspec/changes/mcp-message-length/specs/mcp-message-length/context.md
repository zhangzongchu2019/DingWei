# MCP消息长度基线与上下文

## 基线

main 04cfb018上，MCP send_message正文校验256KiB；HTTP JSON壳1MiB；extseat原始正文1MiB但不计服务器抬头；helper普通IPC行64KiB、body-only行1MiB减一，websockets连接未设置max_size。后续补59的回程提示同样属于服务器生成正文预算。

## 代码位置

- 共享UTF-8与JSON预算/身份抬头：internal/extshared/limits.go。
- PG受理和回程入队：internal/extseat/inbox.go。
- MCP两种codec、REST与错误映射：internal/mcpgw/tools.go、gateway.go、codec2025.go。
- 投影、外部路由与失败详情：internal/m8/extseat.go、hub.go。
- helper消息IPC与单独安装CLI：tools/sessionhelper/sessionhelper/ipc_limits.py、ipc_listener.py、send_dingwei.py；WS接收在app.py。

## 环境与交接

Go1.22.2，临时PG用HUB_TEST_PG_STRICT=1；helper使用requirements-runtime.lock对应venv，独立HOME。结果、包成员和变异复核位于docs/reviews/mcp-p1-len。此文件随SENDWAIT证据补齐首次能力上下文，不改LEN已交源码、编号或正式基线。
