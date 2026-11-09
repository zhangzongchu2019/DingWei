# Tasks

变更：r15-session-history-persist；基线 `c248506cf120c3af2a06eeaeeae3038f596537b1`。本单只写 OpenSpec 提案，不写产品代码、不实现、不 apply、不操作现网／预发。

## 1. 提案

- [x] T-01 核实内存 ring、重启／断线清理、mux epoch/cursor 和现有持久表的职责，完成 proposal、design、context、spec。
- [x] T-02 按 rulings ㉚～㉜ 回写 PM-R15-01～08 定值，并同步容量、淘汰、隐私与验收规格。

## 2. 后续（另行授权）

- [x] T-03 待业主／PM 另行授权后派实现；本单不授权实现、apply 或环境操作。 【甲；证据：ruling ㊲ @ a5a4de214cc9c28ea08aabb8952a4c9b1387522e; docs/reviews/togpt60-r50-r15-final-20261103/REPORT.md @ 43b4d20d1268e6974dde6edc2508ed9a9d8edce5】
- [ ] T-04 由独立执行者验收重启、重连、淘汰、权限、撤权清理、游标连续性、容量／TTL、分页至最早记录、滚动位置、到底／gap 提示、实时切点、迁移和回退；未执行不计 PASS。 【乙；交接：最终重构后尚缺独立执行者完整重启/重连/撤权/迁移回退矩阵；由 QA 在最终源锚复验。】
- [x] T-05 按 rulings ㉞ 定值 PM-R15-09：每页最多 100 个事件；4 MiB 与服务端 2 秒时限沿用原建议。

## 交接记录

- 2026-11-02T03:33:37Z · togpt60lunazzc.zzc · commit 42b9c9238cdec9ab69cf42b9347a5d82c1345f2b
  - 完成：在 c248506cf120c3af2a06eeaeeae3038f596537b1 核对事件 ring、关闭清理、SSE／Console mux 快照和 cursor 现状；根据只读事实报告写入 4 份 OpenSpec 文档，含 8 项 PM 决策和 11 个验收场景。
  - 阻塞：PM-R15-01～08 未定，推荐值仍为提案，不是已批准实现参数。
  - 下一步：strict 已通过；推送本分支并核对远端 tip/tree；等 PM 决策后另派回写／实现。

- 2026-11-02T04:30:43Z · togpt60lunazzc.zzc · commit 03e3cf3be242bdfcb95af7a597cffe1385e2b6cd
  - 完成：依 rulings ㉚～㉜ 将 PM-R15-01～08 标记已定；修订持久化容量与最旧淘汰／公平性建议，明确 eventRing 独立预算和不调整建议；更新规格淘汰验收并删除固定 1000 条场景。
  - 阻塞：无；T-03 实现仍未授权。
  - 下一步：strict 已通过；提交、推送并核对远端 tip/tree；等待后续授权。

- 2026-11-02T04:33:30Z · togpt60lunazzc.zzc · commit 8746611baf410d242db9ca84bcc99e356ed62f4d
  - 完成：按 rulings ㉝ 补充 Console 向上分页、游标、页面／请求预算建议、锚点保持、到底／gap 提示与实时／重启衔接；勘察 renderer 确认现有滚动容器只有贴底跟随，向上加载属新增前端工作。新增 R15-S012～S016；R15-S006 已覆盖他人不可见。
  - 阻塞：PM-R15-09 页大小及单次请求预算待业主确认；T-03 实现未授权。
  - 下一步：strict 已通过；推送本分支并核对远端 tip/tree。

- 2026-11-02T05:18:57Z · togpt60lunazzc.zzc · commit 62266ec56fa94be1b9f4145967f92c49606582f4
  - 完成：依 rulings ㉞ 将 PM-R15-09 定为每页最多 100 个结构化事件，保留 4 MiB／2 秒时限；更新 design、spec 验收与历史建议行，T-05 闭合。
  - 阻塞：无；T-03 实现仍未授权。
  - 下一步：strict 已通过；推送本分支并核对远端 tip/tree。

## 3. R15 实施设计（本单授权，只写文档）

