## Why
健康 Agent 正在进行旧回合时，Claude 的新信仍使用派发时间计入场超时，60 秒即变成 unknown；Codex 虽区分实际写出，但等待缺少上限与可见状态。本变更让确知未提交的新信有界等待旧回合，并在超限时先阻断晚到入场，保持现有未知投递不自动重放原则。

需求来源：DingWei 派单 53812402782b；开工令 source_message_id=6cff6cb58e0842ae8c23b220e0b26731，入箱 request_id=dw-dlv_9a06e6f79a310aa29380979352cdc2d1；基线工蜂 wego-ai/dingwei refs/heads/coder56solzzc/helper-recover-inherit-thread c0777e4b0d26cabc588f22bdf3ae7ce90d412110。入箱未保存发送端 request_id，以上不冒充发送端编号。

## What Changes
- 两种协议驱动确认旧回合仍执行时报告本地等待；adapter 暂停原入场期限，空闲后仅提交一次。
- 配置 SH_BUSY_ADMISSION_MAX_SECONDS 默认1800秒；超限先原子取消未提交等待者，再记录 unknown 与固定告警。
- 经既有 state_change/data.change 通道输出等待状态，不改Hub。
- 传输失活、写入卡住及已可能入场继续走原超时/恢复及只读对账边界。

## Capabilities
### Modified Capabilities
- helper-delivery-evidence: 忙等待有界、状态可见且不造成晚到或重复入场。

## Impact
修改 helper、配置模板及现有测试模块，并让View消费等待状态显示明确文案且保留原输入关联；不改 VERSION，不启动真实协议/账号，不发布、不部署、不重启。

### 待确认假设
无新增待确认假设；等待依据协议驱动的明确旧回合状态，不猜测 View 或进程忙闲。

### 待 PM 决策
无；1800秒默认及超限unknown已由派单明确授权。
