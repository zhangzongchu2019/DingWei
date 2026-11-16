# Proposal

## Why

组长转述业主monitor截图：现网session关闭487次，其中419次标为keepalive_failed，已成为断连的主要关闭类别。源码中唯一Reader还同步处理终端viewer、存储和路由，库的Ping却依赖该Reader消费Pong，健康helper也可能因此被错判。需要把业务阻塞与协议控制帧读取分离，并给慢viewer、队列和写库等待设定明确责任与可见故障边界，避免只加长超时掩盖阻塞或改坏可靠投递ACK语义。

## What Changes

- 新增hub-session-keepalive能力，8条ADDED要求：单Reader与业务解耦、有界队列、viewer隔离、I/O预算、长输出控制帧通行、失败journal、配置及回归/生命周期。
- 现状keepalive参数无env/启动参数入口，不能只改配置缓解。本提案可增加经审定的正值配置，但不把放宽超时作为根治。
- 本次仅分析/规格/离线观察探针，不改产品、不构建制品、不连接现网、不部署；根治与S-SEND-LOST分别审案，联合集成验证。

## Capabilities

### New Capabilities

- `hub-session-keepalive`：保证可服务负载下业务处理不饿死WS协议读取，失败有可定位、无正文的日志；最小原态见specs下context.md，ADDED尚未实现。

### Modified Capabilities

无。Hub生命周期与helper-delivery-evidence的持久/原生证据边界继续有效，不能借入队或Pong合成业务ACK；S-SEND-LOST在aaa3f893的冻结设计不被本件替代或修改。

## Impact

后续实现预计涉及internal/m8的session read/worker、terminal viewer写、touch与history调用、内部观测及cmd/workpulse配置装配；如完整解决超大输出帧/控制帧竞争，可能需要helper端显式分块/公平发送，必须单列范围与测试，不冒充纯Hub改动。本单无数据库DDL或新生产指标部署。视图降载仅限可回放的显示层，session_event兼有AMR/终态业务语义，不能整类丢弃。

## 来源

组长opus50tc2k.zzc于2026-11-16T14:57:50Z在本会话派S-HUB-KEEPALIVE-FIX（原delivery_id缺，不补造）。监控数字是组长转述业主/console/monitor截图，Hub锚329eb9f9；本席没有直连现网或取得原截图文件。前序只读证据：[S-FLAP](https://git.code.tencent.com/wego-ai/dingwei/blob/60e9a80c179432bce15695522d0b6767064927c9/REPORT.md)；[S-SEND-LOST冻结设计](https://git.code.tencent.com/wego-ai/dingwei/blob/aaa3f893626004e0140ef7a67316b692a8506b18/openspec/changes/helper-send-lost/design.md)。不从Git作者推断授权。

## 待确认假设

- HKF-A01：419次确属monitor keepalive失败类别；具体每次错误为等Pong超时、写Ping失败、EOF或取消，当前聚合没有区分。低p95只统计成功样本，不能直接排除失败时段事件循环停顿。
- HKF-A02：现网各失败当时是否挂真实legacy viewer、SessionHistory开关和DB等待情况尚缺。离线注入证明可达路径，不宣称全419次均同源。
- HKF-A03：目标网络和CPU下可服务输出峰值/帧大小需实测后定；有限队列不能承诺任意洪泛下不关闭连接或不产生显示gap。

## 待 PM 决策

| 编号 | 选项 | 建议 |
| --- | --- | --- |
| HKF-PM01 | 仅增加保活超时；或先解耦Reader/隔离viewer/I/O | 推荐后者；加超时不解决无界等待。现网没有配置入口，不能假发env临时改值 |
| HKF-PM02 | worker队列/显示队列预算 | 候选每会话64项/32MiB、全Hub256MiB；viewer每个64项/1MiB，单次取写权＋写入总1秒；需按22活跃及突发规模测，不当上线定值 |
| HKF-PM03 | 保活配置默认与诊断性放宽 | 建议先保留30秒/5秒默认；若单独批准诊断性30秒/15秒须先加配置入口并重建/重启，不能解决库内控制写5秒或无限业务等待，半开检测变慢 |
| HKF-PM04 | helper分块是否同批 | 若峰值下单帧/输出背压实测影响控制帧，则同批加入明确字节分块与公平调度；否则作为后续单独验证，不以0.12秒批窗口当字节界 |
| HKF-PM05 | 发布与SEND-LOST | 两项分别实现/评审/回滚，建议同r60 release set联合验证；发送持久责任不能因断连减少而省略，是否同日上线由业主/组长定 |
