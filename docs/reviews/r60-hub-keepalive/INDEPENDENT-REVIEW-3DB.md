# qwen 对②最终3db产品的独立复验入账

记录时刻：2026-11-17T01:00:12.979716+00:00。只记录他席结果，不冒称作者代跑，也不替代组长终审。

## 锚与取证范围

- 产品/测试：3db78e5b244c4663fe67f7dcd44b74991057a7f6。
- qwen复验文档：b17593fbe8563cf1cff6ee4db1b9d4727b53ced5；其结论不外推到旧167/eca/984或未来产品。
- qwen报告：review/qwen38mvb4/r60-hub-keepalive-20261116@adcb1d649e8f709f030f9e2c603f99a6427a4785，tree219ecb9b7ae2bb0b0cfbeab2f16d05cb0e6280f8，parentd747a5145dd8ed608e22b66814211bda3f4fe46f。
- 已独立fetch并核Git：相对main只有该报告目录5个路径（含SHA256SUMS）；清单4个被校验文档的Git对象SHA全中，旧REPORT/探针对象未变。LOG-SHA256.txt仅记录qwen本机原日志指纹；作者未取得其原始日志字节，不声称重算了这些原日志SHA，也没有复跑它的测试或变异。

## 本轮判据的独立结果

qwen给spec PASS、code PASS，Q1/Q2两阻塞闭合，Q3/Q4/Q6有独立刀证，Q7依令只观察。实际Ping飞行中取消、离线状态/readonly/1000关闭码、写槽共享与无Hub主锁网络写、queue_items与年龄口径、两个计数器各自独立，均由其读码或业务删保护断言核查。

其14有效刀为10 KILLED/4 SURVIVED；W13是锚串语法错误导致编译失败，无效，不计入14。与作者28刀分列，不相加。独立定向m8、obs、store SessionHistory、cmd Keepalive、四包build、focused race（16顶层/4子项/0FAIL/0SKIP/0DATA RACE）及strict36/0通过。vet7条composites归既有未改文件。未独立复现canonical、PG strict、浏览器、identity、全包race、PM04夹具和作者28刀；①②完整helper/outbox联合格仍待正式集成，未被本PASS替代。

## Z-1、Z-2非阻塞，Z-3另请组长裁定

来信标题把三项统称非阻塞；Git报告§九更严格，明确Z-3单列不并入非阻塞、请求组长决定处置。作者按这一边界记录，未据标题自行接受风险或宣称最终冻结。

- Z-1：离线清理防御分支四刀存活，分别是封口回退gap、封口CloseNow、待显队列字节gap、q.stopped；现代码保护在，但未逐条钉住。qwen判非阻塞覆盖缺口，本次不自行修改规格/产品或补探针。
- Z-2：控制写失败测试在全局obs.Default上做精确+1差值，理论上可能受残留工作影响；qwen本轮未复现这种失败，非阻塞夹具风险，不写成已证竞争。
- Z-3：m8整包在本锚三样本rc=1/1/0，interrupted(9)行数7/5/0。增量前167三样本均绿/0行，main d2两样本均绿/0行；main+③另5份日志0行仅旁证。失败用例为TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent，以及TestViewMentionRoute/a_peer_without_delivery_ack_v1_is_not_sent_to。两例隔离单跑通过，不能覆盖整包rc1。样本1起始约7秒与npm strict重叠，样本2零并发仍失败，不能归为机器跑忙。
- qwen推断既有单连接SQLite/取消带外interrupt可能被167..3db增量放大，怀疑离线清理并发；未驱动插桩、未二分、未证实因果。小样本不推失败率，不能定性为环境噪声、与本单无关或已证回归。报告§六最新扩展表为167三样本/main两样本；不沿用§九残留的各一例文字。其统计估算不是作者实验，不把显著性阈值当排除问题的证据。

## 冻结与集成限制

组长23:02:43Z已对3db通过并附qwen通过后冻结条件；本次独立PASS已收到，但新增Z-3处置待组长裁定。作者已上报dw-dlv_52d82ecfcdca2c7d060a84990f79f348，不自签风险接受，不自行扩大到store根治/额外样本/临时探针，不改FAIL为SKIP或改夹具涂绿。此后若有处置令再按新授权执行。

当前①产品b48/交件81a已获组长通过，qwen独立复核待回；③899f及④4e421abea已冻结。正式C仍110e2c1f/.39，须①②冻结齐后按③④①②重做，不以旧12f/167预合读数替代。正式门需完整四树Go并逐名保留上述两例结果与所有失败；不以单跑抵消。完整①②慢读/背压→可观测unknown→dw_send_status以原sender/source/delivery恢复联合格及删保护红仍必须落地。本次仅文档，不合main、不出发布件、不部署。
