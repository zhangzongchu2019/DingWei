# R1 · 客户端配置读取收口：改动前后读取点对照

基线 `newplat/main` c55866db。统一入口：`sessionhelper/config.py` 的 `env_get(name, default=None)`
（语义同 `os.environ.get`，每次调用时刻读取，不缓存；import 期读取的常量仍在 import 期调用）。
机械检查：`tests/test_env_read_single_entry.py`（白名单按文件写死"次数＋理由"，次数精确相等）。

计数口径：改动前 `sessionhelper/` 下含 `os.environ` 的共 **31 行**。其中 **20 行**迁到 `env_get`；**11 行**保留直接读取
（白名单）；另新增 env_get 的实现 1 行。改动后机检按 AST 出现次数计共 13 处（app.py 有一行含 2 处读取，
config.py 含入口实现）。

## 一、已迁到 `env_get`（20 行，8 个文件）

| 原位置（基线） | 变量 | 迁后 |
|---|---|---|
| driver_adapter.py:26 | SH_DRIVER | env_get |
| driver_adapter.py:46 | SH_CLI_CWD | env_get |
| driver_adapter.py:53 | OPENCODE_SERVER_PASSWORD | env_get |
| driver_adapter.py:54 | SH_OPENCODE_PORT | env_get |
| driver_adapter.py:56 | SH_OPENCODE_MODEL | env_get |
| driver_adapter.py:61 | SH_CODEX_SANDBOX | env_get |
| driver_adapter.py:62 | SH_CODEX_APPROVAL | env_get |
| driver_adapter.py:78 | SH_SIDECAR_PORT | env_get |
| provision.py:114 | SSL_CERT_FILE | env_get |
| provision.py:210 | SH_PROVISION_NO_EXIT | env_get |
| provision.py:267 | CODEX_HOME | env_get |
| provision.py:355 | CODEX_HOME | env_get |
| app.py:1102 | SH_ONLINE_LIST_PATH | env_get |
| app.py:1297 | SH_EMIT_SESSION_EVENT | env_get |
| cli.py:765 | SH_CLAUDE_PROJECTS_ROOT | env_get |
| cli.py:782 | CODEX_HOME | env_get |
| claudeDriver.py:45 | SH_CLI_BIN（import 期） | env_get（仍 import 期） |
| codexDriver.py:38 | SH_CODEX_BIN（import 期） | env_get（仍 import 期） |
| openCodeDriver.py:48 | SH_OPENCODE_BIN（import 期） | env_get（仍 import 期） |
| sidecar.py:23 | SH_SIDECAR_RENDERER（import 期） | env_get（仍 import 期） |


## 二、保留直接读取（白名单，AST 共 13 处，每项有理由）

| 文件（次数） | 理由 |
|---|---|
| config.py（3） | 统一入口本身：env_get 实现；load_config / detect_full_session_name 的 `env or os.environ` 缺省 |
| claudeDriver.py / codexDriver.py / openCodeDriver.py（各 1） | 把整份环境透传给子进程（dict(os.environ) / .copy()），语义是继承而非读配置值 |
| app.py（3，原 2 行） | 启动时清理代理变量（1 处 pop）及同批变量诊断打印（1 行 2 处读取）；组长确认合规，且避开 tosonnet50ts 在该段的改动 |
| mcp_shim.py（1）、dingwei_file.py（1） | 自包含入口，无包内 import，可能被当脚本直接启动，加相对 import 会改启动语义 |
| send_dingwei.py（2） | 被拷贝成 ~/.dingwei/send.py 单文件独立运行，无法 import 包 |
