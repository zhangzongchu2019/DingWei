# OpenSpec 未勾任务清账

基线：`opus50tc2k/r54-openspec-archive` `e803d95bc047d6a4ea99bb366659c123d2fa6aeb`。仅修改 14 个变更的 `tasks.md`；未改规格正文，未执行 archive。

| 变更 | 任务号 | 判定 | 证据坐标 / 交接 |
|---|---|---|
| `agent-http-mcp-gateway` | T-04 | 乙 | 尚缺真实飞书网页登录/真实第三方 MCP SDK 对话式与自主循环互操作；由集成负责人安排真实身份与标准客户端验收。 证据/核对：docs/reviews/r53-mcpgw/EVIDENCE.md @ 0fe09b9e3df85ee8b32fe08cdeec60099d2b540c |
| `agent-http-mcp-gateway` | T-05 | 甲 | docs/reviews/r53-mcpgw/EVIDENCE.md @ 0fe09b9e3df85ee8b32fe08cdeec60099d2b540c |
| `agent-http-mcp-gateway` | T-06 | 甲 | docs/reviews/r53-extseat/EVIDENCE.md; docs/reviews/r53-mcpgw/EVIDENCE.md @ 0fe09b9e3df85ee8b32fe08cdeec60099d2b540c |
| `agent-http-mcp-gateway` | T-07 | 甲 | docs/reviews/r53-extseat/EVIDENCE.md; docs/reviews/r53-tempgroup/EVIDENCE.md @ 0fe09b9e3df85ee8b32fe08cdeec60099d2b540c |
| `agent-http-mcp-gateway` | T-08 | 甲 | docs/reviews/r53-extseat/EVIDENCE.md; docs/reviews/r53-mcpgw/EVIDENCE.md; docs/reviews/r53-tempgroup/EVIDENCE.md @ 0fe09b9e3df85ee8b32fe08cdeec60099d2b540c |
| `agent-http-mcp-gateway` | T-09 | 甲 | docs/reviews/r53-mcpgw/EVIDENCE.md (十刀); docs/reviews/r53-extseat/EVIDENCE.md (十一刀); docs/reviews/r53-tempgroup/EVIDENCE.md (四刀) @ 0fe09b9e3df85ee8b32fe08cdeec60099d2b540c |
| `agent-http-mcp-gateway` | T-10 | 乙 | 尚缺真实第三方标准 MCP SDK 互操作验收；需独立 QA/业主完成后补齐交接与切换验收。现有证据明确未做此项。 证据/核对：docs/reviews/r53-mcpgw/EVIDENCE.md @ 0fe09b9e3df85ee8b32fe08cdeec60099d2b540c; docs/reviews/prodops-r53-prod-closeout-20261107/RESULT.md @ 2965ace9d9d5935dae78df0a5bf58ba5222e7804 |
| `agent-http-mcp-gateway` | T-12 | 甲 | docs/reviews/r53-tempgroup-offstate/raw/browser-receipt.md; docs/reviews/r53-tempgroup-ui/EVIDENCE.md @ 0fe09b9e3df85ee8b32fe08cdeec60099d2b540c |
| `agent-http-mcp-gateway` | T-13 | 乙 | 缺少收件资源/subscriptions-listen 的一期取舍结论与实现；由 PM/业主定是否二期立项，若保留则另派实现及验证。 证据/核对：docs/reviews/r53-mcpgw/EVIDENCE.md @ 0fe09b9e3df85ee8b32fe08cdeec60099d2b540c |
| `feishu-agent-sticky-default` | T-08 | 乙 | 缺真实飞书桌面/移动端复制回入及Hub重启、迟到/并发矩阵；业主授权并提供真实客户端后由 QA/prodops 执行。 证据/核对：docs/reviews/togpt60-r49-r14-t09-acceptance/REPORT.md @ f0801adbdc9946a803067610d0d106b0e0e61ce2; docs/reviews/prodops-r53-prod-closeout-20261107/RESULT.md @ 2965ace9d9d5935dae78df0a5bf58ba5222e7804 |
| `feishu-agent-sticky-default` | T-09 | 乙 | 独立验收报告记录 F-01 安全阻断；实现负责人修复后，由 QA 在最终源锚独立复验，再申请 GO。证据：docs/reviews/togpt60-r49-r14-t09-acceptance/REPORT.md @ f0801adbdc9946a803067610d0d106b0e0e61ce2 |
| `hub-graceful-shutdown` | T-10 | 乙 | HLC-S011 自动恢复仍有 gap=1/missing=2，需实现修复并按冻结数值完成隔离 stop→ready 三类流矩阵；由实现席修复、QA独立复验。 证据/核对：docs/reviews/hlc-primary-integrated/REPORT.md @ 81849a388d28bec732848227b548be9a894addc3 |
| `hub-graceful-shutdown` | T-11 | 乙 | HLC-S011 未闭环且独立验收/GO 未完成；修复后由实现席跑全门，QA独立验收，业主另批预发/生产GO。 证据/核对：docs/reviews/hlc-primary-integrated/REPORT.md @ 81849a388d28bec732848227b548be9a894addc3 |
| `r1-identify-high-risk-changes` | T-03 | 乙 | 真实 GitHub/工蜂 ACL、审阅/评论/合并接口及凭据位置未核；由 prodops/提供方负责人补事实后更新规格。 证据/核对：docs/reviews/r1-r8-core/REPORT.md @ 397fafb1c307f624742e4bf477c0ad68d40a2395 |
| `r1-identify-high-risk-changes` | T-04 | 甲 | docs/reviews/r1-r8-core/REPORT.md @ 397fafb1c307f624742e4bf477c0ad68d40a2395 |
| `r1-identify-high-risk-changes` | T-05 | 乙 | GitHub 与工蜂仍为 ErrUnavailable stub，真实权限及双端投递未接；由实现席按提供方事实接线并独立验证。 证据/核对：docs/reviews/r1-r8-core/REPORT.md @ 397fafb1c307f624742e4bf477c0ad68d40a2395 |
| `r1-identify-high-risk-changes` | T-06 | 甲 | docs/reviews/r1-r8-core/REPORT.md @ 397fafb1c307f624742e4bf477c0ad68d40a2395 |
| `r1-identify-high-risk-changes` | T-07 | 甲 | docs/reviews/r1-r8-core/REPORT.md @ 397fafb1c307f624742e4bf477c0ad68d40a2395 |
| `r1-identify-high-risk-changes` | T-08 | 乙 | 缺真实隔离 MR 双端验收与独立 QA；由 QA 用真实受控仓库完成，获得授权后再申预发GO。 证据/核对：docs/reviews/r1-r8-core/REPORT.md @ 397fafb1c307f624742e4bf477c0ad68d40a2395 |
| `r10-task-dispatch-lock` | T-10 | 甲 | docs/reviews/r50-r10-acceptance/REPORT.md @ 8fc3e22d8981483192d0d963632618e1c862470f; docs/reviews/prodops-r50-prod-closeout-20261105/RESULT.md @ 559c8fcc9e98e6562a99eda5d072f5c9774de35d |
| `r11-freeze-legacy-console-spec` | T-05 | 乙 | LCS-002 业主只读旧列表尚未实现；由实现负责人完成列表入口/服务端授权与浏览器格。 证据/核对：openspec/changes/r11-freeze-legacy-console-spec/tasks.md handoff; 未找到允许来源内的结项证据 |
| `r11-freeze-legacy-console-spec` | T-07 | 乙 | 尚缺LCS-002及依赖S003/006格、完整独立审阅；实现完成后由 QA 逐场景复验。 证据/核对：未找到允许来源内的结项证据 |
| `r11-freeze-legacy-console-spec` | T-08 | 乙 | 无预发授权与切换发布证据；业主授权后由 prodops 验证并交审。 证据/核对：未找到允许来源内的结项证据 |
| `r15-session-history-persist` | T-03 | 甲 | ruling ㊲ @ a5a4de214cc9c28ea08aabb8952a4c9b1387522e; docs/reviews/togpt60-r50-r15-final-20261103/REPORT.md @ 43b4d20d1268e6974dde6edc2508ed9a9d8edce5 |
| `r15-session-history-persist` | T-04 | 乙 | 最终重构后尚缺独立执行者完整重启/重连/撤权/迁移回退矩阵；由 QA 在最终源锚复验。 证据/核对：docs/reviews/r15-accept-browser-20261103/README.md @ fdcf455a6ebf2a0139f98ad9386e67dbd86adb93; docs/reviews/togpt60-r50-r15-final-20261103/REPORT.md @ 43b4d20d1268e6974dde6edc2508ed9a9d8edce5 |
| `r15-session-history-persist` | T-09 | 甲 | ruling ㊲ @ a5a4de214cc9c28ea08aabb8952a4c9b1387522e; ruling ㊲ @ a5a4de214cc9c28ea08aabb8952a4c9b1387522e |
| `r5-binding-admin-page` | T-04 | 甲 | docs/reviews/r49-r5-page-t04-acceptance/REPORT.md @ 852ae658dcb6bc1a7ddfbc3b9a5cba61e89f823b |
| `r5-render-openspec-readonly` | T-07 | 乙 | 缺真实受控 Git 托管测试仓及获权/失权主体完整CSR矩阵；由 QA 与提供方负责人补真实权限验证。 证据/核对：docs/reviews/r5-readonly/REPORT.md @ 7f63b3805e5e741db073120dd648fd8be541ebc5 |
| `r5-render-openspec-readonly` | T-08 | 乙 | 缺获授权预发的网络/审计/凭据路径验收；由业主授权后 prodops 与独立审阅者执行。 证据/核对：docs/reviews/r5-readonly/REPORT.md @ 7f63b3805e5e741db073120dd648fd8be541ebc5 |
| `r52-trusted-origins-impl` | T-07 | 甲 | docs/reviews/r52-trusted-origins-impl/raw/browser-receipt-at-9da5b75f.txt @ 425f6452e757d4474c0ac46b18d68197d878936c |
| `r6-coordinate-review` | T-07 | 乙 | 缺真实受控 Git 仓/独立权限主体及 QA 全场景复核；由 QA/提供方负责人完成。 证据/核对：未找到允许来源内的真实提供方/QA结项证据 |
| `r6-coordinate-review` | T-08 | 乙 | 缺预发授权及实际出站信封、超时提醒/重审提示环境核验；业主授权后由 prodops执行。 证据/核对：未找到允许来源内的真实提供方/QA结项证据 |
| `r7-confirm-immutable-version` | T-03 | 乙 | 实际 GitHub/工蜂主体ACL、目录查询事实及secret位置未核；prodops/提供方负责人补事实。 证据/核对：docs/reviews/r1-r8-core/REPORT.md @ 397fafb1c307f624742e4bf477c0ad68d40a2395;  |
| `r7-confirm-immutable-version` | T-04 | 乙 | 当前 R7 bridge 默认503、提供方/ACL接线未完成；由实现席按核实接口完成并测撤权/异常矩阵。 证据/核对：docs/reviews/r1-r8-core/REPORT.md @ 397fafb1c307f624742e4bf477c0ad68d40a2395;  |
| `r7-confirm-immutable-version` | T-05 | 乙 | 缺真实版本策略读取与全场景失效判定，核心实现未完成；实现席补齐。 证据/核对：docs/reviews/r1-r8-core/REPORT.md @ 397fafb1c307f624742e4bf477c0ad68d40a2395;  |
| `r7-confirm-immutable-version` | T-06 | 乙 | 缺R7真实提醒/拒绝路径和追加历史验收；实现席完成后交独立QA。 证据/核对：docs/reviews/r1-r8-core/REPORT.md @ 397fafb1c307f624742e4bf477c0ad68d40a2395;  |
| `r7-confirm-immutable-version` | T-07 | 乙 | 缺真实受控Git仓、独立权限主体与全场景独立复核；由 QA 执行。 证据/核对：docs/reviews/r1-r8-core/REPORT.md @ 397fafb1c307f624742e4bf477c0ad68d40a2395 |
| `r7-confirm-immutable-version` | T-08 | 乙 | 缺预发授权与环境GO证据；业主授权后 prodops 验证。 证据/核对：docs/reviews/r1-r8-core/REPORT.md @ 397fafb1c307f624742e4bf477c0ad68d40a2395 |
| `r8-evidence-driven-change-lifecycle` | T-03 | 乙 | 真实 GitHub/工蜂有效审批、PM/委员会字段、可信主体与secret位置未核；prodops/提供方负责人补事实。 证据/核对：docs/reviews/r1-r8-core/REPORT.md @ 397fafb1c307f624742e4bf477c0ad68d40a2395 |
| `r8-evidence-driven-change-lifecycle` | T-04 | 甲 | docs/reviews/r1-r8-core/REPORT.md @ 397fafb1c307f624742e4bf477c0ad68d40a2395 |
| `r8-evidence-driven-change-lifecycle` | T-05 | 甲 | docs/reviews/r1-r8-core/REPORT.md @ 397fafb1c307f624742e4bf477c0ad68d40a2395 |
| `r8-evidence-driven-change-lifecycle` | T-06 | 甲 | docs/reviews/r1-r8-core/REPORT.md @ 397fafb1c307f624742e4bf477c0ad68d40a2395 |
| `r8-evidence-driven-change-lifecycle` | T-07 | 甲 | docs/reviews/r1-r8-core/REPORT.md @ 397fafb1c307f624742e4bf477c0ad68d40a2395 |
| `r8-evidence-driven-change-lifecycle` | T-08 | 乙 | 缺真实提供方受控事实及独立QA/预发授权；由 QA 与 owner 完成，不能以stub测试代替。 证据/核对：docs/reviews/r1-r8-core/REPORT.md @ 397fafb1c307f624742e4bf477c0ad68d40a2395 |
| `r9-pm-decision-request` | T-10 | 乙 | 缺验收后预发/生产GO与切换就绪结论；由独立QA出验收，业主/PM批GO，prodops记录发布事实。 证据/核对：docs/reviews/r51-r9-impl-20261104/EVIDENCE.md @ 92be585f7b75026457baaaea5cd22715130732b6; docs/reviews/prodops-r53-prod-closeout-20261107/RESULT.md @ 2965ace9d9d5935dae78df0a5bf58ba5222e7804 |

## 全部判甲、可归档

- `r10-task-dispatch-lock`
- `r5-binding-admin-page`
- `r52-trusted-origins-impl`

## 判定口径

- 甲只表示该任务行所述实现/证据任务已有对应 Git 证据，不代表本次执行 archive。
- 乙保持未勾，handoff 写明具体缺项和责任方；找不到满足规则的证据时按乙。
- 本次未判丙：已核到的未勾任务均仍在各自变更范围内，没有可引用的后续承接/作废裁定。
