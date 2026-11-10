# 设计

- `validRedirect` 仅把大小写不敏感、完全等于 `localhost` 的 HTTP hostname 视为回环；HTTPS localhost 和带尾点、子域等形式拒绝。依据 RFC 8252 §7.3，HTTP localhost 原生回调可使用动态端口。
- `registered` 对已登记无端口 localhost 仅忽略有效端口；scheme、escaped path、query 保持完全匹配。token redemption 仍绑定授权请求中的实际 redirect。
- `validateClient` 要求 `authorization_code`，允许额外 `refresh_token`，拒绝未知 grant；token endpoint 保持原有 authorization_code-only 分支。
- Connect CSP 复用 `withLoopbackFormAction`，只加入经过回调登记校验的精确 `http://localhost:<有效端口>` 源；没有通配符。
- `registeredCodexRedirect` 显式限定 IP 回环字面，避免通用 CIMD 新能力扩展 Codex 身份。