- [x] T-06 基于 r48 `1e17255f28ced042784337903914ca8c126ef4cf` 补齐方案 A 表结构/索引、0075 编号、主库与独立文件权衡、写入挂点、容量/TTL/撤权、代际/分页、权限、回退和逐需求测试计划；未修改产品代码。
- [x] T-07 保留 proposal 原有 PM-R15-01～09 裁定和历史选项原文；本次只在设计/上下文添加有标注的实施解释，不覆写 rulings 文本。
- [x] T-08 组长已于 2026-11-02 逐项裁定数据库文件位置、失败语义、清理批量与热路径 SLO、owner 变更和回退 gap 呈现；原提问及裁定标注见 design.md「待组长定」各项。
- [x] T-09 设计冻结复核：审阅所有待定项；冻结后另行派实现，不由本次设计授权实现或环境操作。 【甲；证据：ruling ㊲ @ a5a4de214cc9c28ea08aabb8952a4c9b1387522e】
- [x] T-10 按 2026-11-02T14:05Z 补充令，设计默认关闭的 `DW_SESSION_HISTORY_PERSIST_ENABLED`、关闭态 r48 行为一致性、开关切换历史/gap、关闭态测试及恒开目标变异。

## 交接记录

- 2026-11-02 · togpt60lunazzc.zzc · 本次设计分支（commit 待提交）
  - 完成：在 r48 基线上普通 merge R15 proposal；补充三份授权文档，代码位置均按 r48 文件:行引用；列出待组长定事项和逐需求正反测试/目标变异计划。
  - 阻塞：设计文件位置、helper 写失败重试契约等事项待组长裁定；r48 与 0075 的启动兼容必须之后隔离实测。
  - 下一步：strict 校验、目录范围 diff 检查、提交推送并比对 ls-remote tip/tree；由组长复核冻结后另派实现。

- 2026-11-02 · togpt60lunazzc.zzc · `4684215ee0b0a89086c73443f6af78f588258d3d`
  - 完成：设计三文档已提交并推送；当时 `git ls-remote` 返回的分支 tip 与本地 HEAD 均为 `4684215ee0b0a89086c73443f6af78f588258d3d`，tree 为 `6427ef096ec29b24a041426c58c9498b696f978a`；strict 15/15。
  - 阻塞：待组长复核/裁定 T-08 项；兼容隔离实测及实现未授权。
  - 下一步：组长审阅设计并定案；设计冻结后另派实现。

- 2026-11-02T14:36:35Z · togpt60lunazzc.zzc · commit 待提交
  - 完成：按 14:05Z 补充令写明默认关闭开关、关闭态事件链路与 r48 一致性、新接口未启用响应、开关重启切换与 feature-disabled gap、关闭态验证和恒开变异。
  - 阻塞：T-08 待组长裁定；功能开关补充不替代 R15 设计冻结复核。
  - 下一步：strict 校验、目录范围 diff、提交推送并核对远端 tip/tree。

- 2026-11-02T14:36Z · togpt60lunazzc.zzc · `983a7cb1bb62e16417650e2a268afb2c4a70baf1`
  - 完成：默认关闭开关设计已提交并推送；提交时 OpenSpec 1.16.0 strict 全仓 15/15、diff-check 通过。
  - 阻塞：T-08 待组长裁定，T-09 设计冻结复核待进行。
  - 下一步：由组长复核开关切换语义与待定项；另行派实现。

- 2026-11-02 · togpt60lunazzc.zzc · commit 待提交
  - 完成：按组长五项裁定更新 design.md；撤销主库 0075 方案，采用独立历史库和账本；修正写失败/超大事件仍实时发布及 gap、清理和性能门、换属主删除、回退 gap；保留五条待定原文并逐条标注已定。
  - 阻塞：T-09 等组长复核本轮写回后确认冻结；实现仍排在 R10、R9 后另派。
  - 下一步：strict、目录范围 diff、提交推送并核对远端坐标。

- 2026-11-02T15:11Z · togpt60lunazzc.zzc · `e7afd374deedb67c2b4f96024cf23e0fcf04f9de`
  - 完成：五项裁定已写回并推送；strict 15/15、目录内 diff-check 通过；当时远端 tip 与本地 HEAD 一致，tree=`f57d64c77956b76a2cdcb0f3092a34988f799a59`。
  - 阻塞：组长需复核本次写回后确认设计冻结；实现依派单安排在 R10、R9 之后。
  - 下一步：组长复核并冻结；event_id/generation 身份字段最终对照留待实现阶段写入 context。

## 4. R15 实现补正（2026-11-03 组长交件反馈授权）

