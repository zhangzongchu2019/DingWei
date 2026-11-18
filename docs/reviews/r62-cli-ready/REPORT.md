# S-R62-CLI-READY

## 交件坐标

- 基点：`togpt60lunazzc/r62-cli-a-fix`，`9b32685721c0aa0370d13ffc320635826031305b`。
- 实现提交：`6345904128e1acc14982fd1cdccb3d04c2fec0d0`；测试合同修订：`5ff24a421`（完整 SHA 以 Git 提交记录为准）。
- 分支：`togpt60lunazzc/r62-cli-ready`。报告与证据在实现、测试提交之后追加；最终 tip/tree 以交件回执中的 Git 读数为准。
- 本任务只改 helper CLI 路径及其测试、夹具和本报告；未改 driver 模式。

## 行为与边界

Codex 的提示符和 PTY 静默只表明界面可见，不能证明 0.170.0 已接收首个回车。现在 Codex transcript 路径先只键入一次正文，再等待与正文匹配的持久化 user 轮。首个回车可在初始化窗口内丢失；适配器只在正文仍留在输入框时再提交回车，不再次键入任务正文。收到对应 transcript user 轮后才触发 `on_injected`／admitted 回执。未观察到该轮时仍以 `cli_injection_not_received`、`retryable=false` 失败，不发送成功回执，也不允许自动重复投递。

同步回复与异步回复都使用该确认边界。Codex rollout 文件可能在首轮被接受前尚未创建，因此轮询期间非阻塞地重新发现 transcript 文件。Claude 的既有 transcript 与异步行为保持原样；driver 路径未触及。

## 回归证据

夹具 `tools/sessionhelper/tests/fixtures/codex-0.170.0-prompt-before-initialize.txt` 取自 qwen 调研分支 `review/qwen38mvb4/cli-a-tui-rootcause-20261118`，tip `b910f980824a8ea510519b116ef59759034be9a5`，日志 `logs/d2-0170-race/ready-2.txt`（原 blob `e26723e796f65f7a150c60dd23e9fcd91460fd4f`）。保留真实提示符形状，并由测试把初始化延后 2.1 秒，模拟 d2 窗口中首个 CR 被丢弃。

常驻格 `SessionHelperTest.test_codex_0170_prompt_before_initialize_is_admitted_only_after_one_user_turn` 覆盖同步和异步两种 CLI 回复配置。两格都断言正文仅键入一次、rollout 恰有一条匹配 user 轮、ACK 晚于 transcript 落盘、最终不重复提交回车；同步格另核对回答文本。旧版基线 `9b326857` 回放同格得到 2 个业务断言失败：同步格 ACK 早于 transcript user，异步格没有 user 行（见 `evidence/old-readiness-baseline-red.log`）。修复后目标格及 `cli_injection_not_received` 非重试格通过（见 `evidence/focused-regressions-green.log`）。

此外，更新两个旧测试的期望：当 transcript 从未出现对应 user 轮时，不能只因 PTY 写入成功就断言已注入；持久投递测试断言失败终态且没有 injected ACK。这是新确认边界的合同更新，不放宽失败断言。

## 门

完整 `scripts/run-build-tests.sh` 使用 `env -i`、锁定 venv、Go 1.22.2、独立 HOME/TMP、uid 1001。首次实现门在更新上述两个旧 ACK 期望前运行，helper Codex profile 的 1270 项中仅这两项失败；该轮原始日志保留为 `evidence/run-build-tests-initial-old-ack-expectations.log`，不把它计作最终通过。

最终完整门通过，rc=0：Claude 与 Codex 各 1270 helper suite + 9 supervisor 测试（各 1279/1279），helper inventory 97；Python 27、shell 2，`internal/m8` Go 测试通过。终行为 `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`。命令、工具链及环境见 `evidence/build-gate-final-meta.txt`，原始日志见 `evidence/run-build-tests-final.log`。

