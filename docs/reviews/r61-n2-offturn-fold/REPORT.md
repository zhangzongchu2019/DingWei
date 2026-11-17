# S-R61-N2 实现交件报告：Claude 席回合外输出折叠条

- 会话：qwen38mvb4.zzc（承办）；组长 opus50tc2k.zzc 派单 2026-11-17T18:20:07Z、设计批准 2026-11-17T18:41:20Z（批准信 delivery dw-dlv_bca6333c86d55f691a6348608ccad7c7）。
- 仓库：wego-ai/dingwei；分支 qwen38mvb4/r61-n2-offturn-fold；基点 9818b979e9e20f0bcfeffa6b78045dda71b1bd79（r60 集成锚，交件时 ls-remote 核未移动）；设计稿提交 40491a69ae3cfb084d3d4257b832ecb2751cb32c（DESIGN.md，已批准）。
- 实现提交：见 git log（本报告与实现同分支追加，不改写已交审提交）；产品改动仅 view-service/renderer-canary-sse.html（＋266／−1），新增回归件 view-service/renderer-offturn-fold-regression.js 与 renderer-offturn-fold-fixture.py，登记 tools/run-browser-regressions.py SUITES（renderer_offturn_fold）。
- 边界自查：Hub／helper 零改动；VERSION 未改；md-render-core.js 未改（复用 renderMarkdown）；不推 main、不出发布件、不碰预发／现网。§1.4 Hub 硬化本期不做，renderer 侧预留接口已留（见下）。

## 一、实现与批准设计对照

1. 判定与双闸（设计①）：isOffturnCandidate＝kind assistant_text 且 turnKeyFor 空（turn_id／_norm.turnId／delivery_id 皆空）且 data 无 Codex 标记（闸 A）；scanOffturnCodexMarkers 在 dispatchIncomingEvent 归一后扫描每条被接受事件（含隐藏 kind），命中 item_id／thread_id／params／raw_method／item／model／usage 任一非空即会话级 offturnCodexSeen 置位并 unfoldAllOffturn 回落现状（闸 B，保守 sticky，resetConversation 清零）。闸 A 标记 clause 与闸 B 共用同一检测器 offturnCodexMarkers（单源两处消费）。Codex 席不适用：其协议帧必带归属与 data 标记（codexDriver.py:1888-1897、1960-1987、1813-1830），双闸任一即挡。
2. 预留接口（批准「接口留好」）：offturnSessionCli 变量＋setOffturnSessionCli setter；refreshStatus 初读可选 body.session_cli（后端今日不发该字段，读到空即 null＝走双闸）；非 claude 权威值 ⇒ unfoldAllOffturn 且不再折叠。r61 Hub 批次落地后只需后端发字段，renderer 无需再改。
3. 合并与边界（设计②）：规范时间线序（placeTimelineNode 同轴）上极大连续段；尾随快路径 O(1) 追加（须同纪元 rank 且节点为 timeline 末子）；offturnBreaksRun 断开＝回合事件（turnKey 非空的 user_input／assistant_text／tool_call／state_change／error）、input_posted／user_input 输入卡、mention_reply、error／unknown、_history_gap 提示；不断＝无归属隐藏 kind（tool_call／state_change）与原位 delivery_update／feishu_sync／route_observation 归并。断开以 sealed 封口，合并 pass 尊重封口。组键 offturn-cursor-<首事件 cursor>（较设计稿 cursor- 前缀加 offturn- 命名空间，防与 assistant keyFor 碰撞）；正文 append-only 拼接，不做 cursor 去重（3889 P0 教训）。
4. UI：details.offturn-fold 浅灰（底 #f3f4f6／线 #e5e7eb／题字 #6b7280），summary 标题逐字「回合外输出（Claude）」＋条数·时间 meta；默认折叠（无 open）；点开懒渲染 renderMarkdown 全量＋wireCopyButtons／attachFileRenders／wireMarkdownImages；不开浮窗、不另开文字区。
5. 稳定性（设计③）：折叠＝被接受事件序列的纯投影，不新增去重。有序号重连（admitEventId after 补缺）保留后缀不叠加；旧 Hub 重连 consumeReconnectClear→resetConversation 同步清全部折叠态后重建（字节一致）；历史分页 historyApplying 期间逐事件建组、页 flush＋重定位后 recomputeOffturnFolds（拆分 pass＋合并 pass）确定性归并；bootstrap 缓冲 replay 后再重算，highWater 跳过防双份；逐出整节点原子移除＋releaseTimelineNode 反查清 offturnGroups／pendingRenders／openOffturnGroup。
6. 非交叉（设计④）：折叠条自建 details 节点，不经 baseCard ⇒ 无 .waiting-retry-btn（retry 仍 delivery_id 键控）、无 .prov-ic（provenanceIconHtml 仅 trusted 输入卡路径）；不触 c1InputCards／c1DeliveryCards，retainCanonicalTrustedProvenance 两调用点（2801、3828）零改动。

