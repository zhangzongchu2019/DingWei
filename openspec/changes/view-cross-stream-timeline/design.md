# Design

## 决策

- 所有可排序的 timeline 子节点经同一 `placeTimelineNode` 定位。两节点有 seq 且同 epoch 时只比较 seq；其他有时间戳的组合比较首个可见 `.event-time[data-ts]`。缺少时间戳时才按可用 epoch/seq 键稳定回退。
- 对刚创建、尚未重建内容的回合组暂用事件时间；回合组 HTML 重建后重新定位，读取实际可见的首个事件时间。
- 浏览器回归断言整条 timeline 相邻节点时间单调不减，并单独断言七张输入卡各自处于相邻回合组之间；移除跨流时间比较必须在单调性判据上失败。

## 影响

只改 View renderer、浏览器回归与 `console-timeline-order` 规格；不改 Hub 协议或输入投递生命周期。