- [x] T-11 按三′／三″删除「prior=1 且开态启动即补 feature_disabled」分支；加入连续开态重启无 gap、开→关→开恰一 gap 两格，以及旧分支复原变异。
- [x] T-12 按组长要求运行 store 全包、m8 历史相关、store+m8 历史 race `-count=3` 与 `scripts/run-build-tests.sh`；浏览器运行件未变，复用 163091359 tip 的浏览器读数并注明来源。

## 5. R15 分页顺序补证返工（2026-11-03 13:15Z 组长授权）

- [x] T-13 翻页页内输入同步绕过 live echo hold；重渲同步 flush 后再取差集搬移；分页期间不按 live 窗口淘汰；旧页统一插到时间线原首条之前，并以可见锚点校正 `scrollTop`。
- [x] T-14 回归断言首条为 `SEQ_001`、`SEQ_100` 紧邻原首条之前及锚点 ±2px；新增去掉搬移／去掉滚动校正的独立变异回归并登记进浏览器套件。
- [x] T-15 Chrome、bundled Chromium 各连续 10 次全绿；locked venv 下规范构建门通过。
- [x] T-16 按 13:34Z 初审保持分页后离底历史窗，追加真实 SSE live 事件仍断言旧页首条和锚点；恢复 oldest-first 淘汰变异按 post-live 首条判据转红。
- [x] T-17 移除测试固定睡眠与页面主动 flush，改等 `pendingRenders`／C2 merge buffer 清空；历史 `input_posted`＋同 delivery `user_input` 回显只产生一张用户卡。取锚前 flush 删除变异未红，移除无证据产品行。
- [x] T-18 显式运行 20-Agent 性能门并使数值超 10ms 时测试失败，避免把超限读数报告成绿；7ab03ea4 初审读数超限，14:04:54Z 等组长裁定期间保持 SLO 与写入语义不变，随后依据新裁定继续优化。

## 6. R15 性能门优化（2026-11-03 14:04:54Z 组长裁定）

- [x] T-19 按同步组提交实现有界 Append 写协程；每个 Append 在该事务的持久 cursor 提交后同步返回；失败批次回滚并逐事件重试，不改变 live 发布/gap 语义。
- [x] T-20 增加并发 cursor 顺序、live cursor 对照、重复事件失败隔离测试；性能门断言与计时区域变异齐全，删 p99 断言和把持久写移出计时区两刀均红。
- [x] T-21 隔离环境连续 5 轮性能门全部 ≤10ms；附机核数/load 与原始 paired p99；store、m8、cmd/workpulse 全包通过，`TestSessionHistory*` race 三包 `-count=3` 通过；完整构建门与 OpenSpec strict 通过。超范围全包 race `-count=3` 达到默认 600s 测试超时，单独记录，不计通过。

## 7. 新平台历史键修复（2026-11-09 业主授权）

- [x] T-22 统一新平台历史写入/读取键为 `AgentKeyID(owner_lc)`、Agent ID、`NameLC()`；旧平台键保持不变。核对现网未启用持久化且历史库不存在，记录无存量迁移需求。
- [x] T-23 增加真实 Hub、登录 cookie、helper `session_event` 与全名 history GET 往返测试；验证跨属主、短名和错误全名均 404，并登记 `HISTORY_NEWPLAT_ROUNDTRIP`、`HISTORY_NEWPLAT_OWNER_ONLY` 判据。
- [x] T-24 增加读取 owner、读取 session、写入 owner 三项存储键回退变异，逐刀红于对应标记。
- [x] T-25 执行 m8 与 store 全包、R15 浏览器回归、`scripts/run-build-tests.sh`；记录命令、结果与环境。历史持久化开关仍由单独 GO 控制，本单不启用现网。

## 交接记录

- 2026-11-03T11:29Z · togpt60lunazzc.zzc · 实现源提交待提交
  - 完成：按组长 11:15Z 反馈移除开态连续启动假 gap；新增连续启动无缺口与持久关闭后重启仅一个 gap 两格；三″关闭状态写入失败的不可推断局限写入 design 与交件报告。已执行旧逻辑复原变异，连续启动判据按多出的 `feature_disabled` gap 转红。store 全包、m8 历史相关、store+m8 历史 race `-count=3`、锁定 venv 下完整构建门及 OpenSpec strict 均通过。
  - 阻塞：20-Agent 性能门需隔离性能开关，本轮因未设置 `DW_SESSION_HISTORY_PERF=1` 按用例条件跳过；store 中依赖外部旧 Hub 二进制的一个兼容用例按既定条件跳过。两项均未计通过。
  - 下一步：证据分支以新源 tip 为父追加报告与原始读数；推送两分支并核对远端坐标，交组长复核。

