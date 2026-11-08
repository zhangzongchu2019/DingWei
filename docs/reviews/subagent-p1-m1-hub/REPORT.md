# S-SUBAGENT-P1-M1 Hub 交审

执行者 coder56solzzc.zzc；实取 CODEX_THREAD_ID=01a1bd1c-c28e-7c60-9594-7894d0078533；to=opus50tc2k.zzc。唯一写者由 `/tmp/coder56-subagent-m1.lock` 原子 mkdir 决定，另一路只读不碰工作树。工蜂仓库 `wego-ai/dingwei`，分支 `coder56solzzc/subagent-p1-m1-hub`。

指定父 `79a60a1e351c207ff0b46bdf9151e846ec83f5d4`；产品／测试冻结源 `59c02c90e614af9fb2f14f832caf8807a3b8b6fd`，tree `760cb23fee405a16dcbcda5c36bef3419229c878`。冻结源已 push 并核 ls-remote 一致。后续证据提交只追加报告、复跑包装器和进度；产品源码及原测试不改写。`SOURCE-SHA256SUMS` 对全部差集内部源码／Go 测试／变异脚本作逐文件校验；`ARTIFACT-SHA256SUMS` 覆盖本报告及证据。

## 范围与实现

M1 的 Hub 侧实现：结构化路由解析、专属凭据 Codex 当前连接能力协商、全量树持久 ACK／幂等／重同步、墓碑与别名防重绑、0075 加法迁移、先根授权后寻子节点、可信 helper 来源校验、节点代次投递身份、原子 sidecar、队列出队／最终写／ACK 的作用域复核、持久来源回程与失败通知。协议与回退细节见 `docs/design/SUBAGENT-P1-M1-HUB-PROTOCOL.md`。

产品 11 文件，测试 4 文件，设计／OpenSpec 5 文件（冻结提交共20文件）。不修改 helper、View、Console、identity API、第三方依赖或 VERSION。未部署，不触碰现网／预发／在线席位。原生 threadId 绑定、broker、分道队列、生命周期执行属于 M2；View／Console 与混部／回退演练属于 M3，不以本单 Hub 测试冒充它们完成。

旧索引兼容是实现中的关键发现：`idx_session_delivery_source` 的唯一键不含节点，单改 delivery_id 仍合并兄弟。子投递 source_type 因而追加来源／目标节点代次摘要，原消息 ID 保留，旧根类别和旧索引不变；确定性 delivery_id 与内存 claim 也包含作用域。迁移无 DROP、不重建旧表、不改旧行。

同步检查的 rulings tip 为 `f23a4cca08d5b71c30200dc308c81fb860fd6c70`（最新〔51〕-补11，包含生命周期已批默认）。同期集成 `68505cd3d5a5fd9753a1c843655c8387bf2ea15c` 相对指定父只追加 VERSION，未改本单规格或迁移序列；保留派单父，不夹带版本位。

## Hub 判据格与变异

下面的测试均在隔离 HOME／短 TMPDIR、剥离 SH_／DINGWEI_／DW_／DATAD_ 的普通用户环境运行。专属凭据由测试内存 identity 夹具生成；WS 为本地 httptest，数据库为临时 SQLite。canonical 自带的实际 client／PostgreSQL 格也只使用自己的隔离实例，无真实飞书。

