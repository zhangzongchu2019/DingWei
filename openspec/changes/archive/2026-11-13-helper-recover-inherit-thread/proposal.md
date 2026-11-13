## Why
现役常驻 Codex 守护进程在入场超时后可能仍保留旧线程，adapter 重建未绑定驱动却丢失根线程号，后续投递会静默新建根线程，破坏同席任务上下文连续性。需要优先继承并校验旧根，只在确认旧线程不可用时按授权显式告警换新，同时保持绑定与未知投递边界。

需求来源：[S-HELPER-RECOVER-INHERIT-THREAD 派单记录](../../../docs/reviews/helper-recover-inherit-thread/REPORT.md)，opus50tc2k.zzc，2026-11-12，㊿-补103；基线 3c9f2722fd5b2e05b52188a8ccb664427f5debdd。

## What Changes
- 恢复替换驱动继承当前根线程，首投前沿用 read/resume/cwd 校验。
- 未绑定根校验确认不可用时允许可观测换新；绑定与子分道保持拒收。
- broker 根映射和 private 当前绑定受控更新，不放宽子分道准入。
- 触发恢复的原信保持 unknown、不自动重放。

## Capabilities
### Modified Capabilities
- helper-delivery-evidence：恢复后的线程连续性与显式换新。

## Impact
仅 helper driver/adapter、子 Agent runtime/broker、private 绑定维护、测试与文档；不改 Hub、VERSION 或部署。

## 待确认假设
无。传输/认证等未知错误不等同旧线程确认不可用，不自动换新。

## 待 PM 决策
无。本单交 r59 候选，部署另授权。
