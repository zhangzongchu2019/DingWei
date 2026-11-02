# Tasks

变更：r5-binding-admin-page；提案历史基线 c248506cf120c3af2a06eeaeeae3038f596537b1。S-R49-R5-BINDING-PAGE-IMPL（授权第108条）已批准实现，当前实施基线为冻结的1e17255f28ced042784337903914ca8c126ef4cf；不授权apply或环境操作。

## 1. 提案

- [x] T-01 核对绑定接口、校验、审计及页面访问现状，完成 proposal/design/context/spec。
- [x] T-02 按 rulings ㉙ 回写 PM-R5-BIND-01～03：首期单条查询、管理员手填 GitHub ID 且服务端不出网、页面确认后执行现有 upsert 覆盖；原选项保留为历史。

## 2. 后续（另行授权）

- [x] T-03 提案已获批并完成页面实现；仅新增管理员页面和平台管理区入口，复用既有 API/权限/校验/存储/审计，开发验证不代替 T-04 独立验收。
- [ ] T-04 独立验收管理员/非管理员、创建/查询/删除、非法字段、upsert 与审计；实现和验收不得由同一执行者自验收。

## 交接记录

- 2026-11-02T08:45Z · coder56solzzc.zzc · S-R49-R5-BINDING-PAGE-IMPL，接单会话774d8962（76f1cef3执行路），以1e17255f为基线。
  - 完成：普通merge afcee7c256e4f01d70c52ed35dd24c286f321ebe，仅并入指定提案目录，零冲突；合并6b53ad09daab0bba046c34e64b4cbfbe7073c0d5/tree04b5f9bb736ca275120772e22c127b129edc025a。新增绑定页/入口；三输入固定provider/instance；每次保存/删除前查询当前映射、明确确认后调用现有PUT/DELETE，输入变化使旧确认失效；错误作为原文文本显示。未改既有API、默认读取开关、依赖、迁移或版本位。
  - 验证：草稿实际生产路由管理员/manager/member/未登录四格通过；两浏览器正常流通过，Proxy计数131（最终下限125）；三变异各渠道命中BIND_CONFIRM_BEFORE_PUT、BIND_OLD_VISIBLE、BIND_ADMIN_ENTRY_ONLY语义断言。首轮撤确认变异被busy保护挡住、rc0，原日志保留为无效装置；修正注入后才计有效目标红，无产品阈值修改。
  - 阻塞：本提交源码的canonical仓库门、m8完整包及最终两渠道/变异、路由回归、OpenSpec strict尚待执行，不把草稿日志冒充最终门。T-04独立验收另派，本席不自验收、不创建真实绑定或部署。
  - 下一步：干净精确提交后串行剥净环境/构建venv/短TMPDIR全门；证据另分支只放docs，提交源码分支后逐字核ls-remote与HEAD再交件。最终坐标与逐门读数以Git证据分支回执为准。

- 2026-11-02T03:08:17Z · togpt60lunazzc.zzc · commit 85feb580e3c922034417dce6f9e32fbc56bc0ac3
  - 完成：以基线 c248506cf120c3af2a06eeaeeae3038f596537b1 核对接口、字段校验、页面权限和存储审计；提交 proposal、design、context、spec。
  - 阻塞：T-02 等 PM 决定；T-03/T-04 需另行授权和派单。
  - 下一步：全仓 OpenSpec strict 校验并推送提案分支；等待 PM 决议。

- 2026-11-02T03:30:59Z · togpt60lunazzc.zzc · commit 8868d77e348879a3bd64f772c9abffb57b632cb1
  - 完成：PM-R5-BIND-01～03 按 rulings ㉙ 定值；T-02 闭合。
  - 阻塞：无；后续 T-03/T-04 仍需另行授权和派单。
  - 下一步：strict 已通过；推送本分支并核对远端 tip/tree。
