# S-R60-INGRESS-FIX 检查点（不是最终交件）

依据组长2026-11-17T18:04:11Z令，基9818b979独立实现；未改r60-integration/main，未构建发布件、未上传、未操作预发或现网。冻结评审尚未开始。

源码检查点：`0b1c1dfbaa96aabc6b92cb24bff6fc618b3db8db`，parent `9818b979e9e20f0bcfeffa6b78045dda71b1bd79`；本提交的tree可用git rev-parse独立实取。VERSION仍0.2.3.40。只触碰internal/m8与新增openspec；cmd/tools/scripts/deploy/view-service/go.mod/go.sum不变。

实现：每连接2048、全Hub65536项有序FIFO，32MiB/256MiB raw字节界不变；等槽2s、小控制物理5s、大数据20s，更短父ctx仍先到期。每连接session_end、overflow/物理失败各至多一次，固定枚举、单行4096、每Hub200条/s、非阻塞出口与drop计数。独立Reader不等待业务；不做显示合并。

条件分片已满足：真实旧39、10Mbps/8MiB/3s停读与Ping竞争，不分片业务断言红；同消息32KiB片段后正文SHA一致、峰值32768、连接保持。不是生产SLO，也不说明现网5次断连已逐条定因。

## 检查点读数

- 真实旧39原session_event_loop：1000×4KiB完整入历史、唯一ID/顺序/正文逐项核对；500/501间ACK事务屏障与正文屏障均成立。
- 真实旧39停读3s：8MiB大数据与小控制各通过；小控制可能先进入内核buffer，只将恢复后实际收到当作证据，不宣称小写本身阻塞3s。
- 22个Go回环负载连接×1000条：raw约89.4MiB、Go堆驻留约109.6MiB，排空后预算归零；不是22个真实helper/模型，也不把raw上界当RSS硬界。
- 新增28把删保护：编译成功且业务断言红；恢复源码后基线rc0。恢复64、恢复物理2s、破坏FIFO、提前ACK、移除分片、资源/释放、诊断字段、Z-1四处和Ping中取消均有原始日志。
- focused race：38顶层、含子项48 PASS，0 FAIL/0 SKIP/0 DATA RACE；包含显式隔离Python下①②联合格与同ID双CLI格。
- SOURCE_COMMIT build.sh：rc0，canonical两profile，Python27/shell2，inventory97。产物仅作本机门读数，不作发布件。
- 全仓OpenSpec strict：41/41。

## 仍待完成

原②全部删保护新源重放、R-D03～R-D07完整收口、四树PG strict、Z-3固定新源五轮串行m8、浏览器全量、最终新锚/独立复核。不能将本检查点当最终PASS或部署GO。Z-3仅按01:37:18Z新门，不引用作废的“任一interrupt即停”。

早期编译/夹具错误与无分片有效反例均保留在本机artifact，不将其改绿或计作删保护。最终报告另列原始失败与最终源结果。