## 二、门与读数（原始日志在 evidence/，*.log 被 .gitignore 忽略、已 git add -f 强制入 Git，r60 缺日志教训不重演）

1. 新套件（node view-service/renderer-offturn-fold-regression.js，NODE_PATH=/home/is3/pwrun/node_modules）：
   - run3（最终）：G1～G10 共 17 格全 PASS＋M1～M12 十二刀全红（变异后仍绿即套件失败）。日志 evidence/offturn-suite-run3.log。
   - run1：G1/G2/G3 七变体/G9/G10 绿；G4 闸 A 夹具条数误（N=12 vs 断言 6）⇒ 修夹具 gatea 固定 6 条。日志 evidence/offturn-suite-run1.log。
   - run2：格全绿；M1 首版刀口（只删闸 A 标记 clause）被闸 B 同源检测器兜住仍绿 ⇒ 刀改切 offturnCodexMarkers 检测器本体后转红。日志 evidence/offturn-suite-run2.log。
2. 浏览器全量 runner（python3 tools/run-browser-regressions.py --receipt …）：
   - run1（与 run-build-tests 并行）：47 套件中 45 绿，r15_session_history_pagination rc=2（滚动格 waitForFunction 3000ms 超时）与 r15_session_history_mutations rc=1（派生：期望红在特定断言、实际先超时）⇒ 两套件单独静默重跑皆 rc=0（evidence/r15-pagination-solo.log、r15-mutations-solo.log，mutations 三刀红点逐字在案），定因＝并行 CPU 负载挤占滚动时间窗，非产品回归。
   - run2（静默重跑）：47 套件、aggregate_rc=2、失败 1＝r15_session_history_pagination（同一滚动格 3000ms 超时）；r15_session_history_mutations 本轮 rc=0（三刀红点逐字在案）。回执 evidence/browser-receipt-run2.txt、全日志 evidence/browser-all-run2.log。
   - r15 翻页格抖性定因（非 N2 回归，对照实验在案）：
     · 新渲染器单跑五连：rc=0/2/0/0/2（evidence/r15-solo-1..5.log），红点逐字同为 r15-session-history-browser-regression.js:96 waitForFunction(timelineAtBottom===false) 3000ms 超时。
     · 基线渲染器（git show 9818b979:view-service/renderer-canary-sse.html，RENDERER_OVERRIDE 指之）同跑五连：rc=0/0/2/0/2（evidence/r15-base-1..5.log），红点逐字相同 ⇒ 该格在**未含 N2 改动**的基线上同概率抖，属既有时序敏感（滚动状态翻转 3s 窗），非本单引入。
     · N2 钩子对该夹具为纯 no-op（夹具事件全带 turn_id／delivery_id，无回合外候选 ⇒ offturnGroups 恒空，scan/break/recompute 各钩子空转），且 r60 集成门该格 rc=0（docs/reviews/r60-integration/evidence/browser-receipt.txt:21）⇒ 基线绿是单次采样，抖性潜伏。
     · 前序套件诱发对照：renderer_sse_reconnect（runner 中该格前一位）＋pagination 成对三连＝0/0/2（evidence/r15-pair-1..3.log），与单跑同率 ⇒ 非前序套件系统性诱发，全量上下文（多 Chromium 起落＋contract mock＋python 父进程）负载放大该抖性。
     · run3（再采样）：47 套件、aggregate_rc=2、失败 2＝r15_session_history_pagination（同一 :96 超时）＋r15_session_history_mutations（派生：其内基线跑先超时、未及变异断言，wrong assertion failed）；其余 45 套件全绿。回执 evidence/browser-receipt-run3.txt、全日志 evidence/browser-all-run3.log。
     · 处置：不修无关用例（仓规）；三轮全量读数全量披露（run1 45/47、run2 46/47、run3 45/47，唯一失败点均为该既有抖性格或其派生）。交件以 run2 为正式回执（46/47），请组长裁定：接受既有抖性（基线对照在案）或另单修该格时序窗。图标格与重发格三轮均全绿，满足批准硬条件。
   - 现网热修 8cabb351 来源图标格：renderer_provenance_icon 5/5、renderer_agent_icon_admission 9/9 全绿（run1/run2 同）；r60④ 重发格 renderer_retry_role／renderer_c2／renderer_delivery_status rc=0。
