# S-SOFT-HOLD-RELEASE — 作者交件与本地测试

交件范围：业主专用 hold 解除及审计、业主/prodops 双通知、prodops 只读监控。仅开发和本地验证，待 opus50ts 独立审查；不是部署/宿主/真实账号或 Agent 操作授权。

## 精确源码

仓库 `git@git.code.tencent.com:wego-ai/dingwei.git`，分支 `review/coder/hold-release`。
基线 r24 `f209edc0f0ab664541c2340bc7644b90c661d069` / tree `f7df1a3687ad36380228d197c4eae3dafca54ba6`。
源码冻结 `1a9110c2f54e2864c9567991df235ca6d1c9826b` / tree `e2702b95012cdbd6b7b9013552ef1ae19c236054`；首实现 `35bf1ba3`，后继仅修正测量失败/用量阈值通知文案并增加对应负格。
最终交付 tip 是后附证据提交，除本目录外应与此源码钉完全一致。`SOURCE.json` 记录全部源码 blob/SHA 与未变子树；`MANIFEST.json` 的键均为仓库相对路径。

## 最终读数（作者运行，不冒称独立审查）

* 完整 `./build.sh --test-only`：rc0，python48＋shell2，87.88s；临时 PG 父格与五个 claim 子格确实执行，无 PG 排除 wrapper。
* 串行 `go test -p 1 -race -json -count=1 -timeout=45m` 五包：rc0，无失败/skip。上限45m只为容纳原有慢生命周期测试，没有缩小选择器。

| 包 | 顶层通过 / 子格通过 | 用时 | 失败 / skip |
|---|---:|---:|---:|
| `nodeagent` | 166 / 91 | 10.315s | 0 / 0 |
| `hosted` | 167 / 147 | 392.385s | 0 / 0 |
| `workpulse` | 118 / 35 | 198.298s | 0 / 0 |
| `nodegate` | 5 / 0 | 10.451s | 0 / 0 |
| `seatcapacity` | 6 / 3 | 1.017s | 0 / 0 |

* 当前源码 m8 通知定向 race rc0；补充三个隔离 probe rc0：停席执行中立即拒绝解除、仅内存成功不能算通知落盘、合法但未配置的 Agent 不能监控。
* 现有 Playwright/Chrome 本地页面13检查通过：owner 确认/单次提交，prodops/过期读数无解除按钮，网络失败不重发，HTML 注入不执行。全部 URL 由 fixture 截获；不是真实 Console/飞书/Agent 验收。
* 完整 helper 测试未重跑；helper/runtime lock/VERSION/renderer/NodePolicy/B calculator 与 r24 的源码对象未变，见 `SOURCE.json`。helper 包不变，不另抬版本。

## 解除的实际合同

`DW_G4_HOLD_OWNER_USER_ID` 显式绑定真实业主 UID；N3 有效人类会话、active platform_admin 和同 Origin 缺一不可。其他管理员/manager/member/匿名与所有 Agent bearer（包括同属主 prodops）在解除入口403；真实 N3、MemStore 身份与本地 PG 路由测试覆盖，不是请求体自称 owner。接口不接受 actor/path 等额外字段。

页 `/console/hosted-quota`；读面 `GET /api/console/hosted/quota-monitor`；写面 `POST /api/console/hosted/nodes/{nodeID}/holds/{seatID}/release`。prodops 仅用精确配置且 active 的 enrolled Agent bearer 读面，不能取得解除权限。普通 Console owner 角色与 manager 角色都不等于本功能业主。

节点 `release_soft_hold` 只接受当前持久 hold_id。StopState 必须 stopped；既有 admit/fresh/unknown/预留与 recovery-percent 门保留，retiring writer 拒绝。enforcement TryLock 不等待 Engine；持久化失败保持 fail-closed。新 hazard 有新 ID，旧批准和 ledger replay 不清新 hold。全过程不删除资料、不改阈值、不启动容器或恢复凭据。

写前记 owner 意图，写后记受限回执/拒绝审计；网络结果未知不重发。后续受信节点的同一个 last_release_id 可结合先前审计意图确认结果，仍保持 stopped；单独显式 restart 才能继续，并重过 soft/B 门、等待新连接。无 owner 意图的节点读数不能清 Hub held 细节。

## 通知与监控

