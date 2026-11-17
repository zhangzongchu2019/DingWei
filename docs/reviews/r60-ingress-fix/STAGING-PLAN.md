# 旧39 helper与新Hub的预发补测计划（不含执行授权）

唯一候选源码a24bb16335040ae06486f1c76be69ee542958214；完整作者门、组长及独立复核后，构建/升级/部署仍须另令，作者本地产物不上传、不交prodops作发布件。本计划不操作预发或现网，不另建席位。

1. 优先使用现有stagingtest。先记录其0.2.3.24基线、连接序号、Hub/renderer/数据库版本、已授权回退件与0077安全闸。24读数不能代替39。
2. 组长另令且业主在预发Console点一次，将stagingtest升到批准的0.2.3.39；r59升级包已知可用至2026-11-20T06:16Z。若验证晚于期限，须由组长另核包可用性，不自动换包、不由本席上传。核实际helper握手版本39/对应源码2e1e4c9d、独立IPC和私有状态目录，禁止同名双实例。
3. 经构建/布置令后钉定新Hub制品SHA/源码/工具链。启动前检查candidate参数2048/65536项、32/256MiB raw，写槽2s/小控制5s/数据20s；保活仍30s/5s。正式构建必须显式设置隔离DW_DESKTOP_INTEGRATION_PYTHON并核联合格/双CLI逐名PASS、0SKIP，不能只看BUILD_TESTS_OK。
4. 仅在该测试席重放R-D01三轮：原39 session_event_loop每轮1000×4KiB；独立事件ID/正文SHA、历史游标、500/501之间ACK事务提交屏障及正文顺序逐项对账。每轮真正排空后再开始，不把helper进程存在当作投递完成。无未知session_end，连接序号不能无故递增。
5. 重放R-D02小控制与8MiB各三轮：停读3秒后恢复，保留实际调用方5秒预算，核原JSON ID/正文SHA、连接保持；小控制可能先写入TCP buffer，只断言恢复后完整收到，不声称小写本身阻塞3秒。
6. 39持续运行至少10分钟，覆盖默认首Ping及后续Pong。叠加慢/停读viewer、持续输出及长业务，核只有viewer隔离、gap/readonly离线通知和正常关闭码保留；正常helper不被业务饿死。真无Pong/长停读负格仅在此获准测试连接，须有keepalive失败与session_end固定原因，不能以禁用保活换取连接稳定。
7. 经额外确认的本地限速注入重放10Mbps/8MiB/Ping竞争，保留同一JSON消息的32KiB WS片段、单帧峰值、控制帧延迟及完整SHA。无需在现网灌流。22席并发先用本地隔离生成器；未经另令不建22个预发席。记录raw/堆/RSS各自口径；256MiB不是RSS硬界，回环值也不是公网SLO。
8. ①恢复需要真实40 helper能力。39阶段完成后，另令将同一测试席升级到40或指定现有40测试席，不自行新建。重放unknown→dw_send_status→同source/delivery恢复和同request-id双CLI，Hub只有一条投递；旧39无新outbox，不能记为该格已覆盖。
9. 观测session_end原因/peer code/reader与worker/Pong age、overflow/物理写原因、两类控制写失败计数、DiagnosticDrop、caller库/outbox行数以及窗外未终结fence。日志有200条/s、4096字节和非阻塞丢弃界；缺日志不单独证明没有故障。
10. 立即停止：非预期断连、事件/正文缺失或乱序、ACK早于提交、同ID重复投递、unknown不能恢复、计划关停被记为keepalive失败、敏感信息进入日志、sink阻塞、预算/释放失败。不要凭新日志猜出历史五断点的唯一根因。
11. 回退仍先过0077安全闸：全部未终结投递加failure_notice为0，helper local_queued/unknown为0，停流入和写者、逐项对账后才可用同一最新库换批准旧exe。不得裸换旧exe继续派发，不删除0077，不默认恢复开场备份。安全闸非0即停止并报组长。
