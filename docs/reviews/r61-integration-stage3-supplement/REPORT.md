# R61 第三段晚到令补件：收件元数据、0ab 对照与 Hub 重启历史回放

组长2026-11-18T14:29:24Z令于16:00Z后注入，交叉于N1b＋N3合后交件a85b6c7d。实际合并源已是4581b34ac1e4b9973d12c559cf6ac7772107bebf/tree5c73e93df0255e73137428a9376b65321e91279a，父c430＋1a2，恰为本令对照tree。N1b merge cd01、N3 merge4581，第一父九个merge，无冲突；既有a85原始全门和5轮都锚定同一产品字节，不是N1b-only附加门。故不reset、不重复合N3、不重跑已满足的同源完整门，只补本令新增的三项。VERSION仍0.2.3.40，N1c未入，无main/预发/现网/发布件动作。

## 本地收件只读实证

只用本人SQLite mode=ro查元数据，未read/ack、未改状态，未导出正文、信封或turn值。快照UTC、原epoch、完整delivery/source ID和正文SHA见own-inbox-metadata.json。

| 同令记录 | received UTC | 本地状态 | admitted |
|---|---|---|---|
| N3追加令 cfbc8273 | 12:43:00.199005Z | admission_unknown/model_admission_unknown | null |
| 12:57转述 cba2050a | 12:57:47.552883Z | processed | 14:28:54.548584Z |
| 13:18重申 4b1d1ea8 | 13:18:41.944676Z | admission_unknown/model_admission_unknown | null |

**结论一句：同令已到本地，12:43与13:18两行模型准入未确认；不能把admission_unknown硬写成“必定未注入”，也不能据caller ID零前缀命中判“未到”。** 本地request_id列是delivery ID，envelope/meta.source_message_id为不同Hub源ID；e3fcb315c472、647d98666384的caller请求ID→源ID映射不在本地库中，无法逐一硬配。13:18重申正文同时引用两caller ID，可供发送方对账；本件只给原元数据和正文SHA，不补造映射或时间。首次本令开工回执source4c47786850b74e9da2b67db0f3244dd8为unknown/无delivery，未重发。

## 按本令补0ab同测试对照

0ab43c630c37fdf01ce68d5c66c0db0141a682af独立detached干净树，白名单环境、PG strict，实际失败落点TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent，count20串行，20 PASS/0 FAIL/0 interrupted、rc0。命令/UTC/SHA见baseline0ab-newsession20.json与原日志gzip。原a85中的main206辅助/匹配对照保留，不拿它替代本次指定0ab，也不由20次小样本推断概率。当前合后源五轮依然是此前完整固定五轮5/5全包rc0、每轮998顶层PASS/0中断，不重复起轮次。

## 真正不同OS进程的Hub重启＋View历史

新增外部Go test overlay探针，生产源码及已冻结测试字节都未改。独立detached源4581，测试binary含实际Hub.New、HandleAgentWS、授权HTTP history、SessionHistory与SQLite实现；每次Hub运行是不同OS子进程，不是仅重开history句柄。共用同一app/history SQLite文件与同一隔离真实PG身份库，PG跨Hub进程保持，因此稳定Agent ID、授权组、用户会话不靠重建/拷贝内存。

通过合法业务层创建合成身份/组和测试会话，真实带凭据WS投递到Kimi，再用admitted echo协议写历史。两种来源为Opus50TS Agent席与timer命名的已认证Agent席，沿用既有N3夹具；二者canonical kind都为agent，以display_name区分，不冒称真实定时器调度或CLI模型执行。先断言Hub-attested/trusted、两来源实际存在，防止空对空比较；再正常关闭首Hub进程，冷启动另一个PID，从真实受权/view/Kimi.zzc/history再次读取。完整事件JSON（含全部provenance）前后深度相等，2行；成功PID与快照见restart-result.json。

生产renderer另用两个全新浏览器上下文渲染实际重启前/后HTTP快照，SSE为空，不携带旧浏览器实时内存；每阶段2个turn/2个来源图标，Agent与timer席标题逐字一致。restart-test rc0，restart-browser rc0。该浏览器层为捕获真实HTTP数据后的离线生产renderer回放，不冒称浏览器直接登录live Hub页面。所有身份、PG、HTTP/WS均本机隔离fixture；无在线席位、现网或预发连接。

## 夹具失败完整披露

1. 首次overlay编译rc1：Go1.22.2 vet不能打开仅overlay存在的新测试文件，未跑测试。原日志restart-build-overlay-vet-failed保留。
2. 首次实际运行rc1：借用MemStore夹具省略User.Type，PG建用户拒绝，尚未启动Hub。补显式UserMember/CreatedAt后用新目录完整重跑。
3. 第二次实际运行rc1：夹具误期待timer席kind=timer；既有N3合同该已认证Agent席实际kind=agent/display_name=timer。改正预期，两次失败源码/编译与运行日志/SHA保留；不是产品修改或已运行门变绿。
4. 最终完整fresh运行rc0，两个新Hub PID、前后真实HTTP及浏览器全流程一起通过，未拼接失败轮次片段。仅新增overlay探针编译带-vet=off；此前同源canonical/完整Go门的vet和结果未关闭/未替换。

数据库、私有状态、实际临时PG DSN、会话/Agent凭据和测试binary不进Git；只发布脚本、脱敏原日志副本、结果与SHA。失败原始日志本机保留，原/发布/gzip SHA及替换数见raw-logs.json。probe-build-provenance记录源码overlay和本地binary SHA，不把测试binary称发布件。

## 交接

同源完整门见不可变a85b6c7d2fbe3bc94ab337dda266d8033662cc26:docs/reviews/r61-integration-stage2-n3/REPORT.md及VERIFY；本补件只增加0ab对照与进程重启格、收件审计。作者门不代替组长/独立复核，N1c是否进入r61待组长裁定；不推main、不部署、不出发布件。

## 固定合后源完整门逐项指针

以下都是4581合后源的本次原始读数，已在a85交件；不将其当本补件重新执行，也不与N1b-only cd01读数相混。

| 门 | 结果 |
|---|---|
| build.sh --test-only | BUILD_TESTS_OK helper_profiles=2 python=27 shell=2；两profile各1277，inventory97，codex guide格绿 |
| 真PG strict m8/store... -count=3 | 初始rc1：仅NewSessionName已知interrupted(9)一条FAIL；store各包rc0；原失败保留 |
| Z-3固定全新5轮 | 5/5整包rc0；每轮998顶层/1690含子项PASS，28逐名SKIP，0FAIL、0interrupted |
| focused race ACK/queue_full/failure_notice/history | rc0，103顶层/175含子项PASS，17逐名条件SKIP，0DATA RACE |
| 浏览器全量 | 48/48，aggregate_rc0，history8/8、r15三格绿 |
| 三向真实Hub/helper进程兼容 | 旧501＋新helper、新新、新Hub＋真实0.2.3.39各rc0，通知/协商与FIFO判据符合 |
| 合并源strict | 45 passed/0 failed |
| 本补件文档strict | 9a831a709a45a8c3acad4c7e2570c6344e0daf33上45/0，原日志与UTC见final-document-strict.json/txt |

真实PG证据为180003（18.3）与启动/SELECT链，逐名SKIP和Z3五轮原始日志见a85对应evidence。新补0ab20/0已取代“仅有main对照”的缺口；不改变初始rc1读数。最终文档strict后只补报告与校验清单，spec字节未再改。
