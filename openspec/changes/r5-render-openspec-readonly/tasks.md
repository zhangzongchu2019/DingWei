# Tasks

派单坐标使用仓库、变更 r5-render-openspec-readonly、T 编号及 commit；单一执行者 coder56solzzc.zzc。本轮已派 T-03～T-06；GitHub 事实 e8b14b69 已读取并回写，真实权限验收 T-07/T-08 另派。

## 1. 规格就绪

- [x] T-01 依据固定来源与现有代码建立最小基线/context、proposal、固定编号增量及设计；核代码路径与新旧能力差异，列齐 prodops 所需接口/权限。
- [x] T-02 在锁定 CLI 与关闭遥测环境运行 strict 并冻结 Git 坐标；以校验 rc、固定 tip/tree 和交接记录验证交付。

## 2. 依赖与决策冻结

- [x] T-03 等 prodops 的仓库标识、主体映射/ACL、ref→commit、tree/blob、限流与凭据管理位置事实；落实已批 R5-PM01～04（首月管理员维护按提供方主体绑定，逐主体核权），写回 R5-A01～06、design/context，核对派单后规格变化并 strict 通过。接口和权限未核前不得进入 T-04。

## 3. 读取与安全实现（本轮已授权）

- [x] T-04 按 CSR-001/003/004 实现受控仓库的固定版本只读适配、逐主体权限检查和路径/tree 类型校验；同步越权/撤权/服务中断/混淆路径/重定向/跨仓缓存测试，确认不能用服务凭据代替用户权限，并写回实际接口映射与 secret 管理位置。
- [x] T-05 按 CSR-002/005 实现独立安全呈现策略；同步 HTML/属性/scheme/代码围栏与图片请求浏览器用例，确认恶意片段不执行且渲染/滚动无图片网络请求；回归公共渲染器其它页面并更新文档。
- [x] T-06 按 CSR-001/006/007 实现可见坐标、同 commit 文档导航、原文编号与只读 tasks，以及已批 2 MiB 单文件限额和待核分页/超时失败反馈；同步 70 KB（另以 70 KiB 验证更强下界） 完整读取、末尾标记、恰好 2 MiB 完整/超出 1 字节拒绝与已批文案及来源链接、context.md 源码外链不代抓、各失败与无写请求测试，不把 archive/全勾选显示为验收或发布，并更新 context 中配置名称。

## 4. 集成与证据

- [ ] T-07 按 CSR-S001～015 逐场景记录 PASS/FAIL/未执行/不适用和证据位置，复跑受影响门与 strict；用真实受控仓库托管提供方测试仓/获权与失权主体验证，模拟适配器通过不能冒充真实权限继承验收。
- [ ] T-08 获预发授权后复核浏览器网络记录、API 访问审计、凭据不出服务端和固定坐标阅读；冻结干净提交交独立审阅。未获 GO 不部署，未获真实测试权限不做线上探测。

## Workflow follow-up

- 审过再按固定 commit 开实现；R6～R10 独立变更，不在此 tasks 中当作已完成。archive 不代表发布。

OpenSpec status 的 planningComplete/isComplete 只说明规划文件齐备，不代表实现、场景执行、业务验收或发布完成。

## 交接记录

- 2026-11-01T01:19:55Z · coder56solzzc.zzc · commit 518d46f61771de06db5832cce99bfc9a29330579
  - 完成：T-01、T-02；基于 a8bb5e5847884b62ba01f6f48fbce3772e2d8b04 与固定来源 c2d9c9fdd0416828739f7042d2ceb9bc71f603c6 形成规格；OpenSpec 1.16.0 / Node 22.23.1 / OPENSPEC_TELEMETRY=0，`openspec validate --all --strict --no-interactive` rc=0，2 个基线与 2 个变更全部通过、零 issue。首次 R11 校验发现场景标题身份不匹配，保留原完整标题后重验通过；config 仅补引号恢复原规格规则。
  - 阻塞：prodops 的 R5-A01～06 接口/权限事实、R5-PM01～04 与组长独立审阅；场景仍未执行，T-03～08 均未开始；不得从 CLI planningComplete/isComplete 推断业务完成。
  - 下一步：按交件 tip 冻结审阅；先完成 T-03 并回写未决项，再由组长另派实施。当前段只新增本交接记录，未改上述内容 commit 的规格正文；最终冻结 SHA/tree 以承载本记录的提交为准。

