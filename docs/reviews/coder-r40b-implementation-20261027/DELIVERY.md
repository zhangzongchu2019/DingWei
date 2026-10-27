# r40b 交付证据

最新watchdog修复、旧1019环境说明与复现入口见 [WATCHDOG-QUEUE-CLOSURE.md](WATCHDOG-QUEUE-CLOSURE.md)：原阻断专项及当前严格Go门已过，但完整helper仍有1个临时目录清理错误，未报全门PASS，未上线。下文保留历史受测锚与读数。

本页保留原7ab9460交付的受测代码与读数。后续Claude显式命令闭合及**当前完整Go门rc1、Codex接纳超时未定因**，以 [CLAUDE-ARGV-CLOSURE.md](CLAUDE-ARGV-CLOSURE.md) 为准；不得把本页历史rc0作为当前上线结论。

取证截止：`2026-10-27T06:58:35Z`。受测代码锚 `f0d2c87c6fcc95916a852cb76d0ff19629a277e8`，tree `b2af68adf11dd821f202946745f5eaa131006418`；最后追加的本报告提交只含文档。

范围单 `04e6b6bf423588e71ecddfedbe6eeea4c3ca0942`；④设计 `7af11ba96aac10cbd1d0ce1b78fde819fc1e1f29`；批准计划 `153e366eb233f2078027d54516ff72b0ae0bef05`。本批只实现③④及其验收；⑥未出修法。仓库 `git@git.code.tencent.com:wego-ai/dingwei.git`。基线 r40a `a738d47f303441c410bf09fb0adcb7fe1119f862`，分支 `integration/spec5ff-r40b-wip`，逐笔追加。

## 实现结果

- ③ 提醒明确提示回原通知，不新增提醒的回复入口或回写权限；显式“补充”贯通 Feishu parser、原授权路径、store CHECK 与楼层类型显示，裸说明/更正的类型不重分类。
- ④ Hub 逐件冻结协议、模式和 generation；queued 只表示本地持有，injected 绑定真实 admission，processed 绑定真实原 turn 的终态与 outcome。持久化提交后才发阶段确认，迟到旧阶段不能倒退。
- helper 持久 dispatch intent、原 turn 和时间；ACK outbox 按阶段确认重放。Claude 无远端稳定 ID；Codex 不可证实的 unknown 不创建新 turn，隔离显示“可能已进入会话／暂停自动重投／需核原回合”。不假定无损自动恢复。
- View 真实 queued 30秒提示、原位接纳/终态、legacy/desktop 的诚实文案。旧 helper 重连更换事件纪元后，从持久化原信封重建只读输入观察，避免 legacy 收件状态失去显示锚点；不回放模型输入。
- v2 只在 queued ACK 已持久化之后保护生命周期免于传输重投/过期；未收件项目沿用原传输 TTL。
- 版本位沿用 r40a 的 `0.2.3.14` 三处。没有重启、部署或改在线数据库，没有清理保留的备份/回退件。

## 运行环境与门

实际 Go `go1.22.2 linux/amd64`；实际 Claude `2.1.215`；实际 Codex `0.164.0`。隔离模型 HTTP/SSE，实际安装的原生 CLI 产生 admission/terminal；未用合成 CLI 替代判据1–10。

| 门 | 读数 |
| --- | --- |
| go build ./... | rc0 |
| m8＋store 严格全包 | rc0；顶层 PASS 1038／FAIL 0／SKIP 7；含子测试 PASS 1548／FAIL 0／SKIP 7 |
| helper unittest 全套 | rc0，1019 项，437.257s |
| console-task-board | rc0，assertions=86，floor=86，failed=0；K13 deferred |
| delivery-ack-v2 浏览器投影 | rc0，assertions=42，failed=0 |
| spec-clarification 浏览器 | rc0，passed=24，failed=0；390/820 宽度 |
| run-build-tests | rc0；BUILD_TESTS_OK python=66 shell=2 |

Go 全包命令：`HUB_TEST_PG_STRICT=1 HUB_TEST_PG_SKIP_OK=0 HUB_TEST_PG_EMBEDDED=1 TZ=UTC go test -p 1 -count=1 -v ./internal/m8 ./internal/store`，真实跨运行时 Python 与 ACK E2E 环境变量均显式配置。原始 stdout/stderr 完整落文件。

