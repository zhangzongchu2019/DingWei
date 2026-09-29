# 夹具真源 · Hub 自发的「错误看板」事件

`hub-failure-dock-event.json` 是 **Hub 侧代码产出**的那一串,**逐字节**。

🔴 **不要手改这个文件**,也不要"顺手补一个字段"。它的生产者是
`internal/m8/terminal.go` 的 `publishFailureDockEvent`;
真源闸是 `internal/m8/failure_dock_nodisplay_test.go` 的
`TestFailureDockEventMatchesRendererFixture`(逐字节比对,漂了就红,红文案里带 got/want)。

消费方:`view-service/renderer-failure-dock-regression.js`(读文件,不抄形状)。

为什么用夹具而不是让 JS 自己写一份:两份形状必漂移(本仓已在"两份清单各说各话"上吃过亏)。