现有受信 allowlist 事件与审计、两个 `g4_quota_notice` 行在同一事务提交，之后 nodegate 才能 ACK。每个 node/event/recipient 唯一，收件人独立回执。worker 有上下文、15s周期、32条/10s批预算、5s单投递预算和30s失败延后；无无限 tick goroutine。

owner 投向本人实际 Feishu issuer/subject 的**同步持久 Hub 队列**；不把 Async Outbound 的内存入队当持久完成。prodops 投向配置的实际 ACK-capable Agent 的持久 session_delivery，固定 delivery ID，system_notification/no_reply。重试相同队列 ID 幂等，某一目的地失败不吞掉另一目的地。

usage_warning 是“测量暂不可用”，disk_warning 才是已到/超过用量告警线，hold 文案区分停止/正在停止，不把失败停席写成成功。正文只有固定说明、安全 ID 和 UTC 时刻，不含 Engine 原文、私有路径、CIDs、凭据或账号材料。`notices_queued` 只证明持久排队，不证明真人已读。

监控显示节点与 seat/login writer 双用量、阈值、warning/unknown、hold/停止状态、root byte/inode reserve/emergency、测量时间及通知计数。严格类型投影忽略不认识的 capacity 字段，不返回 labels、WriterRoot/CIDs 等私有内容。Hub 心跳 fresh 与 root known/测量时间分开；hard 不伪造 soft 计量。GET/写面均 no-store。配置、恢复、回滚及 prodops 监控职责详见 `docs/runbooks/soft-hold-release.md`。

## 撤修（断言不变）

每项均先固定绿、临时副本只撤源守卫/边界、断言红、恢复绿；不是改预期值造红。原函数、突变与恢复 SHA 在 controls-*.json，raw 留完整输出；编译红不算有效撤修。本次八项全部为实际断言红。

| 撤修 | 固定 / 撤修 / 恢复 |
|---|---|
| `ops-binding` | 0 → 1 → 0 |
| `persist` | 0 → 1 → 0 |
| `owner-proof` | 0 → 1 → 0 |
| `two-destinations` | 0 → 1 → 0 |
| `generation` | 0 → 1 → 0 |
| `owner` | 0 → 1 → 0 |
| `projection` | 0 → 1 → 0 |
| `durable-queue` | 0 → 1 → 0 |

额外文案负格先在35bf source上红、修正后入源码普通 Go 测试。所有 probe 源以 `.go.fixture` 留档，避免 docs 被 Go 全库发现为坏包。capture scripts 记录作者本地实验路径；复跑者应在自己的隔离 checkout、短 TMPDIR、私有 HOME/GOCACHE/白名单 env 中运行同 argv，不继承外部 PG DSN，不用作者已清理的路径。

## 首轮红记录与解释

1. 本席首轮 TMPDIR 前缀过长，既有 workpulse notify.sock 测试出现 bind invalid argument，M8 实际 Python IPC 子进程8s超时。改为独立短目录后，同源码三个 socket 格与实际跨运行时格绿，随后35bf完整 canonical48＋2绿。未改产品/断言来取得这些绿色读数。
2. 首轮多个包与 canonical PG 并发时，nodegate 的全进程 residue 采样在收尾时看到两个 transient postgres zombie。随后这两 PID 均不存在；测试的 residue 源码采样 `/proc` 全局，不按本夹具后代过滤。原红保留，不称其首次通过。最终当前源码所有 PG 门串行，nodegate 全包 rc0，cleanup 严格检查没有排除。
3. 上述红及排查控制是独立文件，不覆盖最终日志。源码35bf和当前1a分开归属；当前全门只以 final 结果文件为准。

## 限制和授权

所有测试只在本地临时 PG/SQLite、假 Gateway/Engine 与回环 Hub/浏览器 fixture；没有实际 Docker daemon、宿主、真实 key/登录态/Agent、制品发布或部署。真实 owner/prodops 绑定、Feishu 发送/阅读、目标 Engine 与文件系统、节点重启恢复验收须后续单独授权；此处只证明本地代码合同。配置改变不会自动重定向已排队通知。运行时/源布局迁移、真实 hold 材料处置、旧根信任及网络围栏等既有门不因此解除。

收尾读数见 `CLEANUP.json`；保留证据和源码，清除仅本席实验目录、短 TMPDIR、私有 cache/HOME 与 mutant worktree，绝不清共享 module cache或其他席位目录。四席冻结和 r24 不部署的边界保持。
