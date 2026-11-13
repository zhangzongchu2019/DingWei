# Context

来源：2026-11-13归档的view-unknown-ux、view-unknown-ux-followup、view-stale-backlog，按实现顺序合并。主规格包含原未知文案/确认、DOM稳定与计数、30天跨刷新本地整理与500条上限。后续持久化变更将原单页内存整理扩展为浏览器本地标记，不改变Hub投递事实。

实现：view-service/renderer-canary-sse.html；回归：view-service/renderer-delivery-status-regression.js，已登记统一浏览器runner。证据：docs/reviews/view-unknown-ux、view-unknown-ux-followup、view-stale-backlog；完整Git坐标见docs/reviews/r59-closeout/TABLE.md。测试用既有Node22与Playwright/Chromium，未增加依赖。Hub expired_unknown仍为草案，未随归档实现。
