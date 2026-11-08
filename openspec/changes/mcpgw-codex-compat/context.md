# 工作坐标

基线f2a5db5f；分支coder56solzzc/r56-mcpgw-codex-compat。唯一写者coder56solzzc.zzc，锁/tmp/coder56-mcpgw-compat.lock。先读openspec/specs/mcpgw-codex-compat，再读前单745de481与0877df37裁定；不读私有凭据或在线状态。

2025入口internal/mcpgw/codec2025.go；原2026在gateway.go；回调clients.go；备用文档codex_client.go与oauth.go精确本域选择。静态自有ID无网络请求，对任何其他ID仍走配置Clients/MetadataClients；额外路由由Gateway.Register同开关登记。无新增依赖/存储/配置。

本地测试用临时HOME/TMPDIR、假飞书、真实临时PG/SQLite，实际0.167.0二进制；当前OpenAI说明只辅助，版本结论绑定原文。原生回归放docs/reviews/r56-mcpgw-codex-compat，普通用户运行Go，锁venv跑canonical；不启动在线helper。
