# 复盘：View 刷新后输入／输出缺失与错序（2026-11-12～13）

> 状态：定稿（2026-11-13，现网换件 02:33:11Z、业主目视 02:37:31Z 通过）。依据 rulings ㊿-补110、㊿-补111。
> 时间一律 UTC；括注 PST 为业主本地时间。

## Summary (English)

- **What the owner saw:** after refreshing a View page, first the Agent's outputs were missing; after the first fix, inputs and outputs showed but in mixed order (inputs piled at the top, new messages above older replies).
- **Two root causes, one per symptom:**
  1. The Hub's history API returns each event as a JSON object; the page parsed it as a string, failed, and silently dropped the whole history page.
  2. On load, the page also receives the live stream's replay of recent events. Inputs arriving that way (`input_posted` delivery cards and `user_input` without a timestamp or sequence) were not matched to the turns already drawn from history, so they became separate items sorted to the top.
- **Outcome:** full fix c22bb63f (renderer only) went live at 2026-11-13T02:33:11Z; the owner confirmed the display was normal at 02:37:31Z.
- **Why it took ~12 hours instead of ~2:** every test and replay fed the page hand-made or partial data (strings instead of objects; history without the live stream). Real page-load traffic was captured only at 00:30Z, about 10 hours after the first report; with it, the cause was visible within minutes.
- **Faster next time:** capture the owner's real page-load traffic (history + live stream) first, replay it against the page, and judge with a whole-timeline checker before and after any fix.

## 一、时间线

| 时刻（UTC） | 事件 |
|---|---|
| 11-12 14:4x | 业主截图：刷新后 View 看不到输出 |
| 15:xx～16:0x | prodops 只读查明数据面完整；togpt60 用合成夹具与真实标识矩阵均未复现（夹具 event 为字符串形） |
| 16:29 | togpt60 用真实首屏 100 条回放：r57／r59 均 0/22 ⇒ 根因一 |
| 16:4x～17:00 | 远端核对：所称「修复在 01896295」实未推送；令转入修复 |
| 17:10 | 修复 2e1e4c9d（只改 renderer）；组长复跑：新套件 22/22、两刀红、rbt 1203×2、m8、浏览器 42/42 ⇒ 冻结为 r59 发布锚 |
| 18:36～19:5x | 预发 A 段：组长判据两次写错（GET 响应 SHA≠文件 SHA；功能核需业主登录），回放先报 0 段系计数选择器只数 turn 分组 |
| 23:13 | 业主令「You must update it now」（㊿-补107） |
| 23:19 | 现网 View 换件；真实首屏回放（只喂 history）输入 14／输出 14 ⇒ 组长称「除业主目视外已结」 |
| 11-13 00:22～00:34 | 业主截图：输入不见→实为排到页顶、新输入在旧输出之上（㊿-补108） |
| 00:30 | prodops 抓页面加载两路真实流量（history＋SSE 15 秒，1001 帧） |
| 00:5x | 组长用抓包回放现网件：91 张独立输入卡片排在最前（＝input_posted 91 帧）⇒ 根因二 |
| 01:24～01:32 | togpt60 4019441b：配对全对、带时间节点 106，仍余 7 张错位；浏览器 43/43 |
| 01:36 | 业主「wait for full repair」（㊿-补110） |
| 01:58 | 组长复核 a0d68684（与 4019441b 同树）：7 张 23:15Z 后输入卡片仍在旧 turn 之上 ⇒ 退回 |
| 02:16 | togpt60 推 c22bb63f（tree 5fa872c2，renderer 74bb8577） |
| 02:18～02:30 | 组长复跑真实两路判定器：时间倒挂 0（修前 7）、配对错 0、69 张卡片均早于首屏；三套 rc0、两刀红；浏览器 43/43 ⇒ 冻结 |
| 02:30:26 | 现网 GO-VIEW-ORDER（只换 renderer 一个文件，Hub／View 不重启） |
| 02:33:11 | prodops 现网换件：磁盘 74bb8577、GET 200、Hub／View PID 不变；新巡检 02:34:04Z 首轮 PASS |
| 02:37:31 | 业主刷新目视：「看起来轮次交错，显示正常」（㊿-补111） |

## 二、根因