- 2026-11-03T11:35Z · togpt60lunazzc.zzc · `59ea28409c4dcf176c9d41c6a3eea344bd355703`
  - 完成：实现与三′／三″修订已提交；store 全包最后一次在提交前的相同产品源码上通过，race count=3、m8 历史格、规范构建门及 strict 均通过；变异红测绑定新增连续启动判据。
  - 阻塞：性能门和独立验收仍按上文条件/职责保留，不自称通过或关闭。
  - 下一步：生成证据子提交并推送、核对 refs。

- 2026-11-03 · togpt60lunazzc.zzc · rework from `c54b79d22fc3` (source commit pending)
  - 完成：修复历史回放依赖 C2 延迟合并造成的 DOM 差集漏搬，并避免分页逐条渲染触发 live 200 节点淘汰；页内旧事件同步渲染并整体移到既有首条前。新增顺序、锚点及两项定向变异判据。Chrome／bundled 各 10/10、锚点每次 0.00px；`scripts/run-build-tests.sh` 在 `/tmp/dw-filecard-build-venv` 下通过；详细读数见 `docs/reviews/togpt60-r50-r15-final-20261103/REPORT.md`。
  - 阻塞：无；修复前 Chrome 失败读数来自组长提供的 prodops 只读证据，未重跑，具体见报告。
  - 下一步：推送本返工分支并核对远端分支 tip/tree，交组长独立复核。

- 2026-11-03T13:34Z 初审追加 · rework source pending
  - 完成：分页后只要用户离底阅读已展开历史窗，就暂停 live oldest-first 淘汰；回到底部恢复上限。实 SSE 后续事件判据及 3 项变异全做。C2 同 delivery 双事件合卡判据通过。测试内固定睡眠与主动 flush 已移除；取锚前 flush 删除不转红，故删产品行。性能门实测 paired additional p99 约 40.6ms，已让其超过 10ms 时明确 FAIL。
  - 阻塞：性能门超阈值；设计冻结要求超限报告并等组长裁定，不擅改 SLO 或持久化语义。
  - 下一步：三项返工最终证据、完整构建门与 final-tip 性能读数复跑后一次交件。性能优化预计需额外 30–45 分钟分析和实现，且须组长裁定继续；按当前冻结设计，不将未授权优化纳入本次交件。

- 2026-11-03T14:04:54Z 性能门裁定追加 · togpt60lunazzc.zzc · 开工
  - 完成：确认当前 7ab03ea4 源提交及干净 worktree；选同步组提交路径，目标为写协程收集同刻队列、事务内分配各自 seq 并提交后才返回，失败时逐项重试。已完成初版代码和 store 并发 cursor 测试，初轮单次性能读数 6.079566ms；该单轮不作为五轮验收。
  - 阻塞：全包 Go 测试和五轮隔离性能、变异、race 证据未齐；本次仅作开发中间进度。
  - 下一步：完成五轮 ≤10ms 与所有测试/变异证据；本轮预计交件 2026-11-03T14:50Z。

- 2026-11-03T14:39:45Z · togpt60lunazzc.zzc · source `7dad334d8e53991ad55ac7ccf19ea73d84f0a505`
  - 完成：同步组提交固定批量/等待上限并在事务提交后同步返回 cursor；同会话并发 store 顺序、Hub live cursor 对照、错误隔离均通过。20-Agent paired p99 五轮 5.411771–6.217092ms；两项性能变异各红。Go 全包、涉存储 race 三包 `-count=3`、锁定完整构建门与 strict 均通过。宽范围全包 race 三次因默认 600s 超时未计通过；具体裁定、读数、失败尝试和原文日志在 review 报告。
  - 阻塞：无实现阻塞；由独立组长最终复核。
  - 下一步：推送 source 与证据追加提交，核对远端 tip/tree，交组长整体复核。

### 本次清账交接（2026-11-08）
- T-04：最终重构后尚缺独立执行者完整重启/重连/撤权/迁移回退矩阵；由 QA 在最终源锚复验。 证据边界：docs/reviews/r15-accept-browser-20261103/README.md @ fdcf455a6ebf2a0139f98ad9386e67dbd86adb93; docs/reviews/togpt60-r50-r15-final-20261103/REPORT.md @ 43b4d20d1268e6974dde6edc2508ed9a9d8edce5。
