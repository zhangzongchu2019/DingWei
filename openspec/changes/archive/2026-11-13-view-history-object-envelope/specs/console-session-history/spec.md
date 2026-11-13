## ADDED Requirements

### Requirement: VHO-001 历史对象与字符串信封兼容
View MUST 接受历史行event的JSON对象，并兼容已序列化的JSON对象字符串；有效记录 SHALL 沿用既有历史元数据与事件处理流程。null、数组、非对象与无效JSON MUST 按行跳过并告警无效行数量，不输出历史正文，不影响同页有效输出回放。

#### Scenario: VHO-S001 对象和字符串输出可见
- **WHEN** 已授权历史首屏包含对象或字符串形式的assistant事件
- **THEN** 两种形式均按原cursor进入既有回放，直接View及Console iframe刷新/切换后仍显示有效输出

#### Scenario: VHO-S002 无效行不吞掉整页
- **WHEN** 同页有效对象与null、数组、非对象或无效JSON混排
- **THEN** 页面跳过无效行并告警数量，同页有效输出仍可见；告警不包含正文