## 本轮 PM 回写

业主 2026-11-01T02:23:43Z 答复与 02:3xZ 补充已纳入，R5-PM01～04 已决；旧交接记录的 PM 待批为当时状态。R5/R6/R10 共享读取契约按仓库托管提供方抽象；T-03～08 仍未完成，接口/权限事实及实现/产品验证待后续派单。

## T-03～T-06 本轮交件

- 本轮按派单从 3fd26ba6 新分支 opus50tc2k/r5-impl，普通 merge 68c178a5 得 a7c24704；产品实现 a118455e，安全测试补充 0e62fcbd。最终交件 tip/tree 见同分支 docs/reviews/r5-readonly/REPORT.md 的证据和交件消息（最终文档提交只增交件记录）。
- prodops e8b14b69 事实与缺口已回写 proposal R5-A01～06、design/context；OpenSpec strict 12/12 rc0。GitHub 适配器、管理员绑定表/API、默认关闭、新只读安全页面和 2 MiB 边界已实现。真实凭据、绑定均未读取或创建。
- run-build-tests rc0；完整 m8 与 workpulse 包通过；完整 store 首轮发现 0070 补账缺口，修复后整包 rc0，旧失败保留。新浏览器最终定向 19 检查通过（全套时 18 检查通过）；40 项防护变异均为断言转红。
- 全浏览器 26 套 24 rc0，aggregate rc2：旧 renderer_0206_file_render 与 renderer_flood_stress 对应已独立冻结的 browser-baseline-fix，未在本分支重复修改；详情与原日志见交件报告。不把全门禁写成全绿，r46 集成须带该独立子片。
- T-07/T-08 未执行：模拟测试不等于真实平台权限验收；令牌完整 scope、工蜂专用只读凭据与真实 Console/GitHub 绑定仍是后续事实缺口。未部署、重启或启用功能。

## S-R5-CRED-SYSTEMD 交接（2026-11-01T18:23:19+00:00）

- coder56solzzc.zzc 单写，分支 opus50tc2k/r5-cred-systemd，基于 r46 2af469dd；代码与测试提交 85beea907d7511206d08bb7c33a1fd22139189f9（tree a087b9b9a573547a133c53ec7f1902070c25a925）。仅 credential() 安全判定兼容精确 CREDENTIALS_DIRECTORY，外部路径维持 0077；其余读取/授权/开关逻辑不动。
- 完成：六格必测及额外边界/轮换均通过；13 目标变异均断言红，恢复 0077 令 0440 正格红。PG strict 下 specread 96 pass、m8 1206 pass/36 skip/0 fail；跳过名与条件见 docs/reviews/r5-cred-systemd/SKIPS.md。run-build-tests rc0（lock venv、剥继承变量），两个 profile 各 1092，Python27/shell2；OpenSpec strict 12/12 rc0。完整摘要与原始本地日志位置见同目录 REPORT.md、JSON。
- 阻塞/限制：本单只用临时目录和合成内容，没有真实 PAT/ACL 或 root:root、UID 991 的修复后实测；不能把单测认作预发验收。T-07/T-08 原状态不由本单改变。
- 下一步：证据提交后仅推工蜂并核远端 tip，冻结交组长独立审阅；等待另发预发复测令。不部署、不动 r46 或其它冻结分支。最终 tip/tree 以承载本记录的提交及交件消息为准。