- `m8`：PASS 779／FAIL 0／SKIP 5，294.472s。
- `store`：PASS 259／FAIL 0／SKIP 2，34.949s。

七项既有/环境跳过（ACK 判据1–10无跳过）：

- `TestMuxProductHTTP1EightSeats`：set MUX_PUPPETEER to installed puppeteer-core for actual Chrome product integration
- `TestConsolePlatformPayloadDump`：仅在浏览器闸取真载荷时运行(设 DW_CONSOLE_PLATFORM_PAYLOAD_OUT)
- `TestConsoleTaskBoardStartedTaskRestrictionsPendingCaseRecordModel`：K13 deferred: task case record model does not exist, so started state cannot be derived mechanically
- `TestHelper0232MixedVersionEndToEnd`：explicit immutable release pins required
- `TestEnvelopeReadTimeoutEnvironmentChild`：subprocess probe only
- `TestConsoleTaskLegacyDODMigrationPendingProductionCount`：K17 covers new writes only; legacy acceptance_json rows containing "DOD" await the production count before any 0049 migration decision
- `TestConsoleTaskStartRequiresAcceptanceLinkPendingStartGateRuling`：future K7: enforce at the start gate using console_spec_task_acceptance, never acceptance_json; creation remains allowed with zero links until that timing is sealed

③ Feishu 的证据是隔离真实 Dispatch→store→receipt 路径及浏览器，不声称已对生产 Feishu 做人工往返。

## 真实 CLI 端到端与去修复负控

执行入口与判据映射见 [REAL-CLI-GATE.md](REAL-CLI-GATE.md)。两 driver 共5组14个叶子用例：长回合三阶段2格、ACK丢失/确认丢失4格、不确定窗4格、取消两向2格、旧helper升级2格。实际 renderer 只读订阅真实 Hub 事件，没有向页面手工注入 ACK 事件。

以下每刀分别在一次性 Git 工作树以 `bbcccbcdd7a957bb0bde8a9786e7d7efd9e568e8` 运行，均 rc1 且命中指定判据；后续 f0d2c87 只修未收件过期边界，未改负控所覆盖的 dispatch/admission/terminal/恢复路径。

```text
enqueue-as-admitted: expected_red rc=1: delivery_ack_real_cli_e2e_linux_test.go:388: 判据2：A长回合期间B排队不得提前 injected/processed
return-as-terminal: expected_red rc=1: delivery_ack_real_cli_e2e_linux_test.go:409: 判据4：B真实终态前不得因函数返回标 processed
drop-recovery-reconciliation: expected_red rc=1: delivery_ack_real_cli_e2e_linux_test.go:537: condition not met before timeout (40s): 判据8：等待原请求恢复对账实际完成
```

r40a 回归负控也保留：`cancel-drop-hint` rc1 红在“空证据取消必须提示填写真实证据引用”；`clarify-drop-retention` rc1 红在“解释区展开后两个自动刷新周期仍须保持 open”。浏览器断言计数来自运行中的计数器。

## 限制与已有问题

旧 View C2、G2 扩展套件在原 HTML 与本刀 HTML 同形失败，详见 [KNOWN-ISSUES.md](KNOWN-ISSUES.md)；不把新增门通过当作旧套件通过。未纳入范围外修法。

本席发送通道在最后两笔提交边界返回 `send.py ConnectionRefusedError`；MCP 只读上下文也返回 `ipc_unavailable`。Git 推送正常，消息未获本地 helper 接受，不能宣称组长已收到。未绕过 IPC 另连 WS，未重启 helper。

## 原始证据

本机目录 `/data/ai-dev-data/r40b-implementation-20261027-evidence`；通过 Git 交付本报告及日志摘要/散列。无源码或制品上传对象存储。各日志 SHA-256 如下；完整原始文件保留本机，未把源码或凭据作为文件传输。

