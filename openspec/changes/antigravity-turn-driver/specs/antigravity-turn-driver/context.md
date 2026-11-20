# Antigravity 回合驱动上下文

实现位置：`tools/sessionhelper/sessionhelper/antigravityDriver.py` 与 `driver_adapter.py`。测试：`tools/sessionhelper/tests/test_antigravity_driver.py`。部署席位于 `43.172.73.6` 的 `gemini-desktop` 容器，测试部署使用 Git 源码坐标 `4aeca81dd22320c3afd0ece194fd229421a17a1e`，启动配置需同时设置 `SH_MODE=driver`、`SH_DRIVER=antigravity`、`SH_EMIT_SESSION_EVENT=1`，并保留席位身份及 MCP socket 配置。正式发布包由不可变提交构造；部署启动脚本备份在宿主机 `/usr/local/sbin/gemini-desktop-sessionhelper.bak-20261120-acp`。
