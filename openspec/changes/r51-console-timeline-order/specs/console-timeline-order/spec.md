# Console Timeline Order

## Purpose

本变更把 Console 时间线改为优先按 Hub 事件序号定位，避免把 helper 与 Hub 的时钟差误当成事件先后。迟到接收确认的用户输入仍按 `input_posted` 的 Hub 序号放回对应回合的前后输出之间。涉及实时渲染、刷新历史回放与向上分页，不改 Hub 投递协议或授权行为。

## ADDED Requirements

### Requirement: R51-TIMELINE-001 时间线按 Hub 事件序号定位消息

Console MUST 对带 Hub cursor 的用户输入、Agent 回复和历史节点使用同一规则，按 Hub epoch 内的 seq 升序放入时间线。helper `ts` 与 Hub `updated_at` 来自不同机器，不得用两者的毫秒差判定卡片先后。收到 `cli_injected` 确认后，renderer MUST 按 `input_posted` 的 Hub seq 将对应同回合输出分到卡片前后两段，文本合并不得跨过用户卡。实时渲染、刷新重建和向上历史分页 MUST 保持同一顺序；没有 Hub cursor 的旧节点 MUST 保留稳定回退顺序。

#### Scenario: R51-TIMELINE-S001 回合中注入并迟到确认

- **WHEN** 同一回合已有较早输出，用户输入进入待提交区，随后同回合继续输出，而接收确认可能更晚到达
- **THEN** 用户卡按 `input_posted` Hub seq 位于较早输出和较晚输出之间，页面显示完整文本且不跨卡合并

#### Scenario: R51-TIMELINE-S002 刷新重建顺序

- **WHEN** 页面刷新并按相同事件序列从会话历史重建时间线
- **THEN** 用户卡仍在对应 Agent 回复之前

#### Scenario: R51-TIMELINE-S003 空闲发送保持顺序

- **WHEN** Agent 空闲时先有一条既有回复，再收到并确认用户输入
- **THEN** 既有回复仍在用户卡之前

#### Scenario: R51-TIMELINE-S004 历史分页先读确认、后读输入

- **WHEN** 最新历史页已含注入确认和较晚输出，向上翻页才读到较早的输入卡及回合输出
- **THEN** renderer 使用暂存的注入栅栏重建回合分段，按 Hub seq 插入节点，历史与实时顺序稳定，原可视锚点保持不变
