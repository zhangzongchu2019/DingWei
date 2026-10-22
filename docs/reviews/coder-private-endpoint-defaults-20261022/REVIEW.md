# PCE 一期默认 legacy 补格

按组长对 5c7110a8 的窄 REWORK 补测试，源提交 `6d6719aeda69c7bf91edbf63310646f37b68e38a`，tree `7e008f2c3f1b82548b228660482c837683c012d4`。相对父候选唯一非 docs 变更为 `tools/sessionhelper/tests/test_private_endpoint.py`；产品、依赖锁、原 1133 项证据均未改。

两新增格明确清空进程环境，不设 SH_CODEX_ENDPOINT_MODE：一格检查 Config dataclass 与 load_config() 均为 legacy，另一格构造不传 endpoint_mode 的 CodexProtocolDriver，断言 legacy 且未构造私有端点。构造使用私有临时 ledger；不启动 daemon、不连在线席位。

实际锁定 venv、隔离 HOME、独立 helper 副本运行该模块：44 项 OK。三刀分别将 dataclass、load_config 与 driver 默认值改成 private，均在对应断言红，恢复后绿，原始 stdout/stderr/rc 与 UTC 在本目录。指定标记：CONFIG_DATACLASS_DEFAULT_PRIVATE、CONFIG_UNSET_ENDPOINT_DEFAULT_PRIVATE、DRIVER_UNSET_ENDPOINT_DEFAULT_PRIVATE。撤修只发生在临时副本。

本后继是 test-only；没有重新运行全仓 Go／helper 全套，原冻结源的完整门限域沿用。VERIFY.py 对新输入核 1893 项 Git blob（仅测试项更新），核原 1133 项与本目录证据 SHA；原一期 VERIFY.py 自身仍绑定原冻结源，须在其原锚运行，不能把新测试误当旧输入。

补充非阻断限域：0.161.0／0.163.0 真实格均由 owner 自持的原生 Popen 子本身直接 accept，同 FD SO_PEERCRED 的 PID 与该子进程一致；未猜包装器后代。别名先于目标出现仍 fail-closed，本次不增加重试或改产品。单跑该模块须 PYTHONPATH 同时包含 tools/sessionhelper 与 tools/sessionhelper/tests；RUN.py 已显式设置，canonical 接线不变。

不部署；默认 legacy 保持。S-PCE-UPDATER-RETAIN 仍为任何席位启用 private 前必须闭合的前置，人工 lease 与 owner 失联恢复单仍属二期。提交组长与 prodops 交叉核。
