# S-PG-LEDGER-DRIFT：PG 账本只读核与登记修正

## 核对范围与来源

执行线程 01a1c419-c53c-76b3-b125-bb1fc86f6891；基线 `main 3b6157859f5a329026bfcf43bf95de81909919f8`；候选分支 `coder56solzzc/pg-ledger-drift`。需求：组长 S-PG-LEDGER-DRIFT 派单，依据 rulings ㊿-补40 的编号治理；本单不连接现网，不部署，不执行现网 DDL。

以下列表来自基线 `internal/store/migrate/migrations/*.sql`（`migrate.go` 的 embed／Ledger 入口），不是 SQLite 的独立账本 `internal/store/migrations`。域按 `domains.go` 的 DomainOf／domainRanges 读取；施加目的库按 HubDomains／FanoutDomains 与 `cmd/workpulse/newplat.go`、`cmd/feishu-fanout/main.go` 的装配入口核对。本单不修改号段、任何 SQL 或施加逻辑。

## 基线 PG 全账本（15 条）

| 号 | 文件名（schema_migrations.version） | 现行域 | 生产装配施加到哪个库 | r56.5 既有现网证据 |
| --- | --- | --- | --- | --- |
| 0001 | `0001_core_baseline.sql` | core | Hub PG（含 bootstrap-admin） | 已含（r56.5 既有收据＋集合摘要） |
| 0100 | `0100_identity_baseline.sql` | identity | Hub PG（含 bootstrap-admin） | 已含（r56.5 既有收据＋集合摘要） |
| 0101 | `0101_group_communication.sql` | identity | Hub PG（含 bootstrap-admin） | 已含（r56.5 既有收据＋集合摘要） |
| 0102 | `0102_device_claim.sql` | identity | Hub PG（含 bootstrap-admin） | 已含（r56.5 既有收据＋集合摘要） |
| 0103 | `0103_agent_last_source_r31.sql` | identity | Hub PG（含 bootstrap-admin） | 已含（r56.5 既有收据＋集合摘要） |
| 0104 | `0104_np_session_created_at.sql` | identity | Hub PG（含 bootstrap-admin） | 已含（r56.5 既有收据＋集合摘要） |
| 0105 | `0105_extseat.sql` | identity | Hub PG（含 bootstrap-admin） | 已含（r56.5 既有收据＋集合摘要） |
| 0106 | `0106_tempgroup.sql` | identity | Hub PG（含 bootstrap-admin） | 已含（r56.5 既有收据＋集合摘要） |
| 0107 | `0107_mcpgw_oauth.sql` | identity | Hub PG（含 bootstrap-admin） | 已含（r56.5 既有收据＋集合摘要） |
| 0110 | `0110_extseat_expect_reply.sql` | identity | Hub PG（含 bootstrap-admin） | 已含（r56.5 既有收据＋集合摘要） |
| 0111 | `0111_extseat_feishu_copy.sql` | identity | Hub PG（含 bootstrap-admin） | 已含（r56.5 既有收据＋集合摘要） |
| 0200 | `0200_hosted_r30_baseline.sql` | hosted | Hub PG（含 bootstrap-admin） | 已含（r56.5 既有收据＋集合摘要） |
| 0201 | `0201_hosted_node_access_r31.sql` | hosted | Hub PG（含 bootstrap-admin） | 已含（r56.5 既有收据＋集合摘要） |
| 0300 | `0300_console_transport_slot.sql` | view | Hub PG（含 bootstrap-admin） | 已含（r56.5 既有收据＋集合摘要） |
| 0400 | `0400_fanout_baseline.sql` | fanout | 独立 fanout PG | 本件不核 fanout 库 |

Hub 域子集14条（core 1、identity 10、hosted 2、view 1），fanout 独立库1条；cross 当前无条目。全账本15条只是盘点／双轨测试集合，不能当成生产入口给任意一个库全量施加。现有生产迁移仍须显式选择域；fanout 的0400不进入 Hub PG。

## r56.5 PG14：0200／0201／0300 均已含

只从 Git 取既有结项件，未向现网发请求。证据仓库均为 `git@git.code.tencent.com:wego-ai/dingwei.git`：

1. `review/prodops/r565-prod-closeout-20261110`，精确提交 `528f88332cd3c206fbda4556b59c8d16ff19ee7b`，`docs/reviews/prodops-r565-prod-closeout-20261110/raw/1808-final-state.txt`：2026-11-10T18:55:16Z，`pg_migrations=14`；`pg_0111=0111_extseat_feishu_copy.sql|c51dcc928eadcb35b83d13d36b5d17423501def99673ee3f34f46e6c95e81196`。RESULT.md 的1808Z结项节与此一致。
2. 同提交、同目录 `raw/switch-receipt-1808.jsonl`：2026-11-10T18:14:48.085822Z 的 `pg_old_migration_baseline` 记录 rows=14，`ledger_sha256=b64c6b4360be50c78d131306fcce9a192be2a9943dd701d83ba6916b10194209`；18:19:53.249400Z 终态 SWITCH_PASS。该摘要覆盖按 version 排序的完整 `(version,digest)` 集合，不只是计数。
3. `review/prodops/r565-prep-20261110`，精确提交 `924786716e7a1185afa0d7999f24a6369aac3e8e`，`docs/reviews/prodops-r565-prep-20261110/r56-prod-hub-switch.py`，文件 SHA256 `77b9f830c29d01d44403fa8602af5584d620236265458aebc10f39e855513549`，与收据依赖钉值一致。PG_BASELINE_ROWS 包含0200、0201、0300及0111；开场先比对完整账本等于该常量，再记上述摘要。
4. rulings 分支 `review/opus50tc2k/rulings` 在本次读取时的精确提交 `f2263ddedf6fa43d6fba4e88d837864d5479cb6c`，`docs/rulings/S-TEAM-MIGRATE-HOSTED-20261017.md` ㊿-补75／补76／补77：PG14无新迁移重试、现网结项及 main 3b615785清账。

