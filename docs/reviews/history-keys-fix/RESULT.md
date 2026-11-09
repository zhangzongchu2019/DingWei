# S-HISTORY-KEYS-FIX 结果

- 基线：`origin/main` / `d7dc2f04aa675029c381bc29aa27a6e67b20e51a`。
- 目标分支：`togpt60lunazzc/history-keys-fix`。
- 修复：新平台 history 读取通过 `ResolveFullName` 取得 Agent，并使用 owner=`AgentKeyID(OwnerLC)`、agent=`Agent.ID`、session=`Agent.NameLC()`；写入键本来已使用同一规范键。旧平台分支未改。
- 存量兼容：按本任务提供的 prodops 只读核对，R15 现网开关未启用且历史库文件不存在，故无历史库存量需迁移；本单未触碰现网开关。
- 测试：`TestNewPlatformSessionHistoryRoundTripByFullName` 使用真实 Hub、登录 cookie、helper WebSocket `session_event` 和临时持久历史库；全名路径读回事件，另一属主、短名及错误全名均为 404。断言标记：`HISTORY_NEWPLAT_ROUNDTRIP`、`HISTORY_NEWPLAT_OWNER_ONLY`。

## 三项键回退变异

`python3 tests/r15-history-keys/mutations.py`：PASS，3/3 语义判红，均命中 `HISTORY_NEWPLAT_ROUNDTRIP`，编译失败不计红：

1. 读取 owner 改回 `ag.OwnerLC`：RED。
2. 读取 session 改回路径原值：RED。
3. 写入 owner 去掉 `u_`：RED。

## 门禁

- `go test -p 1 -count=1 ./internal/m8`：PASS，221.984s。
- `go test -p 1 -count=1 ./internal/store`：PASS，74.916s。
- `NODE_PATH=/tmp/dw-filecard-playwright/node_modules python3 tools/run-browser-regressions.py --receipt /tmp/history-keys-browser-receipt-nodepath.json`：PASS，aggregate rc=0；R15 pagination、R15 browser mutations 均 rc=0。全 runner 38 套均 rc=0。
- `PATH=/tmp/dw-filecard-build-venv/bin:/opt/node22/bin:$PATH ./scripts/run-build-tests.sh`：PASS，`BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`。
- `git diff --check`：PASS。
- 未运行 OpenSpec strict：当前环境没有 `openspec` 可执行文件；本单按要求已在 r15 tasks 追加修复任务并勾选完成。此项不属于本单列出的门禁。

持久化功能保持代码默认关闭；没有操作现网或设置环境开关。

## 吊销／停用清理键追加修复

- 在两个新平台清理入口中，`RequestAgentDeletion` 的 owner 参数统一为 `AgentKeyID(OwnerLC)`：Agent 吊销使用 `AgentKeyID(agent.OwnerLC)`；用户停用逐 Agent 清理使用 `AgentKeyID(ag.OwnerLC)`。
- 全仓生产 `SessionHistory` owner 调用核对：`internal/m8/terminal.go` 的 `Append` 使用已认证 helper 会话的 `c.ownerKey`（新平台握手取 `AgentKeyID(ag.OwnerLC)`）；history `Page` 使用 `ResolveFullName` 得到的 `AgentKeyID(ag.OwnerLC)`。`internal/m8/newplat_shim.go` 的两处 `RequestAgentDeletion` 均使用 `AgentKeyID(OwnerLC)`。未发现其他生产 `SessionHistory` 调用。测试中的 `Page` 调用使用与测试场景对应的规范键或显式旧平台 fixture 键。
- 新增真实 Hub purge E2E：吊销后清除目标 Agent、保留同属主另一 Agent；用户停用后清除该用户全部 Agent、保留其他属主。断言标记：`HISTORY_NEWPLAT_PURGE_ON_REVOKE`、`HISTORY_NEWPLAT_PURGE_ON_USER_DISABLE`。测试按 `CleanupBatch` 每次处理一个身份队列项的契约反复清理。
- `python3 tests/r15-history-keys/mutations.py`：5/5 语义变异判红。新增第 4 刀把吊销 owner 改回 `agent.OwnerLC`，命中 `HISTORY_NEWPLAT_PURGE_ON_REVOKE`；第 5 刀把用户停用 owner 改回 `ag.OwnerLC`，命中 `HISTORY_NEWPLAT_PURGE_ON_USER_DISABLE`。
- 追加门禁：`HUB_TEST_PG_STRICT=1 go test -p 1 -count=1 ./internal/m8` PASS（217.783s）；`go test -p 1 -count=1 ./internal/store` PASS（75.142s）；`PATH=/tmp/dw-filecard-build-venv/bin:/opt/node22/bin:$PATH ./scripts/run-build-tests.sh` PASS，`BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`。
