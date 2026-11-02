# Tasks

历史提案阶段：派单S-R14-FEISHU-STICKY-PROPOSAL，唯一执行者coder56solzzc.zzc；固定r46 2af469dd6919d8eb6064aa7da0e646f0851becf8，分支opus50tc2k/openspec-r14-feishu-sticky。本次仅更新本变更目录内 OpenSpec 文档；PM回写不授权实现、apply/archive或环境操作。

## 1. 现状与提案

- [x] T-01 独立fetch核7d376639第⑳条与固定r46，先完成context六项勘察：显式/无前缀/歧义路径及原回文、单Agent已成立条件、本人枚举、排序/井号/AMR/飞书纯text、可用内存/SQLite与重启边界；每项附2af469dd文件:行。
- [x] T-02 完成proposal/design/9条增量需求与正反场景；单列PM01～08和待核实假设；全仓OpenSpec strict rc0、scripts/run-build-tests.sh实际rc0，只改本目录后推送核ls-remote，冻结交独立审阅。门通过不等于新产品行为已验收。

## 2. PM与实施设计冻结（历史阶段）

- [x] T-03 按 rulings ㉔～㉗将 PM01～08 定值及短名唯一性勘察写回 proposal/design/spec/context；明确两入口查重实现任务，原选项保留为历史。
- [x] T-04 已按r48基线补充context与实施设计，列出稳定身份、墙钟/代际、同K序列化、投递故障对账、SQLite迁移/回滚、T-10查重点及S001～023测试层/变异；不确定项明确待组长定。仅完成文档设计冻结，不实现、不迁移、不探测真实飞书；不借session_endpoint或mirror配置伪装粘滞。

## 3. 实现与验收（本单授权 T-05、T-06、T-10、T-07）

- [x] T-05 实现R14-001～005/007的私聊身份、本人在线核权、显式优先、时钟、失效拒绝误投与单Agent路径，所有分支按已定PM值；异常不静默投别人。
- [x] T-06 实现R14-006/008/009的候选纯text、复制回贴/仅选择指令、持久/幂等/并发与可追溯结果，保持原命令/群聊/AMR边界。
- [x] T-10 托管 Agent 生成页与自备 Agent 审核页在提交前按当前属主对短名查重并明确提示；保留数据库唯一索引兜底。旧平台 session_endpoint 名不纳入该新增前置检查。
- [ ] T-07 隔离临时DB/HOME/假飞书/固定时钟执行R14-S001～023全部场景；撤owner/channel/显式优先/时钟边界/当前核权/候选块/稳定ID/幂等保护/短名查重分别在目标断言红，恢复后绿，无效变异或未执行不记PASS。
- [ ] T-08 另获授权做真实飞书桌面/移动端复制回入、Hub重启和迟到/并发读数，逐格记录实际目标、重复/缺失、TTL与候选格式；不向真实业主/Agent发送未授权探测。
- [ ] T-09 未来实施完成后跑相关整包/race、全仓strict与构建门、独立验收，再另申请预发/生产GO；不以apply/archive、T-01/02勾选或旧测试通过代替业务验收。


## 4. 阻断返修（01a19c8c 唯一执行）

- [x] T-11 修S002共享Identity.Store测试竞态及0072 legacy补账；精确DDL、损坏schema、70→71旧账哈希与旧Hub副本隔离验证。
- [x] T-12 按431d53d3223c与2386dfd99fd5实现FCS RPC旁路和默认关闭开关；T-10独立，追加S024～026。
- [x] T-13 补九格及镜像r48同命令三轮、关闭态七入口三轮逐字节对照，并给产品连接发布与worker差异分析；helper CAS按6921ce22db01另单。
- [ ] T-14 从完整-list生成穷尽分片，三包每例count3、隔离环境/端口，完成全门与有效变异，追加原文rc和Git精确坐标后重新交件。

## 交接记录

- 2026-11-01，coder56solzzc.zzc，基线2af469dd6919d8eb6064aa7da0e646f0851becf8；T-01完成、T-02文档已起草，验证仍在执行。已核原单Agent免前缀、多Agent无粘滞、多行井号块会按首目标+其它行正文解析，源码证据见context。阻塞：无本单写提案阻塞；PM01～08待业主、新场景均未执行。下一步：strict/完整构建门、检查范围、同分支普通push并核tip/tree后冻结，不实现或apply。

