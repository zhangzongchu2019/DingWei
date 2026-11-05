# 基线与上下文

## 依据

- 基线仓库：`opus50tc2k/r51-integration`，commit `f7f31fca1d1f1468285138578c3f6fafacfb5537`，tree `150f51fea5e2de620912d1d66d35780935782865`。
- OpenSpec 约定：`openspec/config.yaml`，schema `spec-driven`，严格验证要求正文中文、结构标题及 SHALL/MUST 关键字英文、需求固定编号、Purpose 模板、每条需求有 WHEN/THEN 场景。

## 现有实现观察

- `tools/sessionhelper/` 与 `send.py` 的会话消息路径基于本机 helper / sessionHelper IPC；本变更不替换它们。
- Hub 目前为注册席位维护会话连接与在线状态；`internal/store` 的持久投递表名为 `session_delivery`，提供入队、领取、重试、过期与 ACK 状态更新。
- `internal/m8/delivery_ack_v2.go` 与 `internal/store/session_delivery_ack.go` 实现 delivery_ack v2：绑定 delivery、key、session、processing mode 与 generation，并在状态事务提交后确认；当前处理路径校验 `ack_protocol=2` 且 `processing_mode=driver`。
- 身份服务具有凭据哈希与吊销等既有模式可供设计参考；外部席位令牌表、OAuth 资源元数据和 HTTP 授权当前是否已存在：未核实。
- 普通消息经 Hub 路由，提及候选项由既有 AMR 解析；外部席位是否可被现有身份解析器寻址：未核实，实施须复用解析规则并增加受控映射。
- 当前产品是否支持远程 Streamable HTTP MCP、REST 外部席位、浏览器跨源调用及其互操作行为：未核实。

## MCP 规范依据

设计依据 MCP 规范版本 `2026-07-28`：

- [Transports — Streamable HTTP](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http)：单一 MCP endpoint、JSON-RPC POST、响应可为 JSON 或 SSE；服务端须校验所有连接的 Origin，非法 Origin 返回 403；建议正确鉴权。
- [Authorization](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization)：HTTP MCP 授权采用 OAuth 2.1；资源服务器发布 RFC 9728 Protected Resource Metadata，客户端通过其发现授权服务器；Bearer token 由 Authorization header 携带。静态自定义 token 而无所需 OAuth 发现与授权端点，不应宣称符合该 MCP HTTP 授权规范。

以上为文档核对，未运行 MCP 互操作测试；Hub 实现状态与客户端兼容性均未核实。正式实现前应再次核对届时适用的规范版本。
