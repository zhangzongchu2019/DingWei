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
