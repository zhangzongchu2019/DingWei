# T2 上线项验收 —— 可靠投递活体 + 浏览器 #6/#7

- 驱动：manager（zzc-manager-2642）
- 日期：2026-07-12
- 决策：先跑无损部分；iptables 丢包用例推迟到 Mac/符磊/谭平升级完、避开发布窗口再单独跑（张宗楚 2026-07-12 定）。

## A. 代码路径验证（无损，已完成 ✅）

对可靠投递 + ACK 三态的完整往返做了逐行审查，结论：**状态机正确且闭环**。

**触发条件（重要）**：可靠投递**只对真飞书入站消息生效** —— `hub.go:2247/2259` 里 `ackInbound = from.Kind == addressFeishu`。会话之间互发走的是 `c.write` 直投快路径，**不进 `session_delivery` 通道**。所以活体验证**必须由飞书侧发起一条真实消息**，无法用会话互发替代。

**状态机（Hub 侧 `session_delivery`）**：
- feishu 入站 → `EnqueueSessionDelivery` 置 `queued`（hub.go:2795）
- `drainSessionDeliveries` claim → `sent`，写给 helper（hub.go:2872）；FIFO，一次一条（`ClaimSessionDelivery` 用 `NOT EXISTS ... state IN('sent','delivered')` 保证每会话同时只有一条在飞）
- helper 回 `delivery_ack`：
  - `processed` → 表置 `processed`（hub.go:2848）
  - `delivered`/`processing` → 表置 `delivered`，lease 续 2 分钟（hub.go:2863）
  - 失败 → `queued` 重试（+2s）或 `dead`（hub.go:2856/2859）

**helper 侧（`inbox.py`）**：
- `delivery_inbox` 以 `delivery_id` 为 PRIMARY KEY → **重复投递幂等**：同 delivery_id 命中已存在行，只注入一次、重复回 ACK（inbox.py:42-58）。这正是 iptables 丢包用例要验的"至少一次投递 + 去重"。
- 线协议三态映射：内部 `processing` 上线映射为 `delivered`（inbox.py:72-73）。
- ACK 三态对用户可见：`delivered`=已送达；排队时 `notifyDeliveryQueued` 回"消息已接收，前面还有 N 条待处理"=排队等待；`processing`=正在处理。

**结论**：代码层面 at-least-once + 幂等注入 + 三态 ACK + FIFO 串行 均已正确实现。

## B. 生产实况（无损观测，已完成 ✅）

只读查生产 `data/workpulse.db` 的 `session_delivery`：**表为空**。即可靠投递通道自上线以来**从未处理过一条真实消息**——印证其为"已上线/待验收"，绿灯不成立。

## C. 待触发（需飞书侧 + 浏览器，人工动作）

以下三项我已备好观测，但触发动作不在 manager 侧：

1. **可靠投递 happy path**：请从飞书给某个 2.2.0 在线会话（如 #tester 或 #manager）发一条真实消息。我实时盯 `session_delivery`，取证 `queued→sent→delivered→processing→processed` 全程 + `delivered_at`/`processed_at` 落值。
2. **iptables 丢包用例**：脚本 `scripts/reliable_delivery_canary.sh` 已就绪（`APPLY_IPTABLES=1 HUB_IP=... DROP_SECONDS=15`）。**推迟到发布窗口后**，且建议在专用 canary 会话上跑而非全局。
3. **浏览器 #6/#7**：底部截断、空闲回放 —— 需真实浏览器打开 `/view/<会话>` 操作取证（#5 `#unlock` 已通过）。

## D. iptables 丢包用例的生产风险提示

`scripts/reliable_delivery_canary.sh` 的 apply 模式 `iptables -I OUTPUT -p tcp -d <HUB_IP> --dport 8791 -j DROP` 会丢**该机到 Hub 的全部 ACK 流量**。若在生产 Hub 本机或承载多会话的机器上跑，会**短暂影响所有在线会话**。务必：仅在专用 canary 会话/机器上跑、避开发布窗口、`DROP_SECONDS` 取小、trap 清理已内置。
