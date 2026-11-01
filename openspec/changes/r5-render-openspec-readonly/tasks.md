# Tasks

派单坐标使用仓库、变更 r5-render-openspec-readonly、T 编号及 commit；单一执行者 coder56solzzc.zzc。当前只准备规格，真实 API 和权限事实未核，禁止先写适配代码再倒填契约。

## 1. 规格就绪

- [x] T-01 依据固定来源与现有代码建立最小基线/context、proposal、固定编号增量及设计；核代码路径与新旧能力差异，列齐 prodops 所需接口/权限。
- [x] T-02 在锁定 CLI 与关闭遥测环境运行 strict 并冻结 Git 坐标；以校验 rc、固定 tip/tree 和交接记录验证交付。

## 2. 依赖与决策冻结

- [ ] T-03 等 prodops 的仓库标识、主体映射/ACL、ref→commit、tree/blob、限流与凭据管理位置事实；等 PM 决定 R5-PM01～04，写回 R5-A01～06、design/context，核对派单后规格变化并 strict 通过。接口和权限未核前不得进入 T-04。

## 3. 读取与安全实现（待授权）

- [ ] T-04 按 CSR-001/003/004 实现受控仓库的固定版本只读适配、逐主体权限检查和路径/tree 类型校验；同步越权/撤权/服务中断/混淆路径/重定向/跨仓缓存测试，确认不能用服务凭据代替用户权限，并写回实际接口映射与 secret 管理位置。
- [ ] T-05 按 CSR-002/005 实现独立安全呈现策略；同步 HTML/属性/scheme/代码围栏与图片请求浏览器用例，确认恶意片段不执行且渲染/滚动无图片网络请求；回归公共渲染器其它页面并更新文档。
- [ ] T-06 按 CSR-001/006/007 实现可见坐标、同 commit 文档导航、原文编号与只读 tasks，以及核定的分页/大小/超时失败反馈；同步 70 KiB 完整读取、末尾标记、各失败与无写请求测试，不把 archive/全勾选显示为验收或发布，并更新 context 中配置名称。

## 4. 集成与证据

- [ ] T-07 按 CSR-S001～015 逐场景记录 PASS/FAIL/未执行/不适用和证据位置，复跑受影响门与 strict；用真实受控工蜂测试仓/获权与失权主体验证，模拟适配器通过不能冒充真实权限继承验收。
- [ ] T-08 获预发授权后复核浏览器网络记录、API 访问审计、凭据不出服务端和固定坐标阅读；冻结干净提交交独立审阅。未获 GO 不部署，未获真实测试权限不做线上探测。

## Workflow follow-up

- 审过再按固定 commit 开实现；R6～R10 独立变更，不在此 tasks 中当作已完成。archive 不代表发布。

OpenSpec status 的 planningComplete/isComplete 只说明规划文件齐备，不代表实现、场景执行、业务验收或发布完成。

## 交接记录

- 2026-11-01T01:19:55Z · coder56solzzc.zzc · commit 518d46f61771de06db5832cce99bfc9a29330579
  - 完成：T-01、T-02；基于 a8bb5e5847884b62ba01f6f48fbce3772e2d8b04 与固定来源 c2d9c9fdd0416828739f7042d2ceb9bc71f603c6 形成规格；OpenSpec 1.16.0 / Node 22.23.1 / OPENSPEC_TELEMETRY=0，`openspec validate --all --strict --no-interactive` rc=0，2 个基线与 2 个变更全部通过、零 issue。首次 R11 校验发现场景标题身份不匹配，保留原完整标题后重验通过；config 仅补引号恢复原规格规则。
  - 阻塞：prodops 的 R5-A01～06 接口/权限事实、R5-PM01～04 与组长独立审阅；场景仍未执行，T-03～08 均未开始；不得从 CLI planningComplete/isComplete 推断业务完成。
  - 下一步：按交件 tip 冻结审阅；先完成 T-03 并回写未决项，再由组长另派实施。当前段只新增本交接记录，未改上述内容 commit 的规格正文；最终冻结 SHA/tree 以承载本记录的提交为准。
