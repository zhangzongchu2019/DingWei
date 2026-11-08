# Agent 寻址基线上下文

代码锚：6e2c836491f8ba506833d6f849fbe73530e13f92；2026-11-08 首次补基线。identity/service.go ResolveFullName 只解析主身份两段；Hub parseAddress 仅处理井号传输封装；AMR token 可含多个点。完整逐点位置与旧版实测在 docs/design/SUBAGENT-ADDRESSING-PHASE1.md 第7、8节。此处记录已有行为，不声称子能力已实现。external 主名有下划线兼容。工具链 OpenSpec1.16.0、Node>=22.12；Python锁依赖venv，复现用Go overlay不改产品。后续上下文以该变更context与交接记录为准。

M2（2026-11-08）：M1获〔51〕-补12，子Agent走r57；基线eae9eca1。helper实现位置tools/sessionhelper/sessionhelper/{app_server_broker,sub_agent_store,sub_agent_runtime,sub_agent_connection}.py；原有CodexDriver/Adapter/inbox加作用域，默认SH_SUB_AGENTS_V1关闭。协议与原生历史主thread限制见docs/design/SUBAGENT-P1-M2-HELPER.md。只用Git交件，本地隔离验证，不部署。
