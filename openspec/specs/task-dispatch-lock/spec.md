# 按任务编号派单最小基线

## Purpose

本变更把旧数据库任务分配与计划中的仓库编号锁从容易混淆改为明确的最小基线，目的是防止把旧UUID、角色分配或通知回执解释为OpenSpec固定任务已获互斥租约。涉及Console任务流、仓库读取与版本比较模块，不影响既有任务进度及部署流程。

依据2af469dd源码；不将旧Spec文档当成当前Git任务锁的实现证据。

## Requirements

### Requirement: R10-B001 旧任务分配与仓库编号锁分离
平台 SHALL 保持旧数据库任务UUID/assignment_version与仓库变更T编号及Git commit语义分离；已有R5只读与R7比较不代表已存在按编号互斥租约或交接治理。

#### Scenario: R10-B001-S01 旧分配按原来源读取
- **WHEN** 读取只有UUID和assignment_version的既有任务分配
- **THEN** 显示其旧来源，不据此生成仓库T编号有效租约或派单版本

#### Scenario: R10-B001-S02 读取和比较不等于授予
- **WHEN** 只有固定commit的R5读取或R7比较结果
- **THEN** 不将这些结果解释为持有者已获锁、任务已完成或已发布
