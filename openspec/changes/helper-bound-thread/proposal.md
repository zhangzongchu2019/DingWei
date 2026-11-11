## Why
2026-11-11 的两张 prodops 单未送达持有目标工作树的会话，coder56 也曾因首封信新建会话而更换写者。当前 helper 根 driver 不能由操作员选择既有 thread，使投递席位与工作会话脱节。本变更让操作员明确绑定根 thread，并在不可用时可见失败，避免把同一任务静默投到新会话。

需求来源：opus50tc2k.zzc 派单 S-HELPER-BOUND-THREAD，业主裁定㊿-补88 e879d0cb（2026-11-11）；基线 329eb9f9c45d2d785a0429b01eaa0fa5264fe15d。

## What Changes
- 根 Codex driver 支持 SH_CODEX_BOUND_THREAD_ID，严格校验指定 thread，不创建替代 thread。
- private 旧绑定冲突时拒绝启动并显示双方 ID。
- 保留未配置行为、M2 子 lane 绑定和 broker 白名单；提供操作文档与隔离验证。

## Capabilities
### Modified Capabilities
- helper-delivery-evidence：显式根绑定与失败回执。

## Impact
仅 helper 配置、driver、adapter、子分道根 driver 重建及测试、文档；不改 Hub、版本号、发布构建或部署。

## 待确认假设
无。配置空值按未设置处理；private 换绑维护不新增自动覆盖接口。

## 待 PM 决策
无。上线随 r58 helper 包，试点与其他变更合批由 prodops 另行执行。
