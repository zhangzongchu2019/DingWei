# R52 trusted origins implementation

## Why

Console 写接口当前以规范公网来源作为唯一浏览器来源，反向代理在同一应用前提供额外 HTTPS 端口时，浏览器会因 Origin 端口不同而被拒绝。本变更按已批准设计增加精确 HTTPS 来源清单，同时保持规范公网地址负责持久化和对外链接。Console 上传票据与 View 文件渲染也需要在可信入口端口工作，并在发送前恢复规范文件地址，避免把入口地址写入持久内容。

## Source

- Trusted-origin policy: `review/togpt60lunazzc/r52-trusted-origins-design-20261105` @ `c952307d71df16c570f61e0e6e2d3edd80e9fe40`, `docs/design/S-R52-TRUSTED-ORIGINS-DESIGN-20261105.md`.
- Implementation assignment: `S-R52-TRUSTED-ORIGINS-IMPL`, branch `togpt60lunazzc/r52-trusted-origins-impl`, baseline `d555768c102b04caac3a1f05afcff4a55832cc13`; the implementation scope was confirmed by the task owner before this work.

## Scope

Add strict parsing and request-time matching for explicitly trusted HTTPS origins. Preserve every existing endpoint predicate when the trusted list is empty. Use an exact canonical or trusted request Origin only to select the browser upload ticket URL. Keep file-service and stored file URLs canonical; let View display canonical file links through the current page origin and normalize them back before sending.

## Out of scope

Changing canonical PublicOrigin, cookie attributes, CORS, Agent WebSocket origin policy, proxy headers, other public links, or the identity/session model.

## 待确认假设

- 无。来源清单格式、空清单兼容及浏览器文件地址范围均按上述设计和已确认派单执行。

## 待 PM 决策

- 无。
