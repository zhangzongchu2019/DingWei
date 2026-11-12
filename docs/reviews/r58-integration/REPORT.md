# S-R58-INTEGRATION 验证入口

唯一写者：01a1c782-7090-7071-8d99-6ea51512ac13；锁 /tmp/coder56-r58-integration.lock；新工作树 /data/ai-dev-data/r58-integration-c782。
基线 main 153bac4dfe6794a8f2d9438eb4ac4e76d6aa400d；本分支 coder56solzzc/r58-integration。
五项按派单顺序各一个 no-ff merge，完整坐标见 merges.json；无文本冲突。计数钉值提交 b40bf84b0，详情见 helper-count-pins.json、CONFLICTS.md。

本提交固定最终待验tip；以下各门均在它上执行，期间不再改变集成分支源码或文档。完成后的真实读数由交审回执与独立 evidence/coder56solzzc-r58-integration 证据分支记录，避免为写结果再改变受测tip。门尚未运行完成，不冒称通过。

## 门与证据
- 全仓 go test -json -v -p2 -count1 -timeout40m ./...，原始JSON行保留，另解出-v文本，按Go事件数PASS/FAIL/SKIP并逐项列跳过原因。
- internal/hosted -race -count3 -timeout40m，原始JSON及按轮统计，DATA RACE须零。
- sh scripts/run-build-tests.sh，完整BUILD_TESTS_OK与两profile actual=expected=1181。
- python3 tools/run-browser-regressions.py --receipt，不选格，完整40套及失败数/rc。
- openspec validate --all --strict。
- 第1项撤掉_ensure_thread required分支、第4项撤掉result后_active清空，分别要求指定用例红。均只在一次性副本操作。

执行环境为env -i的显式白名单、普通uid1001、Go1.22.2、Node22、HUB_TEST_PG_STRICT=1、锁定Python venv、独立HOME和短TMPDIR；不使用在线席位配置，不构建发布包、不部署、不改版本位。
原始完整日志 /tmp/r58-c782/；提交小型逐行摘录与SHA256，较大原始日志留本机，不纳Git。
