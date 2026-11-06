# Tasks

- [x] T-01 复核最小基线与Hub影响清单：r51 f7f31fca；身份、名称、在线、授权、np_group/np_agent、投递及ACK；未核实处保留标记。
- [x] T-02 同步业主㊹、补1～补6裁定：扫码＋tenant_key、归属名/稳定键、AG序号、组合名、既定传输参数、Hub改造、Console、14天/3天滑动周期；原等待业主六选项已替换；补5按无状态MCP、ACK重投和可选订阅/私有WSS分期同步。PM-GW-01仅轮询不续AG已由业主确认（㊹-补6），此项已完成。
- [x] T-03 完成替代管理员预建的扫码身份、标准OAuth、自动双成员AG、持久命名/续期/吊销/解散设计；这是文档完成，不代表产品实现。
- [ ] T-04 实现飞书网页扫码、本团队tenant校验、稳定主体、OAuth metadata/CIMD与2026-07-28无状态MCP：server/discover、每请求_meta/头/resultType及四工具，验证对话式/自主循环互操作，不依赖initialize/Mcp-Session-Id。
- [ ] T-05 实现同权REST四操作，scope/audience、Origin和身份隔离；不增加执行/管理权限。
- [ ] T-06 实现持久AG sequence、external命名和全名适配、双成员事务、普通路由/AMR、session_delivery及HTTP ACK v2适配；断流新RPC请求重取未ACK、按稳定delivery去重与发送幂等，不用SSE续传；保留helper/driver回归。
- [ ] T-07 实现令牌14天持久滑动续期、90秒在线、AG3天生命周期、启动补扫与分批清理、两侧outbox通知/在途结算、消息24小时到期、限流/审计/吊销中断；实施前冻结节流/跨库方案。
- [ ] T-08 安全与功能矩阵：本租户/他租户/缺tenant，英文/拼音/同名不同主体，多目标AG并发/重启唯一号，非法短名，14天/3天/90秒/24小时边界，吊销并发、回拨、持久写失败、ACK/队列、OAuth/REST等价、仅轮询不续组（已定，㊹-补6）、原helper回归。
- [ ] T-09 逐项语义变异：删tenant/编组检查、按显示名合并、序号非持久、非法短名放行、删除续期持久写（重启提前失效）、轮询错误续组、删除吊销/Origin/ACK绑定/等待上限/过期栅栏、扩任一面权限；每刀由对应原判据杀死。
- [ ] T-10 后续实现跑构建/相关race/strict与标准MCP互操作，完成迁移/备份sequence/回滚方案，另行独立验收和发布；本次文档门原文见validation/。
- [ ] T-11 实现Console Agents下相关AG侧栏与属性页；显示两成员、创建者归属名、创建/最后使用/预计解散，相关主人或平台管理员现核＋二次确认＋审计解散；服务端相关过滤不以创建者/管理员侧栏豁免替代。
- [ ] T-12 Console两渠道浏览器回归：别人AG不出现、属性完整、双方主人/管理员可解散、无关人拒绝、取消无写、到期竞态/通知；新增套件登记统一runner，去掉只列相关过滤→别人AG出现用例必须红，删服务端解散权限亦须红。
- [ ] T-13 评估可选本人收件资源＋subscriptions/listen通知：权限隔离、通知不充ACK、断流主动收件；无订阅客户端保留25秒轮询。二期私有WSS仅在必要时另立项，若一期通知足够可不做；开发前再核定稿/SDK状态。

## 交接记录

- 2026-11-05，执行者togpt60lunazzc，原稿最终基线 f95fa0b3a4a6e693d21c8de4b052e3d3a7424293。
  - 完成：原始十条增量需求、基线上下文与任务；原提案单变更/全仓strict通过，无实现。
  - 阻塞（历史）：当时T-02等待业主；现已由2026-11-06八点裁定替代，不继续据历史选项派实现。
  - 下一步（历史）：原稿冻结，由本次改派执行者修订。
- 2026-11-06，执行者coder56solzzc，会话01a19ca0-0622-7743-a995-767c05e49c78；父commit f95fa0b3a4a6e693d21c8de4b052e3d3a7424293，当前文档提交以分支tip为准（避免自引用提交号）。
  - 完成：已读578a41fb138f、09ae257bb65b、7be46cee6bfb和改派；读取2577d91a/4accf167/5ed6d2ae/51133ad3裁定原文、r51代码与698b719e续期实现；八点及第9点追加同步proposal/design/context及原GW-001～010，不改固定需求编号、不增第11条；T-11/12补Console任务，T-13补可选订阅取舍；提交前补读第9点并自行查官方changelog/版本/传输/订阅/CIMD与RC博客/正式release，原文状态差异分别记录。目录外产品与其他派单未动。
  - 阻塞：PM-GW-01纯轮询不续AG已定（㊹-补6），此项无阻塞；技术假设与尚未核实SDK/OAuth/迁移/解散ACK映射明列，未做真实互操作或产品变异。
  - 下一步：单变更及全仓strict、文档范围检查和仓库要求的canonical构建测试均已完成（rc0，原文/摘要入validation/REPORT.md）；推送核ls-remote后交组长复核，不自行勾实现/独立验收。

