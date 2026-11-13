# Design

## Decisions
VHO-001：parseHistoryEvent 对字符串尝试JSON.parse，对对象浅拷贝；null、数组、非对象与无效JSON返回null。历史循环为有效事件附cursor元数据，经既有handleIncomingEvent处理；无效行计数并仅输出数量告警，不输出正文。
VHO-001：沿用既有历史去重、授权、游标和实时切点，未改变Hub接口。正反证据为真实脱敏首屏夹具22事件及对象/字符串两项变异；本次只引用已有回放，不将真实流量抓取设为后续每次修复的固定要求。