3. scripts/run-build-tests.sh（PATH 前置 /opt/dingwei/sessionhelper-venv/bin）：rc=0，BUILD_TESTS_OK helper_profiles=2 python=27 shell=2；三质量闸（repo-boundary／dependency-ratchet／tenant-isolation）先过。日志 evidence/build-tests.log。
4. Go 聚焦钉值（GOTOOLCHAIN=local go test ./internal/m8 -run 'TestConsoleSpecAssetsArePinnedAndRouteIsAllowlisted|TestConsoleSpecEmbeddedMarkdownCoreIsPinned' -count=1）：ok 0.072s。日志 evidence/go-m8-pins.log。
5. md-render-core：python3 view-service/md-render-core-sync.py --check rc=0（6 段逐字节一致）；sha256=0f03c242f05e359d94ff1dbe0453d77e83ec7f50d0dec2add4bb6e70b7eb146a ＝ internal/m8/console_specs_assets_test.go:44 const want ⇒ 无需回写。

## 三、删保护刀清单（M1～M12，读数为「变异后该格转红」）

| 刀 | 切除的保护 | 变异 | 转红格 |
| --- | --- | --- | --- |
| M1 | Codex 标记检测器（闸 A 形状闸核心，闸 B 同源） | offturnCodexMarkers 首句恒 return false | G4 闸 A |
| M2 | 闸 B 会话级扫描 | scanOffturnCodexMarkers 恒早退 | G4 闸 B |
| M3 | 边界判定 | offturnBreaksRun 兜底 return false | G3 错误边界 |
| M4 | 合并封口检查 | 合并 pass 删 left.sealed 条件 | G3 错误边界 |
| M5 | 合并 pass | recompute 内 offturnMergePass() 置空 | G7 分页合并 |
| M6 | 拆分 pass | recompute 内 split 循环置空 | G7 缺口拆分 |
| M7 | 逐出索引回收 | releaseTimelineNode 删 offturnKey 分支 | G8 泄漏 |
| M8 | 重置清折叠态 | resetConversation 删五行清空 | G6 换 ring 残留 |
| M9 | 默认折叠 | node.open = true | G1 默认折叠 |
| M10 | 正文不吞 | buffer 追加置空 | G10 长度 |
| M11 | 标题逐字 | OFFTURN_TITLE 改短 | G2 逐字 |
| M12 | 折叠路由 | renderEvent 删 renderOffturn 分支 | G9 折叠存在性 |

## 四、偏差与说明

- M1 刀口演进见二.1：双闸共用单源检测器是设计本意（两处消费），只删一处消费会被另一处兜住，故刀切检测器本体；G4 两格分别钉闸 A 形状与闸 B 会话回落，各有独立刀（M1／M2）。
- 逐出为整节点原子移除（设计稿「逐出重键」措辞的实现收敛）：折叠组节点不可分片逐出，G8 钉「索引数＝DOM 折叠节点数、全 connected、渲染队列无 detached」，无泄漏即等价保护。
- offturnCodexSeen 会话级 sticky 为保守回落（批准「不确定即回落现状」）：一旦窗口内出现 Codex 标记，本会话剩余渲染全走现状独立卡，直到 resetConversation。历史分页加载更早窗口若含 Codex 标记同样触发整体回落（纯投影、可复现）。
- 组键命名空间 offturn-cursor-（设计稿 cursor-）：防与 renderAssistant keyFor 的 cursor- 键碰撞，语义不变（首事件 cursor）。
- 批准信在收件箱重复注入多次（r61 延迟注入缺陷），按 STATE 识别为同一件、不重复启动；派单信与批准信均已 read＋ack（本地）。另按组长 18:41:40Z 回执核对令，收单回执 11d01aa342e3「发方报 sent、收方未到」已登记为投递丢失实证（STATE，含 send.py 原文与 UTC），不重发。

## 五、复核入口

- 渲染器改动：view-service/renderer-canary-sse.html（CSS .offturn-fold 族；状态声明 assistantEntries 旁；dispatchIncomingEvent 归一后双闸钩子；renderEvent assistant_text 路由；renderPendingEntry offturn 分支；renderAssistant 前折叠函数块；releaseTimelineNode／resetConversation 清理；loadSessionHistory 重算钩子；refreshStatus session_cli 读取）。
- 回归：view-service/renderer-offturn-fold-regression.js（G1～G10＋M1～M12）；夹具 renderer-offturn-fold-fixture.py（w2／w1／boundary／gatea／gateb／reconnect／legacy／split／history／evict 十模式）；登记 tools/run-browser-regressions.py SUITES。
- 设计稿：docs/reviews/r61-n2-offturn-fold/DESIGN.md（40491a69，已批准）。
- 复跑命令（普通用户、静默串行）：
  - NODE_PATH=<playwright node_modules> node view-service/renderer-offturn-fold-regression.js
  - PATH=<venv>/bin:$PATH NODE_PATH=<playwright node_modules> python3 tools/run-browser-regressions.py --receipt <path>
  - PATH=<venv>/bin:$PATH sh scripts/run-build-tests.sh
