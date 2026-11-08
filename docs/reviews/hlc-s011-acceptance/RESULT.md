# S011 隔离验收结果

## 执行边界与依据

- 执行基线：opus50tc2k/r55-integration，commit 7b123ca4a51fdde77a417d3ce7f03d01235d23ca。本分支只调整 S011 测试夹具、矩阵 runner、该变更 tasks.md 与本验收证据；未改产品实现。
- 规格依据：openspec/changes/hub-graceful-shutdown/design.md 的 HLC-008/S011 与「S011 现有显式重同步协议补充裁定」；停机至新 ready 上限 20 秒，ready 后 helper/SSE/mux/WS 上限 5 秒。显式 resync 的 mux 计时从用户操作开始，并另记 stop 至输出；自动 mux 恢复格保留 gap/缺失读数，不自动执行 reset。组长裁定 3f698b77d848 明确这是既定设计。
- 运行身份与隔离：普通 dev 用户；每次最终执行使用权限 0700 的私有 /tmp/hs11final 或 /tmp/hs11mutfinal，env -i 清除继承环境（包括 SH_*、DINGWEI_*）；仅使用隔离 Hub、临时 SQLite/内嵌 PG、内核分配的 loopback 端口及假 helper，不启动 sessionhelper，不访问在线席位、预发或现网。
- 执行中只调整测试夹具的读数与断言：分别记录20个假 helper 的 ready 后恢复时长；将 SSE、普通 WS 的首个可验证事件恢复纳入 5 秒门；显式格只接受点击后到达的目标 delivery；停机至 ready 使用严格 20 秒上限。

## 正向矩阵

命令：

    mkdir -p /tmp/hs11final && chmod 700 /tmp/hs11final && umask 077
    env -i PATH=/usr/local/bin:/usr/bin:/bin HOME=/tmp TMPDIR=/tmp/hs11final LANG=C.UTF-8 TZ=UTC python3 tests/hlc/process_matrix.py --case S011 --output docs/reviews/hlc-s011-acceptance/raw/positive-final

结果：runner/Go 测试 rc=0，执行时间 2026-11-08T17:15:45.131525Z 至 17:16:01.331599Z。最终测试源 SHA256 与提交内容对应：cmd/workpulse/shutdown_load_test.go d0e8df3d5c96db61ff752688a77125148bd6b115643d2e442417c8f128d86811；tests/hlc/process_matrix.py 44e5114b2cd3d10ea5d155458cc3b87ec47eda94cebbfc77f7b6029ef22c9f00。逐项原始 JSON：

| 判据 | 实得 | 原始 JSON |
| --- | --- | --- |
| 旧 Hub stop 至新 Hub ready ≤20s | 2217 ms | [S011-recovery.json](raw/positive-final/S011-recovery.json) (stop_to_ready_ms) |
| 20 个假 helper，各自自 ready 后 ≤5s | 20/20 恢复；各项 1–37 ms，最慢 37 ms | 同上 (helper_recovery[], helpers_from_ready_ms) |
| SSE ready 后首个可验证事件 ≤5s | 49 ms；gap=1，消息缺失=0，重复游标/消息均=0 | 同上 (sse_from_ready_ms, sse_gap_count, sse_missing, sse_duplicate_*) |
| 普通 WS ready 后首个可验证事件 ≤5s | 49 ms | 同上 (ws_from_ready_ms) |
| mux 自动格（不触发 reset） | gap=1；gap 自 stop 后 2270 ms 观测；缺失=2；重复消息=0 | 同上 (mux_gap_count, mux_gap_from_stop_ms, mux_missing, mux_duplicate_messages) |
| mux 显式 resync，从操作至目标输出 ≤5s | 247 ms；stop 至输出 7469 ms；缺失从 2 降至 0；重复=0；hlc-after-00 在点击后到达 | [S011-explicit-resync.json](raw/positive-final/S011-explicit-resync.json) |

自动 mux 格的 gap 与两条缺失被保留为实际观察；其结果不冒充自动恢复通过。显式格依据冻结裁定按操作到输出计时，两个此前缺失的测试消息均各补回一次，目标消息在点击后输出。

## 反向变异

变异 no-reset-scope-binding 在测试 overlay 中把显式 resync 的 reset scope iframe_instance 改成 stale，使 reset_scope 不再绑定当前新 iframe；没有改交付源码。命令：

    mkdir -p /tmp/hs11mutfinal && chmod 700 /tmp/hs11mutfinal && umask 077
    env -i PATH=/usr/local/bin:/usr/bin:/bin HOME=/tmp TMPDIR=/tmp/hs11mutfinal LANG=C.UTF-8 TZ=UTC python3 tests/hlc/process_matrix.py --case S011 --mutant no-reset-scope-binding --output docs/reviews/hlc-s011-acceptance/raw/mutation-final

Go 测试 rc=1，runner 将其正确分类为目标变异红（runner rc=0）：target_red=true、断言 S011_EXPLICIT_RESYNC_MISSING_OR_TIMING；显式输出缺失=2、重复=0，reset 返回 HTTP 400 invalid_control。原始回执、显式 resync JSON、日志和 overlay 资源记录在 [raw/mutation-final/](raw/mutation-final/)。红点是显式格缺失/时限断言，不是编译失败或无关测试失败。

raw 中还保留了正向重跑前的初始尝试和两次无效变异尝试，均不作为验收读数；正式读数只认 positive-final 与 mutation-final。

## OpenSpec 任务状态

- T-10 已勾选：本地隔离 S011 数值矩阵与反向变异完成，证据即本目录。
- T-11 保持未勾：本次独立验收不包含完整 HLC 总门及预发/生产 GO。

## 仓库门

- `bash scripts/run-build-tests.sh`：rc=0；末行为 `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`。命令原始输出：`raw/build/run-build-tests.log`。执行使用普通 dev、环境白名单、锁定 helper venv、私有 HOME/TMPDIR。
- `openspec validate --all --strict`：OpenSpec 1.16.0、Node 22.23.1，rc=0（25 项通过）；原始输出：`raw/openspec-strict.log`。
- 本目录 `raw/` 下的原始文件校验和见 `SHA256SUMS`。