本地静态复算：对上述基线的14个 Hub SQL文件取原始字节 SHA256，构造 `sorted((文件名, SQL字节SHA256))`，以 Python `json.dumps(rows).encode()` 取 SHA256，得到同一个 `b64c6b4360be50c78d131306fcce9a192be2a9943dd701d83ba6916b10194209`。因此不是从“14行”猜测0200／0201／0300存在，而是核对整份已施加文件名／digest集合与基线一致。此结论针对结项时点，不声称本单新增现网采样，也不校验各行 applied_at。

## 在途占用（不等于基线已有条目）

| 号 | 所属任务／分支 | 状态与证据 | 本单处理 |
| --- | --- | --- | --- |
| PG0108 | ZERO-CONSENT；`coder56solzzc/mcp-zero-consent`；`0108_extseat_short_name.sql` | ㊿-补40：c1164243已冻结未发布；㊿-补65仍登记冻结 | 保留占号，不加回main、不前移或复用 |
| PG0109 | S-EAI-P1-S1（乙P1） | ㊿-补40分配，补65记暂停；关联SQLite0077另属SQLite账本 | 保留占号，S1继续暂停，不新增SQL |

两号均不在本基线的15个SQL文件中，也不在r56.5已核14个Hub条目的摘要集合中。0110与0111后来已上线，不表示0108／0109可被回收。编号只由组长分配；不要为补齐数字顺序重命名或改写已应用条目。

## 登记与文案修正

- `domains.go`：以 hosted 的0200／0201及 view 的0300已有条目替代“还没有条目／前置”旧注释，说明三者均进Hub库。
- `ledger_test.go`：0300文案称“view域 console transport slot”，不再称“mux域”；Hub十四条、全域双轨40表及漏掉fanout五表的注释按现行断言对齐。仅改注释／失败提示文字，断言、测试流程和DDL均不变。

## 待组长确认：0300的语义域

现行证据：0300位于0300～0399的view号段，SQL只建 `console_transport_slot`；源DDL在 `internal/consolemux`，保存Console传输占位／代际，实际使用Hub PG。`consolemux`是组件名，现行注册表没有独立“mux域”。这些事实未显示必须改号段的冲突；本单仅消除把组件名误作账本域名的文案。

- 选项A（建议）：确认该Console传输基础设施继续归view域，沿用0300与Hub装配；仅采用本单登记和文案修正。
- 选项B：若组长认为传输租约应单独成域，另派设计明确新域号段、装配与兼容边界；本单不执行。已应用0300文件、编号、digest不得因语义整理而改写或补跑。

本单未修改0300域归属、domainRanges、HubDomains、FanoutDomains或施加顺序；上述建议不是组长裁定。

## 验证

使用普通用户dev、go1.22.2；以 `env -i` 构造干净环境，PATH前置 `/tmp/coder-0232-locked-venv/bin`，随后 `/usr/bin:/bin`；独立HOME，短TMPDIR `/tmp/pgl-c419/t`，`HUB_TEST_PG_STRICT=1`。显式复用本机Go模块／编译缓存，未提供外部PG DSN，测试使用pgtest隔离PG，不访问现网。

命令：`go test -json -count=1 -p 2 -timeout 40m ./internal/store ./internal/store/migrate`。退出码0；store 599 PASS／0 FAIL／5 SKIP，migrate 42 PASS／0 FAIL／0 SKIP（含子用例）。`TestLedgerShapeMatchesDomainDDL`、`TestMigrate_RealLedgerSatisfiesIdentityCheckSchema`、`TestMigrate_DomainScopingOnRealLedger`均PASS；store同时覆盖SQLite账本及兼容轨，未新增测试。

store的SKIP逐项如下；migrate为0 SKIP，未因PG环境缺失而跳过：

| 用例 | 跳过条件 |
| --- | --- |
| TestConsoleTaskLegacyDODMigrationPendingProductionCount | K17仅覆盖新写；旧acceptance_json的DOD行等待现网计数后再决定0049迁移 |
| TestConsoleTaskStartRequiresAcceptanceLinkPendingStartGateRuling | future K7的start gate约束尚未冻结，creation仍允许零links |
| TestFeishuStickyOldHubMigrationRollbackCompatibility | 未提供R14_OLD_HUB_BINARY，精确r48夹具由R14证据门负责 |
| TestR10OldR48HubUpgradeAndRollbackOnIsolatedCopy | 未提供精确r48旧Hub二进制，隔离R10兼容证据门负责 |
| TestR10RestartChildAuthority | 仅为子进程probe，由TestR10SeparateProcessRestartPreservesAuthorityAndUnknownNotice执行 |

`sh scripts/run-build-tests.sh`：同样使用上述env -i环境，退出码0；Claude／Codex两个profile各1132/1132、rc0（92个helper文件）；最终原文 `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`。此构建门无SKIP。`git diff --check`：退出码0。提交前重新fetch main，规格相对派单基线无变化。

原始本地日志 `/tmp/pgl-c419/store-migrate.jsonl`、`/tmp/pgl-c419/canonical.log`；这些是本席测试证据路径，不作为他席可访问的共享地址。业务源码与登记修正只通过上述Git分支交件。

本地原始证据SHA256（不含凭据）：

- `store-migrate.jsonl`：`8c9d07e70578886802c909a8159e83bf10d5cd4488ee13edf192417703a7240a`。
- `canonical.log`：`4fad8a25e2b2cfe48e30c8b4bc04c2692d67af912b2317811464ad948c099e8e`。