- 2026-11-01T23:04:03.232264+00:00，coder56solzzc.zzc，提案起点commit 2af469dd6919d8eb6064aa7da0e646f0851becf8；最终提案tip/tree以交件消息为准。
  - 完成：T-01/02；只新增本变更目录proposal/design/spec/context/tasks共5份Markdown。43组源码引用逐项核文件存在/行界和2af469dd字节一致；9条需求、20唯一场景，正文各不足500字、spec不足30KB。先完成context再写提案；8个PM点均待业主，单Agent既有条件、无前缀原回文、井号整块回贴风险及SQLite可用设施已入件，没有把既有回程关联当粘滞已实现。
  - 验证：OpenSpec1.16.0/Node22.23.1/OPENSPEC_TELEMETRY=0，全仓strict13/13 rc0，交件前最终文档再次核验；本轮bash scripts/run-build-tests.sh实际2026-11-01T22:54:59.776725+00:00～2026-11-01T23:03:50.390065+00:00 rc0，helper两个profile各1092/1092、inventory90、Python27组/shell2组。普通dev用户、锁helper-deps-lock-venv、临时HOME和环境白名单，剥继承SH/DINGWEI/DW/WP等配置，原始日志/tmp/r14-sticky-build.log，SHA256 5d98749ada27c993dd32b40e9ae1451bbbfccaba1566c71de80b7399dcda50b7；strict日志/tmp/r14-sticky-strict.log。仅旧测试与结构门通过，R14新场景/变异/真实飞书回环/持久化均未执行。
  - 阻塞：无本单提案阻塞；PM01～08、实现原子性和真实客户端复制仍待后续定值/验证。T-03及以后保持未勾选，不自验收、不实施、不apply/archive、不改主规格/源码/配置或r46/r47。
  - 下一步：普通push到opus50tc2k/openspec-r14-feishu-sticky，核远端tip/tree后冻结交独立审阅；收到业主PM裁定后另派规格回写/实现，不自行继续。

- 2026-11-02 · togpt60lunazzc.zzc · PM WRITEBACK：依据 rulings 2f9ddaf0 第㉔～㉗条完成PM01～08回写、短名唯一性现状勘察和 R14-S021～023；㉗要求的双入口提交前查重仍是待另派实现的 T-10。文档提交 `81bf46d4e9edd7a0c9d4a195da04818a5d1b98c7`；strict 已通过。下一步：提交交接记录，复跑 strict，推送并核对远端 tip/tree；不实现、不apply、不操作环境。

- 2026-11-02T11:28:10Z · togpt60lunazzc.zzc · S-R49-R14-T04 设计冻结文档提交 `88b26b427027f21964169b46add4df3e8caf1ae9`，基于 r48 `1e17255f28ced042784337903914ca8c126ef4cf` 并普通 merge PM 回写 `cd238d715ad33e51f001f874a9963aaa96aa85f3`。
  - 完成：按 r48 文件:行补稳定身份、时钟/代际、并发序列、投递故障对账、SQLite 0072 迁移草案、短名双入口前置查重及 S001～023 测试层/变异矩阵；待组长定事项单列。仅 T-04 勾选，T-05 以后状态未改。全仓 OpenSpec strict 15/15 rc0；diff-check通过，提交范围仅本变更目录。
  - 边界：没有产品代码、数据库迁移、真实飞书探测或产品场景执行；该设计提交待组长审阅，T-05/T-06/T-07/T-08/T-10未授权执行。


- 2026-11-02 · coder56solzzc.zzc（唯一写者 01a19b96-bc02-7640-98e5-751be83ee8dc）· S-R49-R14-IMPL，已完成实现源码提交 `9b62531c6fb971239cda5820bd313fe22074ee63`，由基线 r48 普通 merge 冻结设计 c5081e63，merge `65f805a50455f64d0e8119cd1f8462823dda053d` / tree `9bad70e5ae7971f0431a8414f5ebf752e1e860ac`，ort 零冲突。
  - 完成阶段：迁移/存储、路由、T-10 已分次提交；补入组长 2026-11-02T11:44:34Z 迟到对账只结案的裁定及专格。首轮旧提交已有 build/浏览器/strict/整包通过读数；当前最终源码含补充测试和对账 metadata，正在重跑最终门，故本条暂不勾 T-05/06/10/07。
  - 阻塞：无未定产品点；曾询问的迟到对账已获组长裁定。入站已到盘但 admission_unknown 的裁定由只读 inbox 取回，不将未注入说成未答复。
  - 下一步：最终源码的全包、race、严格 PG、浏览器两渠道、逐格语义变异、r48 隔离迁移/回滚和 strict 完成后再勾本单四项、附证据分支并核远端。T-08/T-09 保持未勾；无真实飞书探测、线上操作、版本变化、PG schema 或 apply/archive。