| 日志 | SHA-256 |
| --- | --- |
| `r40b-release-go-strict.log` | `5bd5fb42d386a7d459ea5539ffd1325f3bee6d74145dfd4088636b6199d94312` |
| `r40b-release-go-build.log` | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `r40b-release-run-build-tests.log` | `f31bd38dc12c7693d1a72945460c985063c749ec068c2245299e9cedbf135d42` |
| `r40b-helper-full.log` | `a053419d1898b7e3c75818dd5de8e862d46151213da501f335189fac78150d35` |
| `r40b-final-taskboard.log` | `4711011fd90412cb0ec8902e62022061663906244707fe87fcf7f225cae5c1db` |
| `r40b-final-ack-browser.log` | `e8a3bba84ae7227e24c8f79df9c82e976a71270f213fc8389b770de8c6bc783e` |
| `r40b-final-spec-browser.log` | `5683b7559e2fea8168e1fe3333e2f91ac1fd9304a6d7d6e7589e80816395142b` |
| `r40b-final-cancel-mutation.log` | `435cca448e7b041c3135b7635a7ea77c0a738322bc87ec6c130daacf1ecf59c9` |
| `r40b-final-clarify-mutation.log` | `e9ebd93192393d14e1259f282378ececee871d4896cd2129179710a489f7f4ea` |
| `r40b-b7-mutations.log` | `063fc417018d4f5254f8a450a93eb30f123b3b18ac0dbbb9fbac859a54a6940a` |
| `r40b-b2-expiry-boundary.log` | `698d0353cf70833b78cd96bdda39486b4186dcab29d8e0ab331b17d3b2ebc1f5` |
| `ack-mutations/drop-recovery-reconciliation.log` | `eab381649915558f33bc92c9948f32184e7d95ce8270eadca99b4b5b35686cea` |
| `ack-mutations/enqueue-as-admitted.log` | `a3cac93e8a913fef13ea03a73e1df3a58c11c5b936a78b2770064afb766d350e` |
| `ack-mutations/return-as-terminal.log` | `546d418b1ba019ecb238bfc63d2a169248187392b5049c39d85642b3a5405c0a` |
| `r40b-b6-baseline-renderer-c2-regression.js.log` | `9e07b2645b35223dd0d42e7d38d500f23646e71d37807a85053d7db1a94fbfe5` |
| `r40b-b6-baseline-renderer-canary-g2-regression.js.log` | `82b206682d15ff5107538fcfaa19511ecb33b90645b882a9fb64d3e138d273f3` |
| `r40b-b6-final-renderer-c2-regression.js.log` | `9e07b2645b35223dd0d42e7d38d500f23646e71d37807a85053d7db1a94fbfe5` |
| `r40b-b6-final-renderer-canary-g2-regression.js.log` | `82b206682d15ff5107538fcfaa19511ecb33b90645b882a9fb64d3e138d273f3` |
| `r40b-b6-renderer-contract.log` | `cc46398caa4075739c5df322ccf63c88a6a36dad1b6cd9759d3cfc9d53bdd743` |
| `r40b-b6-renderer-model-admission-regression.js.log` | `f7739740a3a69cfc6f071c23d0ea62aeb5e02dfeb4f3a4e72640e0bdf1da705b` |


## 追加提交链

```text
1bb27e7 store: add nullable delivery ACK protocol and admission evidence fields
abc87f7 r40b: bind delivery ACK protocol and persist monotonic Hub phases
7915244 r40b: migrate helper inbox with immutable protocol provenance
c8287b2 r40b: persist dispatch intent and reconcile driver lifecycle evidence
64ffd11 r40b: replay durable ACK phases independently of worker and reconnect
43eca4f r40b: include ACK replay loop in IPC isolation fixture
9b2feba r40b: render truthful queued admission and turn completion stages
6371625 r40b: direct reminder replies to the original Feishu notice
82c2b85 r40b: preserve explicit supplement type from Feishu to clarification floors
64a15d5 r40b: preserve native failure evidence and quarantine lost pipe recovery
affb6b1 r40b: flush native Claude admission before held model completion
bbcccbc test(delivery): verify ACK lifecycle through actual CLI and View
f0d2c87 fix(delivery): retain transport expiry until helper confirms queued
```
