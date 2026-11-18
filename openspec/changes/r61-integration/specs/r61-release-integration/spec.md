# R61阶段集成

## Purpose
本变更把冻结单项的独立读数转为固定合并源上的阶段验证，目的是让顺序、冲突处置、异常尝试和环境条件可独立核查。涉及集成分支与测试证据，不影响单项产品合同，也不授予版本抬升、现网或预发操作权限。

## ADDED Requirements

### Requirement: R61I-001 冻结顺序与可追溯合并
集成者 MUST 从批准main基点按已冻结七项顺序各建一个no-ff合并提交，每次合并前 SHALL 核远端tip全值，偏离派单立即停报，不自动选择浮动新锚。

#### Scenario: R61I-S001 七项按序合并
- **WHEN** 七项远端tip均与派单相同
- **THEN** first-parent保留七个顺序明确、各含对应来源父节点的merge，记录核验UTC与冲突处置。

### Requirement: R61I-002 浏览器刀区语义保留
相邻刀区合并 MUST 同时保留unhandled-group-action、legacy-group-deadlock和silent-group-action-feedback，白名单 SHALL 使用429cf1dd批准集合，不因自动合并静默丢刀。

#### Scenario: R61I-S002 邻接hunk自动合并
- **WHEN** 两处复活通过ort自动合并
- **THEN** 仍检查三分支存在、白名单逐字一致，并运行浏览器全量及其变异判据。

### Requirement: R61I-003 固定源门与诚实证据
本段 SHALL 固定SOURCE_COMMIT运行canonical两profile、m8/store三遍、浏览器全量和strict；指定codex guide格 MUST 绿。失败尝试与SKIP MUST 逐名保留，不能以单跑覆盖完整门或将环境失败记绿。

#### Scenario: R61I-S003 磁盘不足后恢复
- **WHEN** canonical因临时目录空间不足而失败
- **THEN** 保存原rc/UTC/SHA与未完成范围，恢复环境后在同源重跑完整门，分开报告。

### Requirement: R61I-004 阶段边界
第一段 MUST 保持VERSION且不提前合入尚未冻结的N-3/N-1b/N-1c；作者交件 SHALL 不代替独立复核、最终集成裁定或发布授权。

#### Scenario: R61I-S004 第一段交件
- **WHEN** 七项作者门已完成
- **THEN** 只推集成分支和证据，不推main、不部署、不制作发布件，后续项另待冻结令。
