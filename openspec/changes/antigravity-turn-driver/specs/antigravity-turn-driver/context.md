# Antigravity 回合驱动上下文

实现位置：`tools/sessionhelper/sessionhelper/antigravityDriver.py` 与 `driver_adapter.py`。测试：`tools/sessionhelper/tests/test_antigravity_driver.py`。部署席位于 `43.172.73.6` 的 `gemini-desktop` 容器，现有 helper 仍为旧 CLI 模式；真实部署需要切换 `SH_MODE=driver`、`SH_DRIVER=antigravity`，并保留席位身份及 MCP socket 配置。源代码只通过 Git 交换，发布包由不可变提交构造。
