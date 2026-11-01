# 变更生命周期最小基线

## Purpose

本变更把旧数据库规格、任务和新 OpenSpec 版本的完成语义从容易混淆改为明确的最小基线，目的是防止把旧 final、任务勾选或已存在的确认存储误认为七阶段证据状态机已经实现。涉及旧流程、新确认服务及验收记录边界，不影响现有业务数据与发布事实。

依据 dfc95fd8 源码建立，不把旧 Spec 当作当前 Git 能力的运行基线。

## Requirements

### Requirement: R8-B001 旧完成状态不代表新变更生命周期
平台 SHALL 区分旧 Spec 修订/定稿、旧任务执行与仓库 OpenSpec 的七阶段状态；已有 R7 确认事实不自动成为验收、归档或发布事实。

#### Scenario: R8-B001-S01 按原来源读取旧状态
- **WHEN** 读取旧 Spec final 或任务完成记录
- **THEN** 保留旧来源与编号，不自动映射到新变更的 SPEC_APPROVED、ARCHIVED 或 RELEASED

#### Scenario: R8-B001-S02 只有确认不能宣告完成
- **WHEN** 仅存在绑定 commit 的 R7 确认，缺任务、场景或发布证据
- **THEN** 不自动记录新变更已实现、验收通过或发布
