# Design

## 集成策略
第一段固定基main206d41b2，按admission flake、helper flake2、N-1a v2、N-2 v2、时序窗v2、browser followup、mutant revive顺序，每项一个no-ff合并。每次操作前核远端tip全值，任一偏离派单即停报，禁止追浮动新提交。实际七次均ort自动合并，无文本冲突、无手工改产品。

两处浏览器刀语义独立：unhandled-group-action处理catch路径；legacy-group-deadlock处理零组隐藏区。另保留silent-group-action-feedback，白名单以429cf1dd为准。自动合并不免除这次内容校验。

## 测试与失败
SOURCE_COMMIT为第七merge锚38b9538dd，测试期间产品和测试均不改。build.sh --test-only包含canonical两profile，指定codex guide格须实际ok；m8与store三遍-v落文件、PG strict、锁定DW_DESKTOP_INTEGRATION_PYTHON；浏览器全量及strict。

Z-3按派单7e622c94d104既定口径，失败原日志不能以单跑或择优覆盖。环境NoSpace首轮独立保存，工具链只迁移本席自有目录，校验后原路径保留映射，恢复后同源完整门重新执行。SKIP逐名与条件列出，不冒称0SKIP。

## 后续阶段
VERSION保持0.2.3.40；后续N-3/N-1b/N-1c只有独立复核通过并冻结后才可另行合入。不推main、不构建发布件、不部署。本段证据不等于最终r61整体验收或发布GO。

## 第二段：冻结N-1b追加
依据组长2026-11-18T12:24:42Z令，在第一段0ab43c63上仅no-ff合入b81737d0（产品ed7259fa），形成固定源cd01d67a；N1a638已在祖先链。自动合并共享测试，无手工产品解冲突。门新增focusedrace与真实501Hub/39helper双向及新新正例联测；legacy busy不dead/不发通知，新新拒绝为queue_full dead且一条通知。模型消费仅fixture暂停，运输/协商/accept/ACK与Hub落库为真实进程，不宣称CLI全回合。对应R61I-005/006。

Z-3首轮三遍和资格五轮分列，初始失败保留、不改SKIP。固定5轮至少3轮全包rc0，其余只许既定三格interrupted(9)；任何名单外或非该机制FAIL均停止，不择优不扩名单。独立基线206d41b2目标count20对照保留；本段4/5全绿，第五轮仅已知同落点，按既定规则合格。PG strict启真实临时库并留版本行；SKIP附条件。VERSION与发布边界不变。

## 第二段补令：N1b后追加冻结N3
对应R61I-007。原N1b-only c430及其门记录保持不可变；收到12:57:47Z补令后，在其上追加N3 no-ff，来源1a2a4160、产品9313197f，合后源4581b34a。共享hub与runner由ort自动合并，无冲突、无产品手工改动；N1b R1/R2文件保留，N3 legacy重锚显式nil及五元组sidecar/Append逻辑逐字等同其冻结源。应用库0077不改，history独立0078。VERSION、helper、renderer产品页不变，新增history浏览器suite使runner48套。

重新在双项合后源跑全部门和真实501/39协议兼容，不复用单项或N1b-only绿。初始m8中断按既定Z3保存首轮并固定5轮，实际5/5全包绿、每轮0中断；main辅助reconnect20次18/2，实际NewSessionName落点20次19/1，全部原rc/SHA保留，不从小样本推断失败率。日志审计逐条要求实际FAIL在自身RUN范围内有中断诊断，不以别处清理日志代替。本段作者门不替代组长/独立复核，N1c仍待其独立冻结令。

## 第三段重启回放与晚到令核验

14:29:24Z令注入时合后源4581b34a已包含N1b/N3，父c430＋1a2、tree5c73e93d；因此不重复merge或改写历史。R61I-S010以两个不同OS worker执行实际Hub/授权handler验证，持久PG身份与SQLite历史跨进程保持；先确认两条trusted/hub来源存在，再完整比较重启前后history，防止空对空绿。renderer层用真实HTTP快照冷页面，不冒称浏览器连liveHub。timer仅是已认证Agent席的display_name，kind仍agent；不引入新产品语义。对应补件包含初次overlay-vet编译失败、PG夹具enum失败及错误kind预期失败，均为夹具修正后fresh全流程重跑，产品不变。

最新令指定0ab43c63同失败测试对照，补20次全部绿，不将先前main206对照替代它、不推断小样本概率。本人收件admission_unknown仅代表准入未确认；delivery/Hub源ID不等于caller requestID，无法硬配时如实留缺，不由零命中声称未投递。
