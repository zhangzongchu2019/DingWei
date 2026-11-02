# PM决策请求最小基线

## Purpose

本变更把旧规格澄清、既有飞书私聊与计划中的OpenSpec决策请求从容易混淆改为明确的最小基线，目的是防止把发送成功、旧问答或一个PMResponse数据结构误认为固定Git版本的双渠道决策已闭环。涉及飞书、Console及变更证据模块，不影响现有消息和部署流程。

基于2af469dd源码，不以旧Spec材料导入新Git决策。

## Requirements

### Requirement: R9-B001 既有私聊及旧回应不等于Git决策闭环
平台 SHALL 区分旧Spec澄清、普通私聊和固定Git仓库/commit/PM点请求。现有消息回执或R8 PMResponse输入不能单独证明新请求已经明确回应、另一渠道已关闭或MR评论已写入。

#### Scenario: R9-B001-S01 旧消息按原来源读取
- **WHEN** 读取仅有旧Spec修订号或普通私聊message_id的记录
- **THEN** 不自动为新Git commit的PM点生成有效回应

#### Scenario: R9-B001-S02 接口类型不证明真实写回
- **WHEN** 只有PMResponse值或UpsertComment接口签名，实际提供方未接线
- **THEN** 不据此显示真实双渠道闭环或MR写回完成
