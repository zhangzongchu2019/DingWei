# 背景与出处

- 来源变更：archive/2026-11-10-mcp-cacheable-result（togpt60lunazzc/mcp-cacheable-result 6e45a25c，基线 main b4273dda）。
- 业主实测：Claude Code 2.1.292 授权通过但 tools/list 因缺 ttlMs／cacheScope 被客户端拒（rulings ㊿-补69）；依据 MCP 2026-07-28 changelog SEP-2549。
- 上线：r56.5 0.2.3.36 f1a445da，GO-R565-PROD-20261110T1808Z 于 2026-11-10T18:19:53Z SWITCH_PASS（㊿-补73～补75 及结项条）。
- 验收：Claude Code 2.1.292 端到端由中国区客户端工程师执行（㊿-补72）。
- 代码位置与测试见归档变更 context.md。
