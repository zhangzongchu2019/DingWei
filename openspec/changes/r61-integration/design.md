# Design

## 集成策略
第一段固定基main206d41b2，按admission flake、helper flake2、N-1a v2、N-2 v2、时序窗v2、browser followup、mutant revive顺序，每项一个no-ff合并。每次操作前核远端tip全值，任一偏离派单即停报，禁止追浮动新提交。实际七次均ort自动合并，无文本冲突、无手工改产品。

两处浏览器刀语义独立：unhandled-group-action处理catch路径；legacy-group-deadlock处理零组隐藏区。另保留silent-group-action-feedback，白名单以429cf1dd为准。自动合并不免除这次内容校验。

## 测试与失败
SOURCE_COMMIT为第七merge锚38b9538dd，测试期间产品和测试均不改。build.sh --test-only包含canonical两profile，指定codex guide格须实际ok；m8与store三遍-v落文件、PG strict、锁定DW_DESKTOP_INTEGRATION_PYTHON；浏览器全量及strict。

Z-3按派单7e622c94d104既定口径，失败原日志不能以单跑或择优覆盖。环境NoSpace首轮独立保存，工具链只迁移本席自有目录，校验后原路径保留映射，恢复后同源完整门重新执行。SKIP逐名与条件列出，不冒称0SKIP。

## 后续阶段
VERSION保持0.2.3.40；后续N-3/N-1b/N-1c只有独立复核通过并冻结后才可另行合入。不推main、不构建发布件、不部署。本段证据不等于最终r61整体验收或发布GO。