| 格 | 正例／负例 | 删除或扰动后的目标红 |
|---|---|---|
| AA001 全名 | 短根补属主、两段根、三至五段路径与根到叶顺序；空段、非法字符、超长、Unicode混淆拒绝；历史 external 两段根保留 | segments → AA001_MALFORMED |
| AA002 层数 | 一级可投；二三级 phase_not_enabled；六段 depth_exceeded | depth → AA002_DEPTH_EXCEEDED |
| AA003 拒收／无父回落 | 已协商 active／别名成功；未协商、树未确认、不存在、draining／ended分别拒收；冲突路径不投父 | parent-fallback／ended／reconnect-ready／codex-capability → AA003_NOT_FOUND／ENDED_DISTINCT／RECONNECT_TREE_NOT_INHERITED／CAPABILITY_BOUNDARY |
| AA003 正文与规范目标 | 包含换行、中文、NUL的正文原字节保留；别名命中同一 opaque node，信封为规范完整名 | payload → AA003_BODY_AND_TARGET |
| AA004 来源／权限 | 当前 helper 节点来源、根回复原子节点、持久自动回程；未经可信连接的字段、兄弟伪造、跨编组及无效目标拒绝，主身份API仍拒三段 | source-identity／authorization → AA004_CHILD_ORIGIN／AUTH_BEFORE_ADDRESS |
| AA004 原有回复策略／失败回程 | 子来源默认不自动回信，expect_reply才回；逐条失败通知同事务保存原来源节点，原文不泄露、不回父 | reply-policy／failure-return → AA004_CHILD_DEFAULT_REPLY_POLICY／FAILURE_RETURN_TO_NODE |
| AA008 的 Hub 协商与树 | 当前conn_seq、已持久revision ACK、同版同摘要幂等；旧epoch、同版异摘要、版本倒退、遗漏节点、墓碑复活及别名重绑拒绝重同步 | snapshot-epoch／snapshot-version／revive／alias-rebind → AA009_STALE_CONNECTION／SAME_REV_DIFFERENT_DIGEST／ENDED_CANNOT_REVIVE／AA003_ALIAS_REBIND |
| AA009 兄弟身份／去重 | 同envelope发两兄弟有独立行／sidecar／ID；来源及目标节点、代次均入身份；重新发同目标确定性ID不制造新身份 | dedup-scope／claim-scope／node-id／generation-id → AA009_SIBLINGS_ROUTE_DELIVERIES／ID_INCLUDES_NODE／ID_INCLUDES_GENERATION |
| AA009 事务／迁移／恢复 | sidecar插入故障不留下原投递；重开库读到作用域；迁移前旧投递逐字段相等；旧行无sidecar为根；来源运输重连不改持久身份，但旧连接不能新注入 | sidecar-atomic／migration-old-data／generation-check → AA009_SIDECAR_ATOMIC／MIGRATION_OLD_DATA／GENERATION_CHECK |
| AA009 队列与ACK | 排队后未协商、缺sidecar、封口或旧代次拒绝出队及最终写；兄弟／旧代次ACK不推进状态；回程核对持久来源代次 | replay-capability／ack-scope／reply-generation → AA009_UNNEGOTIATED_REPLAY／CROSS_LANE_ACK／REPLY_GENERATION_PIN |
| AA009 回退关闭门 | feature-off停止协商及子路由，普通根信不受保留字段门影响，表与审计保留 | feature-off → AA009_FEATURE_OFF |

逐刀只改现有文件的 Go overlay，原工作树产品不变；必须非零且包含指定判据，编译失败不计目标红。26刀原文及汇总保存在 raw/mutations-final，正向控制为冻结源专项与整包。M2 的原生 threadId 伪造、broker 并发事件等仍需其正式交付，本报告没有伪造这些已测。

## 最终门禁

| 门 | 最终读数 |
|---|---|
| m8 整包 | rc0；1524 pass／0 fail，既有可选skip37；含子测，顶层904 pass。270.927s |
| store／subagent | rc0；604／8 pass，store可选skip5；迁移、重启、故障点及作用域断言均过 |
| mcpgw／extseat／cmd | rc0；mcpgw125、extseat26、cmd/workpulse259 pass；cmd其余包均过，workpulse可选skip6 |
| canonical | rc0；claude／codex各1129/1129，27 Python／2 shell，BUILD_TESTS_OK |
| OpenSpec1.16.0 strict | rc0；27/27 |
| Go build | go build ./... rc0 |
| 专项 race | m8／store／subagent的全部新专项，-race rc0 |
| 兼容 future | rc0；原两条future断言保持绿，无父误投 |
| 逐项变异 | 26/26目标红；运行器rc0表示全命中，逐刀go test均rc1且有指定判据 |
| model／extshared附加检查 | rc0，保持既有错误码／external名称组件兼容 |

完整逐包pass／skip和顶层计数见raw/PACKAGE-COUNTS.json，48个跳过事件的原名／具体条件见raw/SKIPS.json；m8的37项是既有可选原生／PG等条件，未改变条件或为本单新增跳过。原Go JSON事件经gzip无损保留，解压内容SHA和长度见raw/GZIP-SOURCE-HASHES.json。最终canonical的开始22:43:09Z、结束见raw/canonical-final.json。

