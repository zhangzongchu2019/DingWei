## Why
r57 已上线且 main 已归档结项，r58 的五项冻结变更需要依次整合到新主线，并验证各自局部通过之后的组合行为。helper 测试库存和动态用例数在组合后改变，hosted 并行夹具与 socket 临时目录也需要在同一最终树上复核，才能提供可追溯的发布候选。
需求来源：opus50tc2k.zzc 派单 S-R58-INTEGRATION（2026-11-12），rulings ㊿-补92 fcd8b46e、补93 78615d1b 与五项冻结。

## What Changes
按序五个no-ff merge，单独记录集成计数钉值，固定最终tip后执行全部指定门；只作集成与证据，不改版本号、产品设计或部署。

## Capabilities
### Modified Capabilities
- release-quality-gates：r58组合验证与证据。

## Impact
现有五项冻结代码与测试、集成文档；无新依赖或部署授权。

## 待确认假设
无。runner expected仍由独立discovery生成，不引入新硬编码。

## 待 PM 决策
无。
