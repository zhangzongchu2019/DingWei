# console-timeline-order Specification

## Purpose
本变更把 Console 时间线改为优先按 Hub 事件序号定位，避免把 helper 与 Hub 的时钟差误当成事件先后。迟到接收确认的用户输入仍按 `input_posted` 的 Hub 序号放回对应回合的前后输出之间。涉及实时渲染、刷新历史回放与向上分页，不改 Hub 投递协议或授权行为。

## Requirements

### Requirement: R51-TIMELINE-001 时间线按 Hub 事件序号定位消息

Console MUST 将回合组与独立卡放在同一时间轴排序。两节点属于同 Hub epoch 且均有 seq 时 MUST 按 seq 升序；节点跨 epoch／跨流时 MUST 按 DOM 首个可见 `.event-time[data-ts]` 的事件时间排序；无 Hub seq 的输入卡 MUST 使用自身事件时间。helper 时钟与 Hub 时钟不得用于同 epoch 内取代 seq。收到 `cli_injected` 确认后，renderer MUST 按 `input_posted` 的 Hub seq 将对应同回合输出分到卡片前后两段，文本合并不得跨过用户卡。实时渲染、刷新重建和向上历史分页 MUST 保持同一顺序；没有可用时间和 Hub seq 的旧节点 MUST 保留稳定回退顺序。

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

#### Scenario: R51-TIMELINE-S005 独立输入卡与回合组共用事件时间轴

- **WHEN** 七张无 Hub seq 的输入卡分别处于相邻回合组事件时间之间，且按任意顺序到达或回放
- **THEN** 整条 timeline 子节点首个 `.event-time[data-ts]` 单调不减，七张卡分别位于对应的前后回合组之间；删除跨流事件时间比较后此断言 MUST 失败

#### Scenario: R51-TIMELINE-S006 同 epoch 时钟偏差

- **WHEN** 同一 Hub epoch 的两个节点事件时间与 Hub seq 顺序相反
- **THEN** renderer MUST 仍按 seq 放置两个节点

#### Scenario: R51-TIMELINE-S007 跨 epoch 并入保留原组位置

- **WHEN** 同一 turn 已有 history 分段，较低 rank 的 live epoch 到来一个应并入该分段的事件，且轴上已有时间更晚、seq 更早的排队卡
- **THEN** renderer MUST 将事件并入该组但保留该组的原 epoch/seq 定位键；整组 MUST 仍位于排队卡之前，且可见时间戳沿整轴单调不减
