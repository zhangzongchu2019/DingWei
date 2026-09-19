# G / 0.2.1.0 · ③ Agent 分组后端交付

- 分支：`coder56solzzc/g-grouping-backend-0210`
- 产品锚：`91e24a926f3e7ebf9f4e179b75f7fc52b1d512ae`
- 基线：`gfup/release/0.2.0.7` 的 `7f02e92d58e67ebf0fee6bfd2f1f3740d6cdffb6`
- 更新时间：2026-09-19T09:32:37Z
- 状态事实：实现与仓内证据已推；未合并、未部署、未宣称生产验收通过。

## 实现事实

1. 新迁移 `0036_agent_grouping.sql` 建立组与成员关系：
   - 组码必须逐字匹配 32 个标准 base64 字符；
   - 一组由单行保存唯一组长；组长机械加入成员表，未先改组长不得移出；
   - Agent—组是多对多，同一 Agent 可属于多个组。
2. 未显式编组的 Agent 使用 `default-owner:<owner_key>` 合成默认组；只要存在显式组成员行，默认组即不再加入，符合“指定编组号即覆盖”。
3. 跨 owner 只接受持久化 canonical `SessionName` / `FullSessionName` 的逐字精确命中；不做全域短名或模糊搜索。
4. 路由裁决覆盖在线快路径、可靠离线队列路径、同 owner 短名补全与跨 owner 完整名路径。
5. 组外与不存在均收敛到同一错误 `组内找不到该Agent`。sessionHelper / send.py 路径仍是先接受发送、随后异步收到该正文；没有把它伪装成 send.py 同步失败。
6. 业主豁免按发送者身份判：
   - 飞书使用入站 sender openid 对照已登记 active member；同一机器人/同一通道的未登记 openid 仍被拒；
   - console principal 由 Hub 在 capability 校验后覆盖外来头，再经 View Service 签名请求带回 Hub；只有存在唯一 active owner scope 的 principal 获豁免。调用方伪造的 principal 头在鉴权前被删除。
7. 最简入口挂在已有认证后台 `/admin/agent-groups`，可创建/更新组长、加入/移出成员并查看端点。正式左侧 Agent 设置页不在本交付范围。
8. 分组表为空时保持 G 之前的路由行为，作为迁移与回滚状态；首个显式组建立后启用编组裁决。

## G1–G5 证据

- G1：`TestAgentGroupingReachabilityAndIndistinguishableMiss` 跨 owner 同组直达；组外拒绝。
- G2：同轮构造“存在但不同组”与“不存在”，同步错误字符串及异步回信 body 均逐字节相同。
- G3：`TestGroupingOwnerExemptionUsesSenderIdentityNotChannel` 同轮覆盖飞书 owner / 非 owner，以及 console owner principal / 普通 principal；非 owner 即使走同一通道仍受限。
- G4：`TestAgentGroupingSupportsMultipleGroupsAndOneLeader` 证明一个 Agent 同时被 A、B 两组触达；存储测试证明每组单一组长及换长后旧组长方可移出。
- 默认组覆盖：`TestGroupingDefaultOwnerGroupIsOverriddenByExplicitCode`。
- 最简入口：`TestAdminAgentGroupingMinimalEntryConfiguresCodeAndLeader`。

### 外科变异

1. 在首个显式组存在时直接返回、删除可达性裁决：
   - 命令：`go test ./internal/m8 -run '^TestAgentGroupingReachabilityAndIndistinguishableMiss$' -count=1 -v`
   - rc=1；转红集合仅 `TestAgentGroupingReachabilityAndIndistinguishableMiss`，红点为组外路径意外返回 nil。
2. 把 `ownerSenderExempt` 退化成“同通道一律豁免”：
   - 命令：`go test ./internal/m8 -run '^TestGroupingOwnerExemptionUsesSenderIdentityNotChannel$' -count=1 -v`
   - rc=1；转红集合仅 `TestGroupingOwnerExemptionUsesSenderIdentityNotChannel`，红点为非 owner 飞书身份意外获准。

两发变异均已还原；最终提交不含变异。

## 最终回归

- 修复测试竞态后 `go test ./... -count=1`：rc=0；其中 `internal/m8` 44.578s。
- `cd view-service && python3 -m unittest test_view_page_store.py`：12/12，rc=0。
- `go test ./internal/store ./internal/admin ./internal/m8 -count=1`：三包均 rc=0。

### 独立复跑暴露的测试时序竞态及修复

组长整包复跑曾在 `TestAgentGroupingSupportsMultipleGroupsAndOneLeader` 得到 `session ... offline`，而定向复跑连续通过。根因属于测试前提而非产品掉线：`HandleSessionWS` 有意先写 onboarding 帧，再把 client 发布进 `sessionClients`；测试辅助函数 `dialSession` 读到 onboarding 即返回，因此它不是“已上线”屏障。低负载时后续 SQLite 配置操作恰好掩盖窗口，整包负载下最后建立的 multi 会话暴露。

夹具现对四个端点逐一调用仓内既有 `waitSessionOnline`，按真实条件轮询，不增加固定 sleep。修复后：

- `go test ./internal/m8 -run '^TestAgentGroupingSupportsMultipleGroupsAndOneLeader$' -count=50`：rc=0；
- `go test ./internal/m8 -count=1`：rc=0（40.861s）。

## 明确没有跑 / 没有证明

- 没有部署生产或预发，没有改 nginx/systemd，也没有触碰现网数据库。
- 没有真人在生产 console / 飞书完成端到端操作；仓内证据使用 SQLite、httptest 与真实 WebSocket 测试连接。
- 没有正式左侧 Agent 列表编组 UI；只有本版裁定允许的 `/admin/agent-groups` 最简入口。
- 没有提供跨组系统投递接口；按 Q3 只允许组长用既有方式人工显式转发。
- G2 证明的是响应字节相等；没有做统计学时延侧信道测试，不宣称两条路径在微秒级不可区分。
- 没有做多进程/高并发更新同一组长的压力测试；SQLite 单写者与主键/单行组模型保证仓内结构约束。
- 没有修改版本号；最终装配时由组长统一把发版锚抬到 `0.2.1.0`。
- 本轮只修复测试夹具的上线前提，未修改任何产品行为。
