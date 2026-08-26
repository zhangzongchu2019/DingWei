# M5 usage adapter 与去抖用例 v1

命令：`go test ./internal/mcs/rotation`。

| ID | 前置/输入 | 操作 | 期望与判据 |
|---|---|---|---|
| U1 | 首个完整 known sample | Apply | accepted 并建基线，不触发 |
| U2 | seq 重复/倒序 | Apply | stale，状态不前进 |
| U3 | 身份缺失、token 越界 | Apply | invalid |
| U4 | numerator/max 缺失或 unknown | Apply | unknown，三禁 |
| U5 | model/compaction/max 变化 | Apply | 分别返回三类 rebaseline，连续计数清零 |
| U6 | 连续两次 75% | Apply×2 | 首次不触发、第二次生成一个 rotation_id |
| U7 | 单次 85% / 95% | Apply | 均触发；95% 标 emergency |
| U8 | 已存在 pending rotation_id | 后续高 sample | 不生成第二 rotation_id |
| U9 | 75%计数 JSON 保存/恢复 | marshal/unmarshal 后再 Apply | 第二次阈值仍触发，重启不丢计数 |
| U10 | int64 极值 | 阈值比较 | 无浮点误差或整数溢出 |
