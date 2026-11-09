## Context

官方 0.167.0 CLI 在仅配置 URL 时，元数据是否包含 registration_endpoint 均采用 `https://chatgpt.com/oauth/codex/client.json`，发送 IPv4 回环随机端口 `/callback`，没有 DCR POST。原始记录见 `docs/reviews/mcp-zero-config/raw/discovery.json`。桌面未安装，未测。

## Goals / Non-Goals

消除默认身份的网络取件依赖，保留精确匹配、PKCE S256、租户、资源、逐次授权。无新路由、数据库或版本变更，不部署、不启动推理。

## Decisions

### 选择甲：固定副本（ZC-002）

实取 URL 为默认外部 client_id；匿名 HTTP 200，取得时刻和 SHA256 在报告及 fetch.json。go:embed 保存逐字节原文。原文含 localhost 和 refresh_token，不能直接当本网关策略；解析时仅留下原文中的 IP 回环回调、authorization_code/code 与 none。原文与有效策略分开记录。固定副本坏件直接 invalid_client，无网络兜底。

仅 client_id 字符串完全相等时选此路径，大小写、query、fragment、近似域名仍交既有 resolver；既有 HTTPS、公有 IP、无跳转与 DNS 回绑防护不变（ZC-003）。显式本域文档路径也不变。

### 不选乙：限制型 DCR（ZC-001）

在当前 CIMD 元数据下，仅加入 registration_endpoint 实测没有注册请求。因此新增注册状态、限额、清理和拒绝规则无法解决该原生请求的默认身份取件问题。没有把试验用注册端点加入产品。

### 回调与鉴权保持（ZC-003、ZC-004）

继续复用 registered：仅同一已登记 IP、精确 scheme/escaped path/query 可以忽略有效端口。默认原文只有 127.0.0.1 的 `/callback`，不合成 IPv6。派单后补20（rulings c9581bdb）批准两个精确本地身份在匹配层接受 `/callback` 或 `/callback/<1～64 位 A-Za-z0-9_- 单段>`；空尾斜杠仍拒。仅临时规范化登记匹配用的路径，不改变持久化的原始 URI，原始 scheme 拼写也保留校验。其他客户端仍逐字匹配原路径。不能由 CLI 推导桌面行为。token 仍绑定此次真实 redirect/client/resource/verifier；每次工具 HTTP 请求现核身份，tenant/audience 不绕过。

## Risks / Trade-offs

上游文档将来变化不会自动生效；需重新实取、审查、固定 SHA 并回归。当前桌面目标尚缺实测，不作为已完成的桌面接入验收。现网 403 不影响精确默认身份本地解析，其他外部客户端仍依赖其原取件条件。

## Migration Plan

不迁移数据。独立复核后由发布单决定集成，回退产品提交即可恢复显式本域 client_id 路径；不得删除身份、ACK 或记录。本件没有部署授权。

### 补19、21的默认授权方案（ZC-007）

只做调查和影响方案，位于 docs/reviews/mcp-zero-config/DEFAULT-CONSENT-PROPOSAL.md；当前官方入口证据非钉版桌面实测。现有 PG np_extseat 的 8 位 CHECK 必须纳入未来迁移，不只改 HTML；本提交不实现默认短名、目标或表单重排。
