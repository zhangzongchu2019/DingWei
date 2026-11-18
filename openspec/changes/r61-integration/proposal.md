# Why

组长2026-11-18T10:20:18Z派单S-R61-INTEGRATION第一段，要求先集成七项已冻结修订，以固定合并锚重新运行完整门，避免单项读数被误用为组合验证。N-3、N-1b、N-1c仍待独立复核，不能提前合入。本件仅记录集成顺序、环境与证据责任，不新增产品功能或部署授权。

## What Changes
- 基main206d41b2按派单七项顺序分别no-ff merge。
- 保留相邻浏览器变异刀及末项白名单，保持VERSION。
- 固定SOURCE_COMMIT执行本段门并写可核Git证据。

## Capabilities
- New capability: r61-release-integration（集成和证据规则）。

## Impact
只增加集成文档与验证证据；产品变化由已冻结七项决定。

## 待确认假设
无新增假设；未冻结三项仍不纳入。

## 待 PM 决策
无；正式版本与发布由后续命令另定。
