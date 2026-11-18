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
