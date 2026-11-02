# Tasks

变更：r5-binding-admin-page；基线 c248506cf120c3af2a06eeaeeae3038f596537b1。当前仅提交 OpenSpec 小提案，不授权产品实现、apply 或环境操作。

## 1. 提案

- [x] T-01 核对绑定接口、校验、审计及页面访问现状，完成 proposal/design/context/spec。
- [ ] T-02 请 PM 决定全量列表、GitHub 数字 ID 查询方式、覆盖前确认交互；按决议更新并冻结提案。

## 2. 后续（另行授权）

- [ ] T-03 提案获批后另派实现；仅改获批页面，不改变 API/权限/存储语义。
- [ ] T-04 独立验收管理员/非管理员、创建/查询/删除、非法字段、upsert 与审计；实现和验收不得由同一执行者自验收。

## 交接记录

- 2026-11-02T03:08:17Z · togpt60lunazzc.zzc · commit 85feb580e3c922034417dce6f9e32fbc56bc0ac3
  - 完成：以基线 c248506cf120c3af2a06eeaeeae3038f596537b1 核对接口、字段校验、页面权限和存储审计；提交 proposal、design、context、spec。
  - 阻塞：T-02 等 PM 决定；T-03/T-04 需另行授权和派单。
  - 下一步：全仓 OpenSpec strict 校验并推送提案分支；等待 PM 决议。
