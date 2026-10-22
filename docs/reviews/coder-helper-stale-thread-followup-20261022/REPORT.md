# S-HELPER-STALE-THREAD-FIX 上线前补证 A／B

结论：A 两版协议实测通过，匹配现有严格分类；B 已补设计，现实现不证明 OS daemon 身份。仅追加 docs／证据，已审产品源不变，不授权上线。

前序代码 PASS 候选 `13af901d62e83642e840968028fc877f8e42d6d6`；冻结源 `70358a01e2002ff05e001ce5ff702c238c80f984`，tree `108467778b59d5ba6eec23872cdee93b2a1851d1`。本文与设计新增节共同回答组长补证要求。

## A：官方发行包与实测

独立从 HTTPS 官方 npm 命名空间 `@openai/codex` 的精确平台版本元数据取得 tarball，package.json 中 repository 为 `github.com/openai/codex`，版本与 linux/x64 平台匹配。下载字节核 metadata 的 SHA-1 shasum、SHA-512 integrity，同时计算归档与单独提取二进制的 SHA-256。调用绝对路径，不用 PATH／现装 Codex。元数据原件、完整来源 URL、包内成员路径与大小见各版 `raw/*/metadata.json`、`PROVENANCE.json`。registry 签名与 attestation 地址已记录，未另做签名／SLSA 证明验证，不夸称签名验真。

| 版本 | 归档字节数／SHA-256 | 二进制 SHA-256 |
| --- | --- | --- |
| 0.161.0 | 164668500 / `d50e02c2755be551b576527b2b13c6b6a4af3340149afdf792d5491e5d899978` | `9a820c17865fa825d04db416818679a9d63bd72e50835c396f496e5684626c9c` |
| 0.163.0 | 162055867 / `30efc97738b975940d78b59a9bea115fa34040b732807d54ec35ae6b7528de94` | `399ff69c431c066e40de318fba45c2b07f45a76b19939d247ee4828fc70c4215` |

两版均于 2026-10-22T01:41Z 各一次运行，外层 rc0、`LOCAL_REAL_PROTOCOL_PASS`。沿用先前 0.159.2 的协议步骤，docs 内复现脚本只替换发行版摘要／版本参数和显式冻结源导入路径，加实际 classify 断言；已审 `tests/helper-stale-thread/real_protocol.py` 不改。完整命令与环境见 ENVIRONMENT.json。

每版四个私有 stdio app-server 子进程，独立 HOME／CODEX_HOME、最小环境，无认证文件、不继承席位凭据、不调用 daemon start、不连生产 helper／Hub／socket。测试模型 provider 指向拒绝连接的 127.0.0.1:9，仅 turn/start 写出本地 rollout；不以模型完成为门、不发送到真实模型服务。未将下载包、二进制、测试 HOME 或 SQLite 账本入 Git。临时目录下 PATH alias 创建警告原样保存，两版均继续成功；未为消除警告修改环境或产品。

| 检查 | 0.161.0 | 0.163.0 |
| --- | --- | --- |
| 重启私有进程，resume 持久化的原 ID，read 同 ID 且 idle | PASS | PASS |
| 丢失的临时 thread 与不存在 UUID，各 read／resume／turn/start | 6 条 -32600 拒绝 | 6 条 -32600 拒绝 |
| 调用冻结源 JsonRpcError.classify（非复制分类实现） | 6／6 PASS | 6／6 PASS |
| 真实 helper driver 恢复原 ID；先 resume 再 turn/start、无 thread/start | PASS | PASS |
| 真实 helper driver 对丢失 ID 返回 session_unavailable／missing_thread，无 turn/start／thread/start | PASS | PASS |

三版已测的错误模板完全一致，T 表示本次请求的准确 thread ID：

| 方法 | code | message | classify |
| --- | --- | --- | --- |
| thread/read | -32600 | `thread not loaded: T` | thread_not_loaded |
| thread/resume | -32600 | `no rollout found for thread id T` | missing_thread |
| turn/start | -32600 | `thread not found: T` | missing_thread |

逐条原始请求／响应在 transcript.json，独立分类在 classifications.json，真实 driver 方法调用顺序与回执在 driver-results.json。不存在文本差异，不需要改分类。未知文本仍落 rpc_error；若发生于 turn/start，既有实现保留不确定账本并报 model_admission_unknown，不猜 missing_thread。此补证不扩大到其他版本／错误码／daemon_ws 的进程身份。

## B：同机两个 app-server

设计 `docs/designs/S-HELPER-STALE-THREAD-FIX.md` 新增“daemon 身份与线程绑定的边界”。源码依据为冻结源 codexDriver.py 的 `UnixWebSocketJsonRpc.start`、`_start_locked`、`_start_daemon`、`_daemon_socket_path`、`_initialize` 与 `_validate_bound_thread`。

实际连接依据是 HOME 展开的固定 Unix socket 路径，不是 PID 列表。握手、initialize 不核服务端 PID／二进制／boot ID；本地连接代次不等于 daemon epoch。helper 无法确认 PID219 与485中哪一个是预期 daemon，也没有“身份无法确认即拒绝”的实现门。若非预期进程仍能恢复同 ID／cwd，逻辑绑定检查会通过。这一限制明确保留，不以线程恢复冒充进程身份认证。

缺失 thread、cwd／状态／输入能力不符时失败关闭，不自动新建、不重投；连接／准入不确定按现有分类与账本处理。上线路径的只读端点／PID 关联检查属于运维建议，未伪称已产品化，未读取或操作 oaagent 现场。两个 PID 本身不足以判根因；本地 stdio 实测不覆盖生产双 daemon。

## 输入完整性与验证限域

本补证只新增 docs 文件并追加设计节，前序 source manifest 的 1875 个非 docs 文件逐字保持；前序 VERIFY 继续可跑。新 `VERIFY.py` 从 Git 核本补证 manifest、docs-only 差集、冻结源逐字一致，并运行前序 VERIFY。使用 `python3 docs/reviews/coder-helper-stale-thread-followup-20261022/VERIFY.py HEAD .`。

本次不重跑全仓 Go／helper 全套／canonical：这些执行输入未变，沿用13af901d已审证据；新执行读数仅两版真实协议与真实 driver 定向补证。不把协议 PASS 表述为部署许可、席位恢复成功或模型执行成功。
