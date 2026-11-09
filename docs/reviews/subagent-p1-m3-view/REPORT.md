# S-SUBAGENT-P1-M3 交审证据

CODEX_THREAD_ID=01a1bd1c-c28e-7c60-9594-7894d0078533；to=opus50tc2k.zzc。仓库 git@git.code.tencent.com:wego-ai/dingwei.git；分支 coder56solzzc/subagent-p1-m3-view。基线0b47a5857cc331ee5dc8012a002fd561ae2279f0；设计d9a43e6a §6/7与AA010、rulings〔51〕-补14。本报告记录实现和自测，交组长独立复核。

唯一写者为本会话，持/tmp/coder56-subagent-m3.lock；另一路只读、不碰工作树。M2锁按批准释放。全程无部署、预发、在线席位或真实推理动作；没有合入r56。测试只用隔离HOME/TMPDIR、合成事件/临时库，剥离SH_/DINGWEI_/DW_/DATAD_，uid1001、Go1.22.2。

## 冻结坐标与输入一致性

- 产品冻结18cae8bf5cd5ab737cef7044b326293ba3e87b15，tree861bd037e37b86112ab305e5897cf018af871383。
- 执行候选9ae19c5a1ee5751acdaeb084aed7443ca7c05673，treef80b9f580be8f99108be9a50319f2c9541da9500；产品之后只补接收测试夹具、变异隔离依赖与历史探针辅助断言。没有改写旧提交。
- Go全包及全量浏览器在产品源18cae8bf5执行；canonical在3723e263a5cf5641e59157516bea11f959532910执行；future在9ae19c5a1执行。对应产品/测试输入与交审候选逐路径diff为空，路径集合和差异清单记录于raw/SUMMARY.json。最后的证据提交只追加本目录与OpenSpec进度。
- SOURCE-SHA256SUMS覆盖37个变化的代码/测试输入；ARTIFACT-SHA256SUMS覆盖交审报告、源清单及原文。均可在仓库根目录复算；不包含私有库、账户状态、env、凭据、浏览器安装物或可执行制品。

## 最终门禁

| 门 | 结果 | 原文 |
|---|---|---|
| canonical完整入口 | rc0；claude/codex各1165/1165；92文件；27 Python/2 shell | raw/final/canonical.json及canonical.stdout.gz |
| m8完整包 | 1533通过、0失败、37 SKIP | raw/final/go-all.json及go-all.stdout.gz |
| store / consolemux / cmd | store605/0/5 SKIP；consolemux12/0；workpulse259/0/6 SKIP；其他cmd通过或无测试且编译成功 | 同上，逐包计数见SUMMARY |
| 浏览器完整runner | 39套件全部rc0，未选格；新树套件8项通过 | raw/final/browser-receipt.txt及browsers.stdout |
| AA010 M3变异 | 28/28 target_red；每刀附命令、UTC、rc及目标断言 | raw/mutations/RESULT.json及逐刀stdout |
| 旧Hub混部变异 | 旧helper/新helper两格各红在AA010_OLD_HUB_FULL_CHILD_REJECT | raw/old-hub-mutant/ |
| future兼容保护 | rc0；AMR跳子保根、未协商未知路径不落父保持 | raw/future/及raw/final/future-phase1.json |
| OpenSpec strict全量 | 29通过、0失败；交接进度更新后再次验证 | raw/final/strict.*及latest-strict.* |

48个Go跳过项的名称与实际跳过条件完整列于raw/SUMMARY.json，原文保留。未把可选外部PG、真实CLI或专用产品浏览器集成的跳过项计成执行通过。canonical中的跨运行时desktop shell已独立通过。

浏览器工具路径覆盖仅NODE_PATH与PLAYWRIGHT_BROWSERS_PATH，收据明列；未覆盖生产renderer或夹具。新增专项执行真实Console源码，HTTP依赖为隔离模拟；它与完整runner均不是线上浏览器验收。

## AA010正负例与一刀对应

| 格 | 正例与负例 | 变异 |
|---|---|---|
| root/node/generation投影 | 两兄弟事件独立，代次改变产生新scope；伪造节点/代次不落父 | scope-generation、event-generation |
| 输入/回执展示 | 目标输入按持久sidecar落子投影；父只有真实发信copy，不冒充子primary | input-sidecar |
| 根授权 | 根授权后才展开真实子scope；无根grant不枚举子节点 | root-authority |
| 预算 | 8个真实scope允许，第9个拒绝；兄弟ID各自计数 | scope-budget |
| 票据 | Hub重建并签root/node/generation；header-only、签名错、代次错、scope错拒绝 | ticket-node、unsigned-child、ticket-generation、invalid-signature |
| 过期/撤权 | 独立SSE的viewer会话撤销即停止；根撤权移除全部mux子scope | view-revoke、root-revoke |
| 缓存 | 同显示名不同scope使用不同page key；根保留旧key | page-cache |
| 费用/游标 | 相同epoch/seq发两兄弟分别计费；重启幂等，旧根费用保留，游标独立 | sibling-cost |
| 旧文本/新树 | 混入子行仍从旧文本过滤；独立JSON全量替换，旧Hub快照清掉旧树 | legacy-text-root-only、legacy-json-reset |
| 树状态与提及 | 在线与ended/completed显示不同，ended不可交互；子页候选只列根 | tree-ended-online、root-mention-only、browser-tree |
| UI撤权竞态 | 根失效遮断孩子；迟到旧树响应不能复活已清子树 | browser-root-mask、browser-stale-tree |
| 协商/feature-off | 新新协商并ACK才可投；关闭能力拒子保根 | new-new-negotiation、feature-off |
| feature-off退信 | 不启动broker/原生；封口后逐封发退信，Hub持久接收才记returned | feature-off-returns、helper-return-barrier |
| 旧二进制隔离 | 两封未执行退信、sent未知隔离、ACK原样、barrier阻止再建树 | legacy-rollback、per-mail-return、unknown-preserve、rollback-reopen |

