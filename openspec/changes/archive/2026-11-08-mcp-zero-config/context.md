# S-MCP-ZERO-CONFIG 上下文

- 写者：coder56solzzc.zzc，实际线程 `01a1bd1c-c28e-7c60-9594-7894d0078533`；唯一写锁 `/tmp/coder56-mcp-zeroconfig.lock`，其他会话只读。
- 派单后同步：rulings `c9581bdb` 补19～21；补20仅有限回调路径扩展，补19/21只出方案；不扩大默认 client_id 前缀。
- 基线：`68505cd3`；分支 `coder56solzzc/mcp-zero-config`。不改 M3 工作树，不部署。
- 代码：`internal/mcpgw/codex_client.go`、`oauth.go`、原文 `codex_client.original.json`；测试 `zero_config_test.go`。
- 证据/复跑脚本：`docs/reviews/mcp-zero-config/`；模拟飞书仅限临时 Hub、PG、HOME 和合成身份。
- 官方客户端：0.167.0；二进制路径、SHA、命令在 raw。未安装桌面版，不由 CLI 外推。
- Go 用普通用户；测试环境白名单，临时 HOME/TMPDIR，公共 PG 缓存可复用，不继承在线凭据或 SH_/DW_ 环境。
- 历史主能力 `openspec/specs/mcpgw-codex-compat/` 及兼容变更不改写。本件是加法增量。
