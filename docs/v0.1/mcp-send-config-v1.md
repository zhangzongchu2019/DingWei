# B4: MCP send 四家 CLI 配置样例

## 通用前提

1. sessionHelper 启动时设置环境变量 `DINGWEI_MCP_IPC_SOCK` 指向 Unix socket 路径（与 `mcp_ipc_sock` 配置一致）。
2. shim 脚本位于 `tools/sessionhelper/sessionhelper/mcp_shim.py`，可执行（`chmod +x` 或由 python3 显式调用）。

---

## Claude Code

```json
{
  "mcpServers": {
    "dingwei-send": {
      "command": "python3",
      "args": ["-m", "sessionhelper.mcp_shim"],
      "env": {
        "DINGWEI_MCP_IPC_SOCK": "/tmp/dingwei-mcp.sock"
      }
    }
  }
}
```

## Codex (OpenAI Codex CLI)

```json
{
  "mcpServers": {
    "dingwei-send": {
      "command": "python3",
      "args": ["-m", "sessionhelper.mcp_shim"],
      "env": {
        "DINGWEI_MCP_IPC_SOCK": "/tmp/dingwei-mcp.sock"
      }
    }
  }
}
```

## opencode

```yaml
mcp_servers:
  dingwei-send:
    command: python3
    args: ["-m", "sessionhelper.mcp_shim"]
    env:
      DINGWEI_MCP_IPC_SOCK: /tmp/dingwei-mcp.sock
```

## Cline (VS Code extension)

```json
{
  "mcpServers": {
    "dingwei-send": {
      "command": "python3",
      "args": ["-m", "sessionhelper.mcp_shim"],
      "env": {
        "DINGWEI_MCP_IPC_SOCK": "/tmp/dingwei-mcp.sock"
      }
    }
  }
}
```

---

## sessionHelper 启用 MCP IPC

在 sessionHelper 配置中设置 `mcp_ipc_sock`（例如通过命令行参数或配置文件）：

```bash
# 启动 sessionHelper 时指定 IPC socket 路径
python3 -m sessionhelper --mcp-ipc-sock /tmp/dingwei-mcp.sock
```

或通过环境变量：

```bash
export DINGWEI_MCP_IPC_SOCK=/tmp/dingwei-mcp.sock
```

sessionHelper 会在 Unix socket 上监听，mcp_shim 连接后发送 send_message 请求。

---

## 四家 CLI 差异

| CLI | MCP 配置方式 | stdio 支持 |
|---|---|---|
| Claude Code | claude_desktop_config.json / .mcp.json | ✅ 开箱 |
| Codex | .codex/config.json | ✅ 开箱(无需额外旗标) |
| opencode | opencode.yaml | ✅ 开箱 |
| Cline | VS Code settings.json → mcpServers | ✅ 开箱(修复 #6767 后) |

关键：全部使用 stdio 模式（无回环 HTTP），因四家 CLI 全部开箱支持 stdio MCP server——这是张宗楚 07-31 定论的传输选择依据。
