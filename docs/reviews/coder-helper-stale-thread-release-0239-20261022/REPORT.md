# 0.2.3.9 版本位准备

按组长令，基已审补证57784f11，在独立 `review/coder/helper-stale-thread-release-0239` 分支准备版本位，不推任何 integration 分支、不部署、不生成或上传升级包。

同步根 VERSION、helper __version__、Go Version 至0.2.3.9；Go发布精确锁与真实MCP端到端断言同步至新号，共五个非docs文件。运行时锁仍为5ef8d581…，修复产品逻辑、renderer字节均与57784f11相同。`helper_min_version=0.2.3.8` 是解释能力下限，保持原语义。

定向检查：Go1.22.2、GOTOOLCHAIN=local，`go test ./internal/version ./internal/m8 -run 'TestHelper0239ReleasePins|TestVersion|TestHelperVersion|TestProjectContextAcrossRealHTTPHelperIPCMCP' -count=1 -v` rc0，5格通过、0跳过。端到端格通过实际锁定venv运行本地helper IPC与MCP stdio，使用本地假Hub，不触碰生产。

锁定venv、隔离HOME、最小环境下 `python -m unittest test_codex_stale_thread -v` rc0，23格通过。完整原始stdout／stderr／rc在raw，检查摘要与摘要清单同目录。此次仅版本位与现有断言同步，完整全仓及发布门由组长集成时跑；此前冻结源码manifest仍验证其原受审锚，不将它用于声称本版本号改动未发生。

已知限域S-HELPER-DAEMON-IDENTITY保持后续待派工，不在此版本位提交中实现。Hub、renderer、四席helper实际升级范围与时序等待组长及业主最终批准。
