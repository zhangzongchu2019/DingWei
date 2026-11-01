# Proposal

## Why

旧 Console Spec 仍能新建、修订并通过生成门和逐条签核定稿，与研发管理委员会决定的 OpenSpec 唯一规格载体冲突。切换必须同时撤下普通入口、冻结所有旧格式写入并保留历史查询，才能避免用户继续形成双轨事实，同时不丢失已有正文、版本与审计记录。

## What Changes

- **BREAKING**：切换生效后旧规格不再接受新建、修订、派生、生成/签核结果或其它改变规格与门状态的写入，覆盖 Console、共享 capability、Agent/helper 及异步回调。
- 旧 Spec 从普通工作区移除，提供明确标注“历史只读”的独立查询入口；不删除历史、不批量转换，不把 archive 作为新规格发布。
- 停用旧生成门与逐条签核，不替换为自动通过，不新增另一套审结机制；在途任务执行进度与旧规格写入分离。
- 依据现有代码补 `legacy-console-spec` 最小基线/context；本次只提交规格，不开启冻结、不改产品代码和发布配置。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `legacy-console-spec`：LCS-001～003 从现有读写、入口和生成门改为切换后冻结契约；增加 LCS-004 历史保留、LCS-005 在途边界。基线与本增量在本次一并建立，尚未把增量覆盖到基线。

## Impact

涉及 Console 页面/路由、旧规格存储、Agent 直写、旧 PM 草稿与生成回调。需要排查 DB 触发器和两种存储后端；不改 Hub 消息协议、普通发信、文件传输或现存任务进度。依赖平台架构组宣布就绪切换，实施与发布另授权；本轮严格校验不是业务验收。

## 来源

唯一需求来源为委员会决议：[OPENSPEC-SWITCH-DECISION-20261031.md](https://git.code.tencent.com/wego-ai/dingwei/blob/c2d9c9fdd0416828739f7042d2ceb9bc71f603c6/docs/decisions/OPENSPEC-SWITCH-DECISION-20261031.md) 第 2、9、11、16～22 条。
分工与范围：[就绪计划 R11](https://git.code.tencent.com/wego-ai/dingwei/blob/c2d9c9fdd0416828739f7042d2ceb9bc71f603c6/docs/decisions/OPENSPEC-SWITCH-READINESS-PLAN-20261101.md)。来源 commit c2d9c9fdd0416828739f7042d2ceb9bc71f603c6；实现基线 a8bb5e5847884b62ba01f6f48fbce3772e2d8b04；Git 作者不代表需求来源。

## 待确认假设

- R11-A01：旧规格冻结覆盖附属草稿、生成请求/回调、编辑 capability 与 archive/unarchive 等元数据写入；已在运行的任务仅保留任务进度路径。实施前须由组长确认端点清单及在途对象集合，不能凭 UI 隐藏推断完成。
- R11-A02：独立查询复用现有历史存储与权限判断，不迁移数据；未核现网数据量、历史共享链接使用范围及权限撤销机制。不能把旧共享权限套用于 OpenSpec 仓库内容。
- R11-A03：存在切换控制点可在服务器拒写；控制点的配置名、事务边界和发布接线尚未定，实施前补设计，不在就绪规格阶段自建生产开关。

## 待 PM 决策

- R11-PM01 历史共享链接：选项 A 保留原可见范围并只读；B 收紧为 Console 登录/原主体授权。建议 B，避免匿名链接长期暴露历史；需评估旧用户兼容性，未决前不改变现有读取权限。
- R11-PM02 在途清单及切换瞬间请求：选项 A 在途任务继续原执行方式，但所有旧规格正文/门写入从统一生效点拒绝；B 个别旧规格限期可写。建议 A，与“旧规格只读、不保留双轨”一致；B 需委员会明确例外，不由开发自行采用。
- R11-PM03 查询范围：选项 A 标题/编号检索与分页、按修订读取；B 首期增加全文检索。建议 A，保持历史访问范围和实现规模可控。列表具体筛选项在审阅冻结时确认。
