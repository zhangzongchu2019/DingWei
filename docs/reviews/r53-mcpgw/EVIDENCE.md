# S-R53-MCPGW-IMPL 中间证据（未交件）

执行者 coder56solzzc，实取线程 01a1b01a-3bc9-7003-998c-53b56b27c2ec；父基线144b215e32e718553d9fef2e79c83ece164f6514。2026-11-06开始；源码仅Git交换，无部署/重启，A/C/D/helper包零修改。接口与装配见 internal/mcpgw/INTERFACE.md。

当前仅供组长只读试合，**不是最终交件或独立验收**。

- 四工具MCP/REST、现代头/元数据/版本/resultType、Origin、统一错误表与CIMD已实现。
- 真实C处理器＋extwire＋假飞书HTTP全流程已通过；PKCE错误、redirect/client/resource不符、重放、过期和吊销前置现核有反例。
- 0107按组长16:13:02Z授权新增，唯一mcpgw_oauth表；移除运行时建表。真实临时PG16并发兑换只有1成功，重放/过期/重开连接/资源隔离及过期索引/清理通过（HUB_TEST_PG_STRICT=1，无SKIP）。
- 首轮本包/相关包、m8定向、vet、本包race三轮与m8定向race三轮通过；后续0107/密钥降级改动后还须重跑受影响门，不借首轮结果宣称最终通过。
- 八刀overlay变异（PKCE、code重放、REST扩权、Origin、头一致、版本、WaitMax、Authenticate）均由指定语义断言杀死，原源码未修改；脚本 docs/reviews/r53-mcpgw/mutations.py；最终绑定复跑待办。
- canonical系统Python首轮rc1：缺依赖导致四个helper模块无法收集；原日志保留。改用既有锁定虚拟环境 /tmp/coder-0232-locked-venv 正在重跑。
- 额外仓储边界门rc1：精确基线144b215e也报同样10处建库/1处句柄逃逸，逐条位置相同，本路无新增。原日志与比较见 /tmp/r53-mcpgw-gates/boundary*，未放宽基线。
- 迁移账本/claim测试期待串的0107追加归集成组长（㊹-补12⑥），未擅改，已提前报告可能影响canonical末段。

原始日志本地 /tmp/r53-mcpgw-gates/；变异 /tmp/r53-mcpgw-mutations/。最终证据将追加可独立fetch的Git坐标、命令/UTC/rc及结果文件SHA；不会用本地路径冒充跨席交付。
