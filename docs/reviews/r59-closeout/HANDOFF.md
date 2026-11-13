# r59 未完成变更交接表

基线 a0e280494b2b086c1fac3f765b0d31eb38ec5c09；只建议版本，不构成派单或GO。顶层任务计数不把嵌套T-03.1/03.2重复算入；完整勾选计数见inventory-before.json。19项中8项原全勾、r58两项有原版本证据补勾，另补1项history change；本次归档10项，保留10项。

| 项名 | 已勾/总数 | 剩余项一句话 | 建议归版 |
|---|---:|---|---|
| agent-http-mcp-gateway | 11/14 | T-04/10/13仍缺完整外部收回复/自主循环互操作、独立验收及可选订阅取舍；已核真实登录不重复列为未做。 | r60或后续互操作专项（需另派与真实身份） |
| feishu-agent-sticky-default | 12/14 | T-08/09待真实桌面/移动端复制回入与重启并发矩阵、遗留F-01复核及独立验收。 | r60或后续独立验收/修复专项 |
| hub-graceful-shutdown | 10/11 | T-11仍待全场景整包/race证据与独立预发/生产GO；既有子项勾选不替代最终闭环。 | r60或后续隔离恢复专项 |
| r1-identify-high-risk-changes | 5/8 | T-03/05/08待真实GitHub/工蜂ACL及双端接口接线、受控MR实测和独立QA。 | 真实提供方接入版（r60后另定） |
| r11-freeze-legacy-console-spec | 5/8 | T-05/07/08待业主专属只读旧列表与越权浏览器格、全场景独立审阅及另行切换GO。 | 旧Spec切换版（r60后另定） |
| r5-render-openspec-readonly | 6/8 | T-07/08待真实提供方与独立主体的逐格授权/撤权验收、预发授权和独立审阅。 | 旧Spec切换版（r60后另定） |
| r6-coordinate-review | 6/8 | T-07/08待真实受控Git仓/权限主体/浏览器逐格独立QA与另行预发GO。 | 真实提供方接入版（r60后另定） |
| r7-confirm-immutable-version | 5/11 | T-03～08待真实权限和接口前置、版本有效性/失效实现与逐格独立验收及预发GO。 | 真实提供方接入及旧Spec切换版（r60后另定） |
| r8-evidence-driven-change-lifecycle | 6/8 | T-03/08待真实提供方审批/PM/委员会事实和全部场景独立QA及预发授权。 | 真实提供方接入版（r60后另定） |
| r9-pm-decision-request | 9/10 | T-10待独立验收后的预发/生产GO与切换就绪，MR评论写权限仍后置。 | 独立验收/GO专项（r60后另定） |

## r58 专项核对

原3/5→5/5；T-04/05不是本次重测补造，原0054bd049上的全部门、两刀和Git交件已在独立evidence/coder56solzzc-r58-integration d63bf9d1完成，rulings补94复核通过。坐标逐项见TABLE.md。
