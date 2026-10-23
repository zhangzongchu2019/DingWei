# r39 0.2.3.12 版本位交审

指定基线 `integration/spec5ff-r39-wip@20cdb2e5095c4a5217fb724e3b971a484ba5fb41`，tree `ec24422d071f5a373d89d874d1ea385d8a26298a`。执行源 `ee494bf7ab978e8991f61a2b26a4aa98284df83c`，tree `bf43a8d68d1e9f031cf1f320f2987848acaee270`，直父即指定基线。仓库 `git.code.tencent.com:wego-ai/dingwei`，分支 `review/coder/r39-version-02312`；后续证据 tip 只增加本目录，以交件消息的完整坐标为准。

## 改动与能力下限

与参照 r37 `01f87ac2b457d3e2386eba1c699ae351496c4ece`、r38 执行源 `a84c1a08e02a042b04a0f64e074c009ae8bb5ab5`（定稿证据 `30463142`）的路径集合完全一致，仅 7 文件、12 行替换：

- `VERSION`、`internal/version/version.go`、`tools/sessionhelper/sessionhelper/__init__.py`：三处同号 `0.2.3.11 → 0.2.3.12`。
- `internal/version/helper_0233_test.go`：`TestHelper02312ReleasePins` 钉死 r39 的根／Go／helper 版本，前版文案为 `0.2.3.11`，依赖锁 SHA 断言不变。
- `tools/sessionhelper/tests/test_legacy_mcp_entry.py`：源包 manifest、校验回执及升级夹具版本同步。
- `tests/pilot-project-context/local_context_roundtrip.py`：真实本地 HTTP → helper IPC → MCP stdio initialize 的版本期望同步。
- `view-service/console-helper-update-regression.js`：迟到快照与关闭重开回归夹具、断言同步。

只提升兼容 D 位，没有新的协议要求，因此所有能力下限保持原样。AMR 结构化失败默认下限 `0.1.3.0`、不展示通知默认下限 `0.2.1.100` 均与 Hub 本身版本无关；INBOX-LAG A 保持按 schema／available 样本、当前连接与新鲜度判断，缺失能力／未知仍不会当成新鲜零。没有抬高最低 helper 版本或改变权限。历史单调测试的 `lastReleased=0.2.3.2` 按前两次做法保留；新的独立发布钉值防止三处一起退回旧版仍被接受。

`requirements-runtime.lock` 字节未动，SHA256 仍为 `5ef8d5815239c3e165ae6ede14559fb545cce4f59cc144f49e20d8162453d2a0`。发布号钉值同步不重新解析或升级运行依赖。除上述 7 文件外，基线源码全部不变；能力、锁和 canonical 清单的 SHA 见 [SOURCE-BINDING](raw/SOURCE-BINDING.json)。非文档搜索仅在“前一发布号”说明处保留 `0.2.3.11`，没有遗留的当前发布版本行。

## 冻结执行源验证

全部命令使用独立 HOME、短 ext4 TMPDIR `/data/ai-dev-data/t`、白名单环境、锁定 venv `/tmp/coder-0232-locked-venv`；Go 门使用 `HUB_TEST_PG_STRICT=1 / SKIP_OK=0 / EMBEDDED=1`。Go 1.22.2，Python 3.12.3，依赖 websockets 15.0.1、pexpect 4.9.0、ptyprocess 0.7.0、certifi 2026.7.22 与锁一致；[PROVENANCE](raw/PROVENANCE.json)。命令、起止 UTC、rc、冻结 source commit 与 7 文件／锁／清单 SHA 均在每门 JSON；[门测执行器](raw/run_gate.py)。

| 门 | 结果 | 原始证据 |
| --- | --- | --- |
| 版本包 | 4 格 PASS，rc0 | [version-packages](raw/version-packages.json) |
| Console／真实 MCP 版本链路 | 11 个顶层格 PASS（另 4 子格），rc0；真实 initialize 版本断言已执行 | [console-mcp-version](raw/console-mcp-version.json) |
| Chromium helper-update 页面 | HTTP 全拦截，14 格 PASS，rc0 | [browser-version-pin](raw/browser-version-pin.json) |
| canonical 原清单 | `BUILD_TESTS_OK python=66 shell=2`，rc0 | [canonical](raw/canonical.json) |
| helper 全套 | 982 项 OK，rc0 | [helper-full](raw/helper-full.json) |
| 全仓 Go，包串行且禁用测试缓存 | 53 个测试包、13 个无测试包，rc0 | [go-all](raw/go-all.json) |
| vet | rc0 | [vet](raw/vet.json) |

Console 定向正则保留前次做法 `Version|TestProjectContextAcrossRealHTTPHelperIPCMCP`，会额外选中历史 r14 的 `TestHelper0232MixedVersionEndToEnd`；该格按原有条件跳过，原因是缺少要求的不可变旧发布坐标 `explicit immutable release pins required`。它要求旧 Hub `0.2.3.1`，不属于本轮同版本构建门，没有修改它或计作 PASS。真实本地 MCP 版本测试已执行 PASS。helper 测试输出中的预期负例 traceback／ResourceWarning 原样保留，不删除或屏蔽。

## 钉值撤修红绿

五组变异仅在独立 detached worktree 执行，门测源码不变。每组依次运行完整版本包，记录原／变异／恢复源码 SHA、命令、隔离环境、UTC 与原始 stdout/stderr；全部 `0→1→0`，每组 `TestHelper02312ReleasePins` 都红，原 SHA＝恢复 SHA＝当前冻结源码 SHA。[汇总](raw/mutations/SUMMARY.json)、[执行器](raw/run_mutations.py)。

| 刀 | 变异 | 证明 |
| --- | --- | --- |
| M1 | 仅根 VERSION 退回 0.2.3.11 | 同号检查与发布钉值拒绝 |
| M2 | 仅 Go 常量退回 0.2.3.11 | 根／Go／helper 一致性与发布钉值拒绝 |
| M3 | 仅 helper 退回 0.2.3.11 | helper 同号与发布钉值拒绝 |
| M4 | 根／Go／helper 一起退回 0.2.3.11 | 普通同号检查仍绿，独立 r39 发布钉值红 |
| M5 | 给依赖锁追加注释形成字节漂移 | 原有依赖锁 SHA 钉值红 |

[证据核验脚本](raw/verify_evidence.py) 核过全部七门、参照路径集合、能力文件未变及五刀原／恢复／当前 SHA；[VERIFY](VERIFY.json) 为 PASS。

没有更改依赖、Hub 迁移、默认行为、部署配置或产品逻辑。没有运行 `build.sh`、构建发行制品、安装、部署、迁移、重启或升级在线 helper。此交件交组长审、prodops 独立交叉，不是上线 GO。
