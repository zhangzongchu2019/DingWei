# 风险提醒最小基线

## Purpose

本变更把现有通用风险记录与 OpenSpec 风险提醒的边界从隐含理解改为明确的最小基线，目的是防止将旧风险条目误认为七类规则、固定 commit 判定和双端提醒已经具备。涉及模型、Console 与仓库接入边界，不影响当前合并与发布流程。

依据 dfc95fd8 源码建立，不声称已核现网；新需求见本能力对应变更。

## Requirements

### Requirement: R1-B001 通用风险记录不冒充仓库风险判定
平台 SHALL 区分现有通用 Risk 的内容/状态/关联任务与新仓库、commit、规则版本、命中依据；缺少后者时不得宣称已经完成 R1 七类识别或 MR/Console 双端提醒。

#### Scenario: R1-B001-S01 保留原风险含义
- **WHEN** 读取仅有 Content、Status、RelatedTaskKey 的原 Risk 条目
- **THEN** 仅按原风险记录展示，不生成仓库固定版本的识别或双端投递事实

#### Scenario: R1-B001-S02 不能用已有条目伪报提醒完成
- **WHEN** 新 MR 的指定 commit 没有规则判定和投递记录，而数据库有同名风险条目
- **THEN** 不标该 commit 已识别、已知悉或已在两个渠道提醒