最终门均绑定冻结源，UTC起止、命令、rc、uid见 raw/对应JSON；m8完整逐测试事件与跳过条件原文保留。末尾证据提交不改变测试对象。正式最终结果只使用带 final 后缀的运行。

## 开发阶段失败与修正

- 首轮兄弟投递暴露旧 source 唯一索引合并，第二个 sidecar触发外键失败；随后加入上述 source_type 作用域与确定性ID，未删除原索引。原文 `raw/early-hub.stdout.gz`。
- 首轮新建第二条健康连接被既有 already_connected 门拒绝；重连用例改为先真实断开第一条再连，不放宽健康连接拒收门。
- 第一次整包的一项 presplit 源码顺序检查未命中临时上下文变量名；将可信客户端 context 绑定移到已鉴权请求入口，保留原 handler 调用形态，原测试一字未改。
- 同次cmd两项 Unix socket 测试的绝对路径过长、bind invalid argument；第一次canonical的真实client格也失败（子进程只给出受保护摘要，未打印具体子错误，不补猜它与路径失败是否同因）。最终统一短 TMPDIR 后复跑；不放宽用例时限、不跳过、不改单测断言。第一次 packages/canonical 原文和UTC元数据保留 `raw/pre-final-*`。第一次canonical的两helper profile均1129/1129通过，失败发生在后续shell格，未冒称canonical通过。
- 初轮22刀用于开发反馈；最后26刀重新运行在已推送冻结源，前者不替代最终结果。正式读数以最终证据为准。

## 复跑

Python环境须符合 requirements-runtime.lock；以本席为例锁环境 `/tmp/coder-0232-locked-venv/bin`。其他席位用自己的等价锁环境。包装器不读取或输出席位凭据，短临时目录在 `/tmp/sm1-*`。

```sh
python3 tests/subagent-p1-m1/run_isolated.py --output /tmp/m1-review --python-bin /path/to/locked-venv/bin packages -- go test -json ./internal/m8 ./internal/store ./internal/subagent ./internal/mcpgw ./internal/extseat ./cmd/... -count=1
python3 tests/subagent-p1-m1/run_isolated.py --output /tmp/m1-review --python-bin /path/to/locked-venv/bin canonical -- bash scripts/run-build-tests.sh
python3 tests/subagent-p1-m1/run_isolated.py --output /tmp/m1-review mutations -- python3 tests/subagent-p1-m1/run_mutations.py --output /tmp/m1-review-mutants
python3 tests/subagent-p1-m1/run_isolated.py --output /tmp/m1-review race -- go test -race ./internal/m8 ./internal/store ./internal/subagent -run '^TestSubAgent|^TestRouteParser|^TestSnapshotCanonicalDigest' -count=1
python3 tests/subagent-p1-m1/run_isolated.py --output /tmp/m1-review --python-bin /path/to/locked-venv/bin future -- python3 tests/subagent-addressing-design/run_legacy_probe.py --future --output /tmp/m1-review-future
OPENSPEC_TELEMETRY=0 openspec validate --all --strict --no-interactive
sha256sum -c docs/reviews/subagent-p1-m1-hub/SOURCE-SHA256SUMS
sha256sum -c docs/reviews/subagent-p1-m1-hub/ARTIFACT-SHA256SUMS
```

OpenSpec需1.16.0。首次证据执行使用本席同逻辑本地包装器；交付的 run_isolated.py 另做uid／短路径烟测并记录source_commit，供独立复核复现，不修改被测产品。

## 回退限制

`DW_SUB_AGENTS_V1=0` 是新二进制拒收门，不是完成回退演练。回旧二进制之前必须完成封口、逐条退信、在途对账及歧义子信隔离；旧claimer不懂sidecar，不能直接接手仍含子投递的根队列。不删除墓碑／ACK／sidecar，不DROP，不把unknown改processed。M2／M3继续完成实际生命周期和回退演练。当前交付只请求M1独立代码／证据复核，不请求部署或接入在线席位。