1. **线格式不符**：Hub `/history` 的 `events[].event` 是 `json.RawMessage`（对象）；renderer 写 `JSON.parse(row.event)`，对象抛错后 `continue`，整页静默丢弃。既有回归夹具都用 `JSON.stringify(event)` 造数据，与真实线格式不符，长期假绿。
2. **页面加载两路未建模**：真实页面先开事件流、再取 history；事件流初始重放把近期输入再发一次。`input_posted` 投递卡片与缺 `ts`／序号的 `user_input` 没有与 history 已画出的 turn 归并，成为独立节点，按 0 时间排到最前；新输入同理排到旧输出之上。所有回放都把事件流打成 204 或空流，所以看不到。
3. **验收口径偏离业主视角**：组长在只喂 history 的回放通过后即称「已结」，没有用业主真实的页面加载流量验收。

## 三、为什么慢（耗时分解）

- 第一次截图到首个真实复现约 2 小时：前期复现用合成数据。
- 上线后再发现错序约 1 小时：验收只覆盖 history 一路。
- 拿到真实两路抓包距首报约 10 小时；拿到后数分钟内定位到 91 张卡片。
- 首个完整修复候选被退回一次（4019441b／a0d68684 余 7 张错位）：整页判定器在上线前拦下，没有第三次让业主看到错页；从退回到完整修复上线约 35 分钟。
- 总计：首报到业主目视通过约 12 小时。
- 流程摩擦：判据写错导致预发两次停点；消息交叉（执行席按顺序处理积压来信）；一封含半角井号的令被闸放行（管道退出码陷阱）。

## 四、下次更快的做法（已执行或拟执行）

1. **先抓真实流量**：业主报显示类问题，第一步由 prodops 只读抓该会话页面加载两路（history＋事件流前 15 秒，脱敏），作为复现与回归夹具。
2. **整页判定器入回归**：断言「旧上新下、每个有输出的 turn 恰一个输入且在前、同一输入只显示一次、无成批独立卡片排在最前」；修前修后都跑。组长判定器 replay2／replay3 已用于本次复核，拟由 togpt60 固化为浏览器套件。
3. **夹具从真实响应生成**：禁止手写线格式；夹具来自 Hub 真实响应或脱敏抓包。
4. **判据先读服务代码**：换件判据按服务实际产出写（如 `_serve_file` 改写注入）。
5. **验收以业主视角为准**：显示类修复须经真实流量整页回放＋业主目视，才称结项。

## 五、对工作效率的影响

- 自 11-12 14:4x 起，业主刷新任一 View 都无法按「输入—输出」对照阅读，直接降低日常交互效率；这是本轮最高优先级（㊿-补109）。
- 版本验收原则（㊿-补110）：每版须提升工作效率；影响业主正常交互的回归优先级最高。

## 六、遗留与相关

- 投递顺序（FIFO）：r59 helper 在常规忙等下保持到达顺序；两处缺口（1800 秒上限后、派发意图后重启）已由 coder56 测试定性（0dac7205），修否待业主定。
- 证据坐标：修复 2e1e4c9d、4019441b、完整修复 c22bb63f；抓包 review/prodops/view-input-capture-20261113 216186ee；现网换件 review/prodops/r59-prod-view-20261112 e3cadb6a、第二次换件 review/prodops/view-order-20261113（GO 385614c8d721）；现网真页回放判定器 opus50tc2k/view-order-live-judge aef0f183（S-VIEW-ORDER-PROD-REPLAY 44ec658733a3，结果随交件追加）；rulings ㊿-补107～111。

## 七、上线后真页回放与残留（2026-11-13T03:xxZ 追加）

- 判定器：opus50tc2k/view-order-live-judge（只读、只出计数）。交件 review/prodops/view-order-20261113。
- 前两轮假红：直连 View 回环取不到 history（History 由 Hub 的 /view/{会话}/history 提供，需 Console 登录；View 无此路由），页面只靠事件流画出时间线。教训：判定器先在现网路径上确认 history 状态为 200 且有行，否则判回放无效，不判页面。
- 第三轮（--history-file 喂现网首屏 100 行）：新页 vs 旧页，配对错 1／10、1／12；无时间输入卡 0／95、0／58；倒挂 0／1；重复输入均 0。
- 第四轮（badTurns 定位）：opus50tc2k 最老 turn 属首屏截断（可接受）；其余缺输入的都是最新 turn（opus50tc2k 3 个、opuszzcgmail 1 个），同一 history 下数量随事件流变化 ⇒ 实时事件流一侧的新 turn 未与其输入归并。
- 处置：不回退（新页各项优于旧页，业主目视正常）；作为残留缺陷进下一版（S-VIEW-ORDER-LIVE-PAIRING，先抓包定根因，回归以 badTurns 为门）。