- 2026-11-06，执行者coder56solzzc，A路会话01a19ca0-0622-7743-a995-767c05e49c78，实施基线262f4747；接口中间锚e261aaa5。
  - 完成：按㊹-补7/补8与R53-MVP-CONTRACT实现PG0105六表、external身份分支、逐交互令牌CAS、PG收件/received/outbox、Hub路由与presence消费者接线；初轮PG正反及四项变异已有读数，详见本分支EVIDENCE.md。
  - 阻塞：无本路实施阻塞；canonical/整包/race长门仍在执行，D实现与组长main装配不在本路，未自验收或部署。
  - 下一步：完成长门与SKIP逐项记录，追加最终证据并推送核ls-remote，交组长复核；广义跨路实现任务未据本路中间结果勾完成。

- 2026-11-06，执行者coder56solzzc，A路会话01a19ca0-0622-7743-a995-767c05e49c78；本段父commit 2e5e0b02b5d77db0428c5d357152ce7e418f54bf。
  - 完成：按组长04:54:43Z增量将外部路由身份查询入口收紧为SplitAgentName语法；补非外部下划线名零查询/返回false/普通离线路径回归，定向普通及race rc0；改回Contains变异rc1且三个子用例均命中查询次数断言，见EVIDENCE.md。提交前核提案目录最新提交仍为2e5e0b02，无派单后新增规格修订。
  - 阻塞：本项无；原canonical/整包/race长门仍待原流程收口，本项不提前勾选T-06～T-10或宣称最终交件。
  - 下一步：随下一个tip合入本项及EVIDENCE读数，按原单完成余项；不单独回信，不部署或重启。

- 2026-11-06，执行者coder56solzzc，A路会话01a19ca0-0622-7743-a995-767c05e49c78；本段父commit 2e5e0b02b5d77db0428c5d357152ce7e418f54bf。
  - 完成：已fetch/读取593ed5ef第6节，同步派单后新增冻结：固定四通讯scope、不支持子集、A/B/C共享TenantKey/Resource、C不交飞书令牌；A令牌持久audience/scope与资源现核，IssueToken签名保持；接口与context补装配边界。
  - 阻塞：本项无；原canonical日志已结束rc1，失败为0105加入后的旧claim migration清单断言，不能据本路定向PASS交最终件或验收。
  - 下一步：本次PG正反、race三轮与audience/scope语义变异读数入EVIDENCE，随下个tip交；main由组长统一装配，不单独回信。

- 2026-11-06，执行者coder56solzzc；本段父commit 2e5e0b02b5d77db0428c5d357152ce7e418f54bf。
  - 完成：登记C路k31t2075对A中间锚的独立fetch／接口比对报告；同意组长集成层以薄适配器映射C容器与A的Seat.ID、group，接口及证据已补充。
  - 阻塞：此签名差异不作为阻塞；不因此改A/C包、不认定已真实装配或独立验收。
  - 下一步：随下个tip提供精确Git坐标，由组长集成及验证适配。

- 2026-11-06，执行者coder56solzzc，A路01a19ca0唯一写者；本段父commit 2e5e0b02b5d77db0428c5d357152ce7e418f54bf，承载commit由最终tip给出。
  - 完成：遵05:03:21Z指令原子mkdir取得writer.lock，owner.txt记录开始UTC；保留两轮全部改动，合并核对路由形状、audience/固定scope、同事务双方身份锁、锁后时钟重核及后台重试。补真实PG令牌行锁等待/双方身份锁和Hub零身份重查用例，七项语义变异均在对应断言转红。修正0105引入后的真实跨runtime claim迁移清单；桌面真实Hub/PG集成与定向已rc0，逐项完整包结果见EVIDENCE和docs/reviews/r53-extseat-writer-reconcile/。
  - 阻塞：无本次合并阻塞；长门canonical/race分片按组长特许可在提交后独立重跑，未自验收或部署，不勾全跨路T-06～T-10。
  - 下一步：完整包通过后单提交推送，核tip/tree/ls-remote并回含to字段及两轮改动核对表的回执；完成后释放锁。提交前已fetch A分支2e5e0b02与集成分支593ed5ef，原提案需求无新增修订，集成新增第6节已同步；集成tasks缺A历史条目不构成规范变更，不删除本席交接。

- 2026-11-06，执行者coder56solzzc.zzc，实取CODEX_THREAD_ID=01a1af35-98c2-7451-a54a-6ec76c50d4f9；依据组长05:14:20Z持锁者即唯一写者新裁定，05:14:45Z抢锁并持续持有至推送＋交件回报完成。产品源commit fe67be849cca1498e0c188864e8df476d55aaa28，证据承载commit由交件回执给出。
  - 完成：接续既有长门，canonical rc0，五包整包rc0；extseat/migrate/identity race三轮包级PASS，m8四片各rc0且891顶层用例各3次。store旧合并命令90m总超时rc1保留，仅store按120m完整三轮重跑rc0。五包1375顶层用例各3次，共4125次，新增12测试无SKIP；42条普通整包SKIP中的PG相关一项已由canonical实际补跑五子项PASS。七刀修订变异保持，四条核心变异移到fe67当前源片补验均命中预期断言。证据见docs/reviews/r53-extseat/EVIDENCE.md。
  - 阻塞：无A路实现/自测阻塞；独立复核由组长集成执行，不据此勾选全跨路T-06～T-10或冒认B/C/D/main完成。
  - 下一步：只追加证据与交接，不改写fe67产品；提交前已fetch A=fe67、集成=75a40880，派单后合同新增第7节已同步至新证据目录，需求无新增变更。最终strict已rc0；正式交件tip/tree以推送核ls-remote后的回执为准，回执附实取ID及to字段，回报完成后释放锁。下一步由组长集成独立复核；不部署或重启。
