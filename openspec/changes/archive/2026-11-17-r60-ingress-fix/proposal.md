# R60 helper入站与写截止兼容修订

## Why

r60切换期间部分旧0.2.3.39 helper无详细原因断连，本地固定旧包已复现64项入站界与2秒物理写界在突发和短停读时关闭连接。需要在保持唯一Reader、可靠提交顺序与资源界的前提下扩大明确兼容窗口，区分等槽与物理写，并给退出路径留下有界可定因证据，而非靠放宽上线门或回退到Pong被业务饿死的旧行为。

需求来源：业主R60授权rulings ㊿-补123，review/opus50tc2k/rulings commit 2f5560b7756d1e971a16ebdb85395970b5dd24fa；组长2026-11-17T18:04:11Z派单S-R60-INGRESS-FIX（本席收到正文，delivery_id未提供，不补造）。方案依据coder56solzzc/r60-prod-disconnect-analysis 0d8e2539。

## What Changes

1. helper入口2048项/连接、65536项/Hub，保留32MiB/256MiB字节界，保持唯一Reader与有限顺序worker。
2. 会话写拆成等槽2秒、小控制物理5秒/大数据物理20秒，显式调用方取消继续优先。
3. 每连接session_end与overflow/物理写各一次有限枚举诊断，非阻塞≤4096字节journal及drop计数。
4. WS同消息分片仅在真实限速Ping竞争不分片红的前置证据成立后加入；不做显示合并，不改canonical事件/ACK/正文ID或顺序。
5. 固定Git对象的旧39真helper压力回归、删保护、原集成与五轮门、预发39升级计划（不执行）。

## Impact

改动internal/m8及其测试，必要的obs固定计数/日志封装；无DDL、0077/outbox语义、公开业务API或发布操作。VERSION仍0.2.3.40；基9818b979，新分支coder56solzzc/r60-ingress-fix，不改r60-integration/main。旧PASS不外推，新锚须另复核。

## 待确认假设

无新增授权假设。2048/65536、32/256MiB、5/20/2秒为组长初值，最终压测若不足只停报，不能自行突破。有限兼容窗不等于旧39在任意过载/崩溃后都有端到端重放。

## 待 PM 决策

P-01 预发stagingtest升39需业主在预发Console点击，本文不执行；正式构建与任何现网动作仍须另令。
