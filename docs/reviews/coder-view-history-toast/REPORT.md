# S-VIEW-HISTORY-TOAST delivery

开发完成，待 opus50ts 独立审查；**未部署、未访问真实View/Agent/Hub**。仓库 `git@git.code.tencent.com:wego-ai/dingwei.git`，分支 `review/coder/view-history-toast`；基线 r18 `89314543226c7cba6b3b4d34d27ea2c69ded5b45` / tree `34b860eea401bc1ba73b0573702ce91786812e5c`。

唯一产品改动是 `view-service/renderer-canary-sse.html`，新 SHA256：

`e99753c7625da9f75038325f2cd09fc1453ce4a5ba9b43c6aad74a3883af351c`

基线renderer SHA256为 `2d6a7bcc885427fd3b43995c83c4f59609fe1ec1106aac4b5c447421fde83543`。无后端、协议、版本、依赖、包、部署配置或helper改动；其余增量为测试/证据。

## 行为与故障边界

- history/overflow 显示独立中性气泡：“View 只保留每个 Agent 最近约 1000 段输出（或 16MB），更早的内容不在这里显示，可在 Agent 自己的会话中查看；当前内容完整。”
- **仅实际 SEEN_ABOVE_MAX=5000 容量交接**的tracking状态标记 `tracking_limit` 并显示：“部分中间输出未能补齐，已从最新内容继续显示。”未知tracking损失/旧未解决缺口跨epoch仍为真实警告；A4显式epoch_changed的既有中性常驻文案/角色/优先级保留。
- 气泡有独立DOM和计时器、`role=status`/polite、muted色、可换行、pointer-events:none，无按钮/焦点阻挡；10000ms后删除。每原因每连接仅一次，重复不延长寿命；到期后也保持已提示标记。两个原因互不抑制，最多两类固定键。
- 成功SSE onopen重置提示范围；失败尝试、过时代际onopen、同一活跃连接重复onopen不重置。Console聚合模式仅在可信bind进入**不同generation**时重置，重复同generation bind不算新连接。不引入新协议消息或网络重发。
- history内部状态继续保留，保护既有正文不被legacy replay清空；没有将“气泡消失”视为历史恢复成功。新事件继续处理，当前正文与草稿保留。
- streamAlert继续承载连接/重连、replay不确定、recoverable、apply、managed及未知tracking故障；气泡超时不触碰其DOM。history之前已知的真实故障不能被后来的tracking容量交接清除。实际连接“重试”按钮不会被仍保留的history状态劫持成“查看范围”。
- 没有改动A4/A5的重连预算、时间窗、序号游标/去重、正文恢复/清空或成功判据。只在客户端已经放弃容量跟踪、转向最新内容的路径撤去该临时跟踪告警；真实apply/managed等保持。

## 最终验证（逐项钉住上述renderer字节）

Playwright1.64.1，Google Chrome151.0.7922.108，PW_CHANNEL=chrome。使用本地已安装Playwright，只读复用依赖目录；未安装任何依赖。`RESULTS.json`、`WITHDRAWAL.json`、`PROVENANCE.json`和raw根目录为最终/对照证据。

| 测试 | 读数 |
|---|---|
| renderer_integrity_recovery | 22/22 |
| renderer_history_priority | 7/7 |
| A4 overflow/body preservation | 4/4 |
| A4 notice tone（含Chromium CSS） | 15/15 |
| A4 epoch（含Chromium CSS，原测试字节未改） | 19/19 |
| A5 replay state / inline syntax | 8/8 |
| A5 real integrated gap timer | 9/9 |
| 新toast Chromium真DOM＋假时钟 | 18/18，另1项managed VM连接生命周期 |
| T-086 deferred clear | 8/8 |
| renderer实际完整页面 | PASS，1280×900及390×844；见下面并发失败与独立重跑说明 |
| 真SSE replay-seq | 13/13 |
| 真SSE empty-ring | 2/2 |
| 真SSE legacy reconnect | 26/26 |

