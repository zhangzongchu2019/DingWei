# 变更上下文

## 基线

- 冻结 tip：`c01c2f40c02e55c8f37b860ae78753a915c1d725`，父 `47042045a148c68f0c958d9267a186b95ea8569e`，tree `13fef958fefc58fd1a9f4efec5a0164aaddbc1ec`。
- 产品页面：`view-service/renderer-canary-sse.html`；已有浏览器回归：`view-service/renderer-delivery-status-regression.js`。
- 页面定时器每秒调用未知条目刷新；该刷新目前会无条件清空并重建未知列表。
- `deliveryByTurn` 由 `cli_injected` 的 delivery/turn 对建立，用于在后续无 delivery ID 的 assistant/reply 事件上回推处理状态。已知同 delivery 输出事件自身带 delivery ID；`cli_injected` 又已让等待条目离开等待区，因此没有发现此回推能影响可见重发入口的场景。

## 约束

- 不修改 Hub、helper、版本位及协议。
- 不新增 runner suite；扩展现有 `renderer_delivery_status` 用例。
- 浏览器回归使用自托管 renderer、注入时钟和隔离的页面环境。