- 2026-11-02T13:26:12.435124+00:00 · coder56solzzc.zzc（唯一写者 01a19b96-bc02-7640-98e5-751be83ee8dc）· S-R49-R14-IMPL 开发侧交件：T-05、T-06、T-10、T-07 已完成并勾选，T-08 与 T-09 保持未勾。
  - 源码分支 `coder56solzzc/r49-r14-impl`；最后产品代码 `59e859e1f3bb4099e8f15da246070b549d598263`，最终测试补充锚 `e7d7e6ca35d612f5671a57ea584ee74633bee608`，本次其后只补本变更目录文档。基线/普通 merge/tree 沿用上条；分阶段提交迁移/存储、路由、双入口及测试，未改 PG schema、既有路由权限或版本。
  - 门：锁构建 venv、短 HOME/TMPDIR、环境白名单的 run-build-tests rc0（helper 双 profile 各1097、Python27组、shell2组）；m8 全包 -v rc0；workpulse 全包 -v rc0；相关 m8/store/workpulse 场景 race 与严格 embedded PG rc0；新增同键显式+无前缀并发格重复5次 race rc0。全仓 OpenSpec strict 15/15 rc0，最终文档提交后再次执行。
  - 场景/变异：S001～023 的冻结层逐格实现；28个 Go/store 目标变异均红在语义断言，撤变异后原隔离工作树绿。浏览器 Chrome 与 headless shell 各覆盖两入口及旧回归，共6次正常绿、6次目标变异语义红；S018仅本地 mock，不冒充真实飞书回环。早期编译无效、setup错误、S020初始存活、S002仅撤一层仍被路由层拒绝的存活变异及原始race失败保留，不计最终有效通过。
  - 迁移兼容：精确 r48 Hub 二进制对原库、应用0072库副本、迁移失败库均 health200/exit0；审定回滚 SQL 使2/2状态失效、2/2 pending变unknown、表和4条操作保留；故障迁移无状态表或ledger半提交。兼容夹具改为等旧Hub READY再发退出信号，连续3次race绿；未修改旧Hub。
  - 证据分支 `evidence/coder56solzzc-r49-r14-impl`，相对最终源码仅 docs；收录完整命令、UTC、日志SHA256、逐格矩阵、逐条SKIP原因、有效与无效原件。最终tip/tree及非空ls-remote核对原文以交件消息为准。
  - 组长11:44:34Z迟到对账裁定已落实：仅关闭原操作，不改新绑定、TTL或代际，不重发；真实本地Agent迟到回文、queued稳定ID、新绑定后旧意图对账都有专格。无待组长定产品点；不承诺跨动作原子。T-08真实飞书、T-09独立验收、部署/重启/apply/archive均未执行，后续另派。


- 2026-11-02 · coder56solzzc.zzc（01a19c8c-6b8b-7f23-8a6a-f7b69fd0aafb）· 返修已实现，最终全门进行中。
  - 完成：接到01a19c61已停无提交/无推送回执后，从远端fc7de19d新树开修。两路增量逐项核取，重复migration反测留一份，原共享树未回滚/删除。S002测试夹具与0072补账分别提交ae4376622/0b2cd2ac2；F-01 aab9d4064、默认关闭2cda2562b，关闭态公共对照74308a078。普通legacy建连夹具等同代发布，刻意发布前镜像格走raw。
  - 已知读数：原45m整包rc1/未完成，不改标精确tip；九格+镜像r48同命令race三轮全绿，关闭态七入口两边各三轮rc0、6份规范化JSON字节一致。FCS+开关+主要矩阵三轮rc0；最终所有包全用例三轮、变异和构建仍待最终固定源码重跑。T-07重新打开，旧交接勾选与fc7de19d/11b2d545交件作废；T-09由别席独立验收。
  - 阻塞/范围：helper CAS为r48同代码既有产品并发，按6921ce22db01另单，不改本件产品代码；本件仅该两格及已定100k时限可按裁定例外，其余失败必须定位解决。无部署/重启/版本位/PG schema/真实飞书动作。
  - 下一步：固定源码后完整穷尽race分片、m8全包、strict PG、规范构建、浏览器两渠道、strict与逐刀红/恢复绿；勾T-07/T-14并以Git-only坐标重新交件。R14交件后先S-R49-HELPER-OFFER-RACE，再R10。
