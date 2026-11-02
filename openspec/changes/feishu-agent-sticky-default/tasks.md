# Tasks

派单S-R14-FEISHU-STICKY-PROPOSAL，唯一执行者coder56solzzc.zzc；固定r46 2af469dd6919d8eb6064aa7da0e646f0851becf8，分支opus50tc2k/openspec-r14-feishu-sticky。本次仅更新本变更目录内 OpenSpec 文档；PM回写不授权实现、apply/archive或环境操作。

## 1. 现状与提案

- [x] T-01 独立fetch核7d376639第⑳条与固定r46，先完成context六项勘察：显式/无前缀/歧义路径及原回文、单Agent已成立条件、本人枚举、排序/井号/AMR/飞书纯text、可用内存/SQLite与重启边界；每项附2af469dd文件:行。
- [x] T-02 完成proposal/design/9条增量需求与正反场景；单列PM01～08和待核实假设；全仓OpenSpec strict rc0、scripts/run-build-tests.sh实际rc0，只改本目录后推送核ls-remote，冻结交独立审阅。门通过不等于新产品行为已验收。

## 2. PM与实施冻结（未授权）

- [x] T-03 按 rulings ㉔～㉗将 PM01～08 定值及短名唯一性勘察写回 proposal/design/spec/context；明确两入口查重实现任务，原选项保留为历史。
- [x] T-04 已按r48基线补充context与实施设计，列出稳定身份、墙钟/代际、同K序列化、投递故障对账、SQLite迁移/回滚、T-10查重点及S001～023测试层/变异；不确定项明确待组长定。仅完成文档设计冻结，不实现、不迁移、不探测真实飞书；不借session_endpoint或mirror配置伪装粘滞。

## 3. 实现与验收（未授权、未执行）

- [ ] T-05 实现R14-001～005/007的私聊身份、本人在线核权、显式优先、时钟、失效拒绝误投与单Agent路径，所有分支按已定PM值；异常不静默投别人。
- [ ] T-06 实现R14-006/008/009的候选纯text、复制回贴/仅选择指令、持久/幂等/并发与可追溯结果，保持原命令/群聊/AMR边界。
- [ ] T-10 托管 Agent 生成页与自备 Agent 审核页在提交前按当前属主对短名查重并明确提示；保留数据库唯一索引兜底。旧平台 session_endpoint 名不纳入该新增前置检查。
- [ ] T-07 隔离临时DB/HOME/假飞书/固定时钟执行R14-S001～023全部场景；撤owner/channel/显式优先/时钟边界/当前核权/候选块/稳定ID/幂等保护/短名查重分别在目标断言红，恢复后绿，无效变异或未执行不记PASS。
- [ ] T-08 另获授权做真实飞书桌面/移动端复制回入、Hub重启和迟到/并发读数，逐格记录实际目标、重复/缺失、TTL与候选格式；不向真实业主/Agent发送未授权探测。
- [ ] T-09 未来实施完成后跑相关整包/race、全仓strict与构建门、独立验收，再另申请预发/生产GO；不以apply/archive、T-01/02勾选或旧测试通过代替业务验收。

## 交接记录

- 2026-11-01，coder56solzzc.zzc，基线2af469dd6919d8eb6064aa7da0e646f0851becf8；T-01完成、T-02文档已起草，验证仍在执行。已核原单Agent免前缀、多Agent无粘滞、多行井号块会按首目标+其它行正文解析，源码证据见context。阻塞：无本单写提案阻塞；PM01～08待业主、新场景均未执行。下一步：strict/完整构建门、检查范围、同分支普通push并核tip/tree后冻结，不实现或apply。

- 2026-11-01T23:04:03.232264+00:00，coder56solzzc.zzc，提案起点commit 2af469dd6919d8eb6064aa7da0e646f0851becf8；最终提案tip/tree以交件消息为准。
  - 完成：T-01/02；只新增本变更目录proposal/design/spec/context/tasks共5份Markdown。43组源码引用逐项核文件存在/行界和2af469dd字节一致；9条需求、20唯一场景，正文各不足500字、spec不足30KB。先完成context再写提案；8个PM点均待业主，单Agent既有条件、无前缀原回文、井号整块回贴风险及SQLite可用设施已入件，没有把既有回程关联当粘滞已实现。
  - 验证：OpenSpec1.16.0/Node22.23.1/OPENSPEC_TELEMETRY=0，全仓strict13/13 rc0，交件前最终文档再次核验；本轮bash scripts/run-build-tests.sh实际2026-11-01T22:54:59.776725+00:00～2026-11-01T23:03:50.390065+00:00 rc0，helper两个profile各1092/1092、inventory90、Python27组/shell2组。普通dev用户、锁helper-deps-lock-venv、临时HOME和环境白名单，剥继承SH/DINGWEI/DW/WP等配置，原始日志/tmp/r14-sticky-build.log，SHA256 5d98749ada27c993dd32b40e9ae1451bbbfccaba1566c71de80b7399dcda50b7；strict日志/tmp/r14-sticky-strict.log。仅旧测试与结构门通过，R14新场景/变异/真实飞书回环/持久化均未执行。
  - 阻塞：无本单提案阻塞；PM01～08、实现原子性和真实客户端复制仍待后续定值/验证。T-03及以后保持未勾选，不自验收、不实施、不apply/archive、不改主规格/源码/配置或r46/r47。
  - 下一步：普通push到opus50tc2k/openspec-r14-feishu-sticky，核远端tip/tree后冻结交独立审阅；收到业主PM裁定后另派规格回写/实现，不自行继续。

- 2026-11-02 · togpt60lunazzc.zzc · PM WRITEBACK：依据 rulings 2f9ddaf0 第㉔～㉗条完成PM01～08回写、短名唯一性现状勘察和 R14-S021～023；㉗要求的双入口提交前查重仍是待另派实现的 T-10。文档提交 `81bf46d4e9edd7a0c9d4a195da04818a5d1b98c7`；strict 已通过。下一步：提交交接记录，复跑 strict，推送并核对远端 tip/tree；不实现、不apply、不操作环境。
