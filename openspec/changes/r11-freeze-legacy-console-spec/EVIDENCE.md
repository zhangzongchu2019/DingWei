# R11 核心阶段验证

范围：d1a7a686110c9721c61f403bb9cac4cae0b694ee 之后、组长批准的 LCS-001/003/004/005。未切换现网、未改历史共享可见范围、未开 R5、未 archive。LCS-002/T-05 与预发 T-08 仍待后续，整个 R11 不宣告完成。

## 场景与证据映射

| 固定场景 | 本轮结果 | 可复跑证据 |
| --- | --- | --- |
| LCS-S001 | PASS | `TestR11FrozenHTTPWritesAndUnchangedReads` 的原 handler 拒写矩阵；`TestR11FreezeStoreRefusalsPreserveHistory/LCS-S001_append` |
| LCS-S002 | PASS | `TestR11FrozenWriteStillRequiresIdentity`：未认证仍 401/403，已认证写 409/legacy_spec_frozen |
| LCS-S003 | 未执行 | 新导航与独立历史查询属于后续 T-05，本轮不改变普通入口/共享可见范围 |
| LCS-S004 | PASS | HTTP workflow/agent-review/criteria/confirm/PM/template 拒写，数据库历史快照未改；不伪造定稿或通过 |
| LCS-S005 | PASS | 24 个修改 handler、WS 迟到 review/criteria/PM/presplit 结果、`TestR11LateResultsAndTaskProgress/LCS-S005_late_review`；触发器覆盖 26 表三类写操作 |
| LCS-S006 | 未执行 | 新历史查询的越权/深链接流程待 T-05；本轮仅验证既有 shared GET 权限及原始内容未变，不把它冒充新功能验收 |
| LCS-S007 | PASS | `TestR11FrozenDispatchAndPersistedQueue`：五种旧生成派发拒绝，已有 durable queue 在 socket write 前终止；reconcile 不再改写/派发 |
| LCS-S008 | PASS | store 全表历史快照对比；HTTP 读取原文/修订原样；现有原页面在冻结后有历史只读标注，无批量转换/删除 |
| LCS-S009 | PASS | `TestR11FreezeProcessLease` 真实子进程竞争；`TestR11FreezeSQLTransactionAtomicity` 独立连接上的 SQL 事务竞争；过期租约不能复用，取消冻结不改变状态 |
| LCS-S010 | PASS | `TestR11LateResultsAndTaskProgress/LCS-S010_inflight`：旧任务 start→mark_case→独立 confirm_case→complete，正文不变；普通消息不受冻结拦截 |

所有已执行对象来自私有临时测试库/进程，无真实用户数据。PG 仅为身份依赖；旧 Spec 业务数据实际在 SQLite，不存在“第二份 PG Spec 表快照”读数。

## 命令与日志

- 定向：`go test ./internal/store ./internal/m8 ./cmd/workpulse -run '^TestR11' -count=1 -v`；含新增 migration 兼容、表清单和原子性用例。
- 全包：`go test -count=1 -json ./internal/m8 ./internal/store ./cmd/workpulse`；m8/store 最终日志 `/tmp/r11-packages-final2.jsonl`。修正 cmd 测试夹具后对 cmd 全包单独复跑，日志 `/tmp/r11-cmd-final.jsonl`。
- 竞态：`go test -race -count=1 ./internal/store ./internal/m8 ./cmd/workpulse -run '^TestR11'`；`/tmp/r11-race-final2.log` rc=0，store/m8/cmd 全部通过。
- OpenSpec：1.16.0 / Node 22.23.1 / OPENSPEC_TELEMETRY=0；`openspec validate --all --strict --no-interactive`，`/tmp/r11-openspec-validation.log` rc=0，4/4、零 issue。
- 完整门：锁定 Python venv 执行 `bash scripts/run-build-tests.sh`，日志 `/tmp/r11-build-tests.log`，rc=0，helper claude/codex 各 1087/1087（1078+9），inventory 90，Python27 组、shell2 组，BUILD_TESTS_OK。

## 负向验证与首轮失败处置

- 在隔离测试库撤去 78 条 `r11_freeze_*` 触发器，原拒写断言报 `frozen write succeeded`，原 SQL 拒写也失败；负向控制 rc=1，日志 `/tmp/r11-db-guard-negative.log`。分支上的迁移未被删改。最初只用 go overlay 替换嵌入 SQL 未影响实际 embed，不能算有效变异，已作废并改用上述真实数据库负向控制。
- 新迁移首次在“旧库丢账再恢复”的既有用例报表已存在；0067 改为可幂等重放的 CREATE IF NOT EXISTS 与 INSERT OR IGNORE，保留锁存状态，相关迁移及冻结重启用例通过。0067 尚未发布，未改任何已部署迁移。
- cmd 空子测试名生成 `#00`，原 SQLite URI 将该字符后的路径当片段，复跑误用旧测试库并撞到本轮旧摘要；仅把该案例命名为 explicit-empty，未改产品 DSN 语义，未删除可能属于其它会话的旧文件。修后 cmd 全包 rc=0，无剩余失败。

## 最终读数

- m8：PASS，777 个顶层测试、405 个子测试，191.028s。
- store：PASS，273 个顶层测试、99 个子测试，47.764s。
- cmd/workpulse：夹具修正后独立全包 PASS，138 个顶层测试、58 个子测试，210.833s。原合并日志中的 cmd 失败为已记录的旧夹具问题，不冒称该旧命令整体 rc=0。
- 竞态：三个包的全部 TestR11 用例 PASS，无 race。
- OpenSpec apply 进度 5/8：T-01/02/03/04/06 已完成，T-05/07/08 保持未完成；核心八格已执行，新查询两格及预发未执行。

日志 SHA256（本地取件路径只作本席恢复；审阅以 Git 锚与复跑为准）：
- `/tmp/r11-packages-final2.jsonl`：`a9bd962f41a4ac54c41c1276a282fffcd16c7a2b3c26b476cce055725449427c`
- `/tmp/r11-cmd-final.jsonl`：`4be0a4e17af00b9392f50e9092a7fe544c15c1d01ee2933c95927046c27e8ff5`
- `/tmp/r11-race-final2.log`：`43a756f1290e2bb5dcb6166e73cabc0c3e4a1376b50165e6ce1bae36b70c8124`
- `/tmp/r11-build-tests.log`：`3b640c0afcd48455a975e4d6fd9bb8b0b2d12f4af43ebde91fe2bd76cafb5013`
- `/tmp/r11-db-guard-negative.log`：`59dd1c2b2aa19d4756a91f3127798c9f96c44431047f8b2cc92cb17a2766d976`
