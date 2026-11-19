# S-MCP-DCR 设计交件

状态：设计待组长审阅；未实施产品，不是DCR产品验收或发布GO。

设计页：[MCP-DCR](../../design/MCP-DCR.md)。设计源 `64f08d658f99aeec9092bc61003769ef4d597ccd`，基 r61 `4a349ee59a1d8d7b8b9c562c462bded2f8b0f192`。本交件全部只追加docs/openspec，产品、依赖、迁移、VERSION零改动。后续设计补充仅说明客户端缓存/TTL兼容及持久计量使用PG时钟。

## 审阅决定

- A-DCR-01：独立加密PG登记/计量；24h绝对TTL（1h～7d），4096存活/resource、128/source，来源5/min burst5、全局60/min burst20；可信代理右侧取源，有界GC/桶/并发。均为候选技术值，非业主定值。
- A-DCR-02：回环三种主机/动态端口；HTTPS完整URL名单初始空；私有scheme默认拒。CodeBuddy真实回调尚缺，不能冒称已兼容；实施前另协调PG迁移号。
- A-DCR-03：保留AUTH-FIRST未认证GET与畸形体先401，认证GET405，不做旧SSE。CIMD、内置客户端与原业务权限不放宽。

## 本轮实跑

普通用户dev（uid1001），Go1.22.2、锁定Python3.12.3、OpenSpec1.16.0，白名单环境、独立数据盘HOME/TMP；无继承SH/DINGWEI凭据。只跑一套canonical，无额外并行门。

- `SOURCE_COMMIT=64f08d658f99aeec9092bc61003769ef4d597ccd ./build.sh --test-only`：rc0，2026-11-19T08:25:51.619333Z～08:35:28.647034Z；两profile各1290、inventory97；`BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`。仅基线门，不能记成DCR新增功能已测。
- `OPENSPEC_TELEMETRY=0 openspec validate --all --strict`：48 passed、0 failed、rc0。
- `git diff --check`：rc0。基点至设计源非docs/openspec路径0；详见structure.json。

**未实施/未跑**：DCR端点、持久表、真实授权全链、DCR删保护、实现源mcpgw/extseat/m8三遍及qwen独立复核。对应tasks均未勾，不以旧基线门替代。

## 证据与下一步

仓根 `sha256sum -c docs/reviews/mcp-dcr-design/SHA256SUMS`；canonical原日志gzip解压后的SHA/字节数在evidence/raw-log.json，环境与命令/UTC/rc均留档。原始CodeBuddy私人排查件不复制到Git，设计页仅保留摘要和SHA。

按派单先将可审阅设计交组长；获得设计确认、协调迁移编号后才写产品代码。未推main、未部署、未接触现网、未出发布件。