最终13套分别返回0；不是把所有历史尝试都写成0。新假时钟先明确暂停，再断言9999ms仍在、10000ms消失；重复不续期，失败/过时/重复open不重置，成功重连允许再次提示。覆盖两类提示前后出现的apply/managed/recoverable/未知tracking/conn/replay故障在20秒后仍常驻、重试动作真实发起连接；managed VM补测可信来源/正确generation与重复bind。

### 撤修及变异对照

仅在自动销毁的本地临时renderer副本修改产品片段，测试断言不改，7项均rc1：分别撤去history/tracking气泡路由、将TTL延长到20秒、撤去去重、故意让气泡计时器误隐藏真实告警、撤去重连重新提示、让同generation bind错误重置。前两项是分类撤修，后五项为额外判别力控制；不将修改测试期望当撤修。`WITHDRAWAL.json`保存精确替换及各副本SHA，raw/withdraw-*.log保存失败断言，均基于最终renderer SHA。

### 测试适配与保留的失败

1. history相关旧测试把“history状态保留”与“告警条永久可见”绑在一起，展示断言改为气泡，**正文/草稿、零reset、既有state.kind、真实故障优先级检查仍保留**。history遇到已知故障时新增警告不降级的断言。A5八项状态断言原样，抽取夹具仅补一个提示重置函数stub。
2. 原replay-seq S5要求epoch变化“不提示”，但r18/A4已经明确要求中性epoch提示。用未改r18 renderer独立对照，原S5同样失败；基线日志保留。S5对齐为精确A4既有文案、status/notice、epoch_changed，保留全文/新事件唯一性；S6再增加真实history气泡文案检查，并保留并存缺口故障检查。
3. 旧legacy SSE长正文测试最后一秒可能落在重放中途：一次采到39/48片、6240/7680字，而观测最大值仍完整；未改r18对照26项全过。现在以**真实夹具__done__事件**停止下一轮重连，再取最终样本；没有按预期长度挑样本，既有所有最大正文/事件数、卡片、折叠、真实重连次数、窗口内不误报、出窗可见告警断言原样保留。L4依然只证明出窗不静默，不声称修复已有慢重放重复。
4. 最终字节的一次完整页面真wheel测试在并发负格浏览器负载下出现“用户上翻后被拉回底部”；**没有修改产品滚动代码或该测试断言**，单独复跑同字节通过。失败在raw/renderer-concurrent-gesture-failure.log，独立通过在raw/renderer-isolated.log；RESULTS.json的renderer条目保留initial_attempt。此项存在并发调度敏感性，不把单次重跑说成已修复滚动问题。
5. 开发中新增矩阵抓到的旧真实缺口被容量提示清除、实际Retry被history状态劫持、同generation bind重新提示均在最终候选修正。此前raw/initial、raw/interim、raw/pre-rebind及INITIAL/PRE_REBIND JSON仅为诊断过程，不充作最终提交全量读数。interim还保留一次renderer在测试中变化被SHA门拒绝的说明；最后一轮各项前后字节检查一致。

## 复跑与审查

从仓库根设置现有Playwright的NODE_PATH和PW_CHANNEL=chrome后，直接运行RESULTS.json列出的node文件；`renderer-t086-deferred-clear-regression.js`的cwd是view-service。新测试为 `node tests/view-history-toast.cjs [renderer-path]`，可用 `--only=history`/`--only=tracking`单跑；浏览器均自行启动并关闭，本地HTTP/SSE夹具使用随机loopback端口。

源码、测试和证据仅通过Git交付，所有临时撤修副本及基线HTML已清理；不清理共享缓存或其他席资源。未运行Go/helper全库：本次没有相关代码变更。请求opus50ts审查最终精确tip/tree，特别核对真实故障隔离、history内部状态与连接重置边界、测试展示适配和完成事件采样没有削弱原覆盖。无部署授权或部署动作。
