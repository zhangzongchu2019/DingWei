# R52 投递状态与取消设计

## 复现与根因

基线 `425f6452e757d4474c0ac46b18d68197d878936c` 的 renderer 回归 `renderer-c2-regression.js` 已运行，rc=0；它验证取消请求与取消事件，但不验证 helper 已取走后的取消结果。代码路径实证：`internal/m8/terminal.go:897` 把 `queued/sent/delivered` 一概发布为 `cancellable=true`；`cancelSessionDeliveryCore` 在 Hub 队列撤销失败后仍把 `sent/delivered` 转发给 helper；helper 的 `delivery_inbox.cancel` 仅能原子取消 `received`，到 `processing` 即回 `not_cancelable`（`tools/sessionhelper/sessionhelper/app.py:1253`、`inbox_lifecycle.py`）。因此 delivered 之后，页面仍显示删除而 helper 拒绝；View 收到拒绝 ACK 当前只会移除待确认提示，不会给条目解释。真实 helper 单测已覆盖 processing 拒绝（`tools/sessionhelper/tests/test_sessionhelper.py:149-201`）。这是状态契约与 UI 反馈缺口，不是点击绑定故障。

## 改法

- Hub 仅在尚未交给 helper 的可撤销队列态发布 `cancellable=true`；已进入 helper 生命周期的 delivered 不再显示删除。保留 Hub 对竞态取消请求的防线；收到 helper `not_cancelable` 时以安全的 `delivery_update` 反馈「已交给 Agent，无法撤回」，不触碰 helper 账本。
- 对 `model_admission_unknown` 显示「Agent 正忙：消息已交出但未确认被处理，请看它的下一条回复」。提供显式重发，使用新 delivery、复用原正文与附件；Hub 以原 delivery 作为幂等键，只准创建一次，原条标记已重发，重发按钮随后消失。
- Hub 的会话事件处理在同一会话、原 delivery 的 `turn_completed` 到达后，把 admission_unknown 对应投递推进为可重发的未确认终态展示；事件必须绑定原投递身份，不能以无关回合收敛，也不写 helper 账本。具体实现优先落 Hub/View 投影状态，保留 delivery 的 helper ACK 事实。
- `inboxLagState` 的未知计数旁增加只读入口，列出当前启动未知条目；无明细数据时显示明确空/不可用状态，不推测正文。

## 验证

基线浏览器套件 rc=0；删除根因由 Hub 的宽泛 `cancellable` 发布逻辑与 helper 仅允许 received 取消的既有测试共同复现。实现新增真实跨层浏览器回归，覆盖取消前后阶段、拒绝原因、重发单次幂等、回合结束收敛、新文案与未知条目入口；按派单完成四项独立变异。Go m8 与全量构建、浏览器门禁结果随最终提交 SHA 写入证据。