所有变异均使用Go overlay或隔离Python副本，浏览器变异只在自己的HTTP夹具中替换源码字符串；不在工作树撤修，不修改在线实例。

## 五格混部与回退原文

旧Hub/旧claimer源码不可变锚79a60a1e351c207ff0b46bdf9151e846ec83f5d4，含先行拒收保护。旧源码工作树只读，测试通过overlay挂入。

| 组合 | 实测结果与提示 | 证据 |
|---|---|---|
| 旧Hub/旧helper行为 | 完整子地址拒绝，根信正常；不截成父 | raw/old-hub/old-hub.stdout |
| 旧Hub/新helper行为 | 新能力请求无ACK；完整子地址拒收，根正常；helper创建门保持未就绪/不支持 | 同上，new_helper格与canonical helper状态机 |
| 新Hub/旧helper行为 | 未协商子投递sub_agent_unsupported；根正常 | 完整m8的TestSubAgentViewNewHubMixedMatrix/false |
| 新Hub/新helper行为 | 协商/树ACK后子信带node/generation成功；根正常 | 同方法true格与helper36项 |
| 新View/旧Hub | 无树旗标时保留根列表；无票子scope拒绝 | 浏览器AA010_NEW_VIEW_OLD_HUB_ROOT_ONLY与View unsigned-child |

此处混部为实际Hub HTTP/WS处理器、helper状态机和旧源claimer的隔离组合实测，不启动真实模型。原生0.159.2控制工具证据沿用已复核M2，本单没有冒称重新跑原生推理。

先feature-off，再旧二进制回退：helper维护路径先原子draining、两封逐条退信；未确认退信时downgrade_ready=false；可能执行的一封保持admission_unknown，旧根inbox claim不到子项。新Hub的0076事务生成两条正常失败通知，sent未知行保留状态并移到隔离namespace；已processed行保留ACK generation7，墓碑/别名/sidecar都保留。重复准备幂等，rollback barrier阻止重开树。

run_old_claimer.py将新源准备的隔离SQLite副本交给79a60a1e实际ClaimSessionDelivery读取：根可读、子0封，unknown与ACK不变，原文raw/actual-old-claimer/。实现中回给同根孩子的通知也纳入隔离；本次旧源claimer主格使用根来源退信。未完成逐封退信和隔离时继续新版本feature-off，不允许只换程序。工具cmd/subagent-rollback要求明确DB/root及已停写/feature-off/helper已封口确认；不自动迁移，不联网，不DROP，也不删ACK、墓碑或barrier。

运维操作顺序/备份水位/禁止降级条件见docs/design/SUBAGENT-P1-M3-VIEW.md；席位草稿见docs/releases/r57-sub-agent-seat-guide-draft.md，含重开主会话、受控工具、一级限制及1h/2d/5d/10min/完成即退/父关闭级联默认。

## 过程失败与修正

原文保留在raw/prior，不用修正后的成功覆盖旧读数：

1. 新0076首次未登记legacy exact-DDL补账预检，旧库补账测试红；补接后完整store通过。
2. 独立Go夹具最初把不完整mux测试对象交给Hub关闭导致清理panic；补清理归属后专项通过，未通过改产品关门掩盖。
3. 回退变异最初用delivered/ACKv2行，旧claimer本来就不取，撤隔离仍绿；改用真正可重试sent未知行后命中AA010_LEGACY_REPLAY_PARENT，并由实际旧源重复验证。
4. 浏览器变异首跑因隔离HOME缺浏览器安装路径而未进入断言；仅显式设置工具路径后目标红。缓存变异首跑缺隔离依赖搜索路径；补路径后目标红。
5. 完整canonical发现旧接收静默性SimpleNamespace夹具缺新增树回调；只补夹具并断言旧目录清树而不注入，最终双profile各1165通过。
6. 历史探针的Console辅助断言把decode当永久拒收门，与M3结构化scope相冲突；保留原红，更新为decode不授予权限、无grant必须在scope授权404拒绝。原AMR及未知路径父误投两条future断言未改；正式future全绿。

独立复核完成前保留M3写者锁。后续集成、版本号、发布与恢复barrier另派；本单没有这些运行授权。
