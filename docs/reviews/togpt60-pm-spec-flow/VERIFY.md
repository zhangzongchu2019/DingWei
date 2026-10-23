# S-PM-SPEC-FLOW 验收映射

受审设计：`2a6d3260`；实现基线：`integration/spec5ff-r37` (`29e63f99`)。冻结实现源为 `40f74e2f00f0ebe4f5785e8d4a5b4379dc12fa52`；最终门读数及源码摘要见 `VERIFY.json`。

| 设计验收 | 实现位置 | 自动化覆盖 |
|---|---|---|
| 功能默认关闭；仅显式能力开关开放页面和 API | `cmd/workpulse/console_routes.go`; `internal/m8/console_spec_pm_flow.go` | `TestPMSpecFlowDefaultOffAndCASConflictIsHTTP409`; `TestPMSpecFlowPageIsFeatureGatedAndBindsAuthenticatedPrincipal` |
| 工作草稿可变，自动保存用 version＋SHA CAS；冲突两份字节保留 | `internal/store/console_spec_pm_flow.go` (`SavePMSpecWorkingDraft`); `internal/m8/console_pm_spec_flow_page.go` | `TestPMSpecWorkingDraftCASPreservesBothConflictPayloadsAndCreatesNoRevision`; `pm-spec-flow-browser-regression.js` CAS 与新草稿回执丢失场景 |
| 仅接受建议、转审、审查完成固化不可变修订 | `FreezePMSpecWorkingDraft` | `TestPMSpecFreezeOnlyAllowsThreeExplicitActions`; `TestPMSpecAcceptSuggestionsAppliesSelectedStructureAndFreezesOnce` |
| Agent 协助绑定工作草稿版本和 SHA；变更后过期；请求身份、能力、boot、期限重核；可靠 outbox 在实际投递时再核 Agent 当前 active/owner/endpoint，PM dispatch 在原身份锁释放后唤醒队列 | `StartPMSpecAssist`, `CompletePMSpecAssist`; Agent result/request handlers; `Hub.drainSessionDeliveries` 分流 R8 与 PM 请求 | `TestPMSpecReviewCompletionRequiresFreshAssistAndEveryDecisionThenExistingFinalGateRemains`; `TestPMSpecAssistRejectsMalformedStructuredSuggestionPayloads`; `TestPMSpecAssistUsesNegotiatedHelperCapabilityAndAuthenticatedHTTPReply`; `TestPMSpecAssistQueuedDeliveryRechecksCurrentAgentAuthorization`; helper PM tool tests |
| 接受建议写入验收标准／来源引用／规范锚点；逐条接受或不接受 | `applyPMSpecAcceptedSuggestions`, `DecidePMSpecAssist` | `TestPMSpecAcceptSuggestionsAppliesSelectedStructureAndFreezesOnce`; browser 接受／忽略项场景 |
| 当前编辑者可自行完成审查；必须对当前版本重新协助并逐条表态 | `FreezePMSpecWorkingDraft` | `TestPMSpecReviewCompletionRequiresFreshAssistAndEveryDecisionThenExistingFinalGateRemains`; browser self-review 场景 |
| 转审者只读；只能完成审查或带异议退回；不能编辑或定稿 | PM freeze/return handlers and store transaction | `TestPMSpecTransferFreezesDraftAtomicallyAndBlocksLegacyWritesAndFinalize`; browser transfer/recipient role scenario |
| 审查完成后直接定稿并进入既有拆解门 | existing finalize route plus frozen flow guard | store review completion test; browser one-click finalize scenario |
| 旧接口不得绕过新流程写入；旧迁移表缺失时既有行为兼容 | `internal/store/console_specs.go` legacy write barriers and `pmSpecFlowTableExists` | `TestPMSpecTransferFreezesDraftAtomicallyAndBlocksLegacyWritesAndFinalize`; `TestMigration0044PreservesEveryLegacyStateAndDropsClaimedBy` |
| 手机布局及冲突恢复不丢编辑内容 | `console_pm_spec_flow_page.go` | `pm-spec-flow-browser-regression.js` at 390px/820px, conflict pause, explicit merge/load, first-save retry |

## 撤修红绿矩阵

冻结源 `40f74e2f` 上完成六项撤修：移除 CAS 冲突拒绝、允许任意 freeze action、移除 Agent 版本/SHA freshness 检查、跳过全条目表态、允许旧 spec 写路径越过 PM 流程状态门、移除 default-off gate。每项均为基线绿→指定断言红→恢复绿；逐项命令、原始输出和退出码记入 `withdrawals/RESULTS.json` 与同目录文件。

## 运行门

- 最终冻结源全仓 Go：66 包中 53 个有测试包通过、13 个无测试文件；`go vet ./...` 通过。
- canonical：`BUILD_TESTS_OK python=59 shell=2`；helper 隔离 HOME 全套 851 项通过。
- 浏览器 PM Spec Flow 专项 15/0，含 390px／820px；spec workflow 12/0、R32 task flow 11/0。
- broad browser runner 总 rc2，四个非 PM 套件仍红：`renderer_0206_file_render`、`renderer_flood_stress`、`renderer_c2`、`console_specs_browser`。完整原始输出保留，不能视作浏览器全门全绿；PM 专项及相关 workflow 已单独通过。
- source/test 运行输入冻结 SHA、迁移号核对与每个门的 UTC／RC 见 `VERIFY.json`；原始日志与摘要位于 `evidence/`。
