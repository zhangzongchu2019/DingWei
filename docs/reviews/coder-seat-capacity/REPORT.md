# B：soft 席位容量与最小套餐 — 作者终交

仓库 `git@git.code.tencent.com:wego-ai/dingwei.git`，分支 `review/coder/seat-capacity`。
基线 r22 `840741456534edb3b4f6457847aa670bba93d2d2`；冻结代码 `26f506c3ef47014db59b339133943c98585a8c54`，tree `df4bbe79be326b9e2b819193502bcd548be6ca89`。
最小套餐按要求独立提交 `1ed944134e11dae3c51e7727799e3882ca47983b`。本目录提交只追加证据。审查方 opus50ts；本文是作者结果，不是独立审查 PASS。

## 采用范围与资源口径

按主审最终确认：B 只在**显式 soft** 节点发布 `seat_capacity.version=1` 并强制准入；hard 不发布 B，沿用既有准入，默认 hard/既有 MaxSeats=4 不变。hard 独立预留台账另立项。缺失能力的 soft 不降级，Console 显示未知并拒绝新建/新启动。

nodeagent 是系统服务，其消耗属于节点预留，不另算管理席。B 不改变运行席，不因容量收缩自动 hold/停席；原 soft 用量、字节/inode 预留、紧急线与 hold 保留。hold 解除/通知/prodops 监控是 B 后的 S-SOFT-HOLD-RELEASE，不在本件。

三维分别计算：可分配净量=max(0,总量−节点预留−非受管占用)；每业务席预留=max(套餐资源,配置最小资源)；剩余量再扣受管预留。总席数/可新增席数取 CPU、内存、磁盘与 MaxSeats 的最小值。**净** 8CPU/16GiB/320GiB 接受 8 个 1CPU/1.5GiB/40GiB，第 9 个拒绝；净值不再重复扣节点预留。此例不是原始 8CPU 主机在默认预留下可放 8 席的承诺。

旧 2c4g、4c8g 实际磁盘仍 8/16GiB；B 预留至少 40GiB，不擅自扩大用量。新 `1c1.5g40` 实际给 1000m CPU、1536MiB 内存、40960MiB 磁盘。既有套餐不改，默认用户 64GiB 磁盘额度不增：新套餐一次扣 40GiB、剩 24GiB，额度不足照常拒绝。

## 实现入口

- `internal/seatcapacity/capacity.go`：纯计算、边界、wire 算术、版本/时间检查、CanAdd/CanStart、pending 补记；无 IO。
- `internal/nodeagent/policy.go`：最小资源参数、校验、revision；显式 soft 未设 MaxSeats 时上限 100，实际仍取三维净量；显式较小上限有效。
- `internal/nodeagent/seatcapacity*.go`：有界采样、缓存、live 预留。`seat.go` 生产 soft 注册强制安装 B；`softquota.go` reserve/start/最终 writer 与 commandGate 接入，失败为 `seat_capacity_blocked`，不伪报 soft hold。
- `capacity.go`、`softquota.go:reportWithCapacity`：同一短锁快照 soft_seats 与 B，避免数量/占用不一致；上报不含 WriterRoot/CID。
- `internal/hosted/seat_capacity.go`：soft 缺能力/坏算术/unknown/过期拒绝；hard 返回旧行为。fresh heartbeat 不能替代 B 时间戳。
- `store.go:CreateSeat`：节点行 FOR UPDATE 与 queued/pulling/starting/busy 未上报预留同事务检查，避免并发 9 个都通过；已上报 ID 不重记。查询/枚举有界，超界拒绝。
- `orchestrator.go`、`node_quota.go`：实际创建及 start/restart；创建前拒绝不遗留身份令牌。满节点既有席复用自身预留，unknown 或净资源不足仍拒绝。
- `readapi.go`、`internal/m8/console_auth.go`：能力版本、总/占用/剩余、三维可用量及原因；hard 不显示 B 承诺。soft 风险标识/同意/原权限不弱化。
- `plans.go`、`schema.go`、`readapi.go`、`cmd/workpulse/hosted_routes.go`：套餐、幂等升级旧 CHECK、1.5GiB 选项、实际 Create 资源、额度扣减。禁用席仍占额度，销毁后按原语义释放。

## 配置与采样

默认值保守，最终值由业主定；本件不改主机配置：

| 配置 | 默认 | 作用 |
|---|---:|---|
| NODEAGENT_MINIMUM_SEAT_CPU_MILLICORES | 1000 | 每业务席 CPU 下限 |
| NODEAGENT_MINIMUM_SEAT_MEMORY_BYTES | 1610612736 | 1.5GiB 下限 |
| NODEAGENT_MINIMUM_SEAT_DISK_BYTES | 42949672960 | 40GiB 下限 |
| NODEAGENT_MAX_SEATS | soft=100；hard=4 | 不绕过三维容量 |
| NODEAGENT_RESERVE_CPU_MILLICORES | 2000 | 节点固定预留 |
| NODEAGENT_RESERVE_MEMORY_BYTES | 4294967296 | 节点固定预留 |
| NODEAGENT_RESERVE_BYTES / RESERVE_PERCENT | 32GiB / 10 | max(固定,实际文件系统比例) |
| NODEAGENT_IMAGE_RESERVE_BYTES | 8GiB | 镜像余量 |
| NODEAGENT_RESERVE_INTERVAL | 5s | B 采样间隔 |
| NODEAGENT_SCAN_BUDGET | 5s | B 单次预算 |
| NODEAGENT_MAX_AGE | 60s | B 再取 min(此值,60s) |

最小 CPU 范围 1..1000000m；内存 4MiB..1PiB；磁盘 1MiB..1PiB；MaxSeats 1..100。均进入 NodePolicy/revision。最终集成含 r23 inode 配置，**须从最终集成代码/实际配置计算 revision 并核注册绑定**，不能复用 B 单线或旧包 revision。

固定第四 worker 与原 scan/reserve/enforce 分离，不按 tick 重生 goroutine。statfs/proc/Engine stats/inode walk 均锁外；缓存、原子发布和预留才用短锁。预算/时间失效使准入及上报 unknown；不可中断内核 IO 返回前最多占一条 worker，不累积替代，也不承诺该 IO 退出时限。

CPU 读 `/proc/stat` 差分，排除 guest 重算，iowait 不算 busy；MemTotal/MemAvailable 严格解析，缺项不是零。host busy/used 包含旧 Engine/非受管进程。仅对已核 node/seat 或 login 标签及挂载的受管 CID 读带时间戳的一次性 stats，扣 owned CPU/匿名 RSS；坏返回拒绝。首个 host 窗口 warming；首个 managed CPU 窗口不扣 owned CPU，采用保守 host-busy 上界。后续是采样估计，**不是未来负载或瞬时隔离保证**；失败后清差分历史，避免恢复误扣旧区间。

磁盘按实际 seatRoot 文件系统总量/Bavail；受管路径只做 no-follow 元数据遍历，不读内容；物理 allocated inode 并集用于扣非受管占用，跨席硬链接只扣一次，稀疏文件按实际分配块。全目录合计最多一百万 inode 且受预算约束，超限 unknown。原 per-seat 双口径执行不变。新 Engine/data-root 与席位根同在 /data2 的前置仍需 prodops 实测。

持久业务预留包含停止/禁用/hold 的未释放席，重启不清零。r23 N2 临时 login 的实际 CPU/内存/磁盘预算加入节点预留，**不算业务席、不套 40GiB**；tmpfs 内存不作为 /data2 已分配磁盘扣除。读取 /h 使用 N2 dingwei.login 标签/private mount，错误标签/挂载拒绝。B 保持 r22 基线，不导入 N2 运行流程，与 r23 实际 login 链通过本地组成夹具验证。

## 本地结果与归属

命令/rc/时间见 RESULTS.json 及 raw 同名 result.json。均作者本地结果，无真实 Engine/宿主/外部 PG；已锁定 Python 依赖，未安装或构建镜像。

| 读数 | 精确归属 |
|---|---|
| 完整 canonical | 26f506c：rc0，102.440s，python=48 shell=2；临时 PG parent+new/reuse/lost_ack/takeover/cancel_then_new 五格 PASS |
| nodeagent/计算/版本 race | 26f506c：149/6/4 顶层，无 skip，整体 rc0，11.472s |
| hosted 全包 race | 8c6a315：155 顶层 rc0，401.464s；hosted 产品字节与最终一致；随后 test-only stale_wire 在最终源码另跑父测试 rc0，7.047s，不冒称最终源码单轮全包 |
| workpulse 定向 race | 26f506c：5 顶层 rc0，22.989s；真实 admin CLI→normalize、套餐 HTTP/额度等；不是 workpulse 全包 |
| Console 浏览器 | 21 断言 rc0；撤修后最终源码再跑21断言 rc0（2.752s）；非真实用户/镜像登录验收 |
| 撤修/恢复 | 18 项 source withdrawal 均 rc1 且断言红，无编译失败代替；候选源码未改，恢复定向 suite rc0、浏览器绿 |
| 原测试保留 | 13 个原 nodeagent test 文件逐字不变；softquota_test 增加注册/unknown 断言并注入已知容量后继续原路径；hard 测试不删不跳过；hosted 原用例补 B frame，原权限/审计/副作用断言保留 |
| docs 包装 | overlay 为 .go.fixture；go list ./... rc0，无 docs Go 包 |

18 刀：生产注册、reserve、start、阻塞采样预算、Hub 事务 pending、Hub fresh、外部 CPU/内存/磁盘各一、最小 CPU/内存/磁盘各一、失败差分恢复、Console、新套餐、1.5GiB 显示、旧 CHECK 迁移、N2 login 标签。raw/capacity-mutations.json 钉每刀源码/替换/哈希/命令。注册补刀最后追加，原 17 刀执行脚本未冒称含补刀。

保留开发失败：首次 hosted 全包因旧 quota error JSON 小于 1GiB 显示红，最终恢复 whole-GiB error 契约，实际 MiB 扣减和 options/admin 1.5GiB 不变；早期 CPU 错误夹具只命中 warming，补 warm+目标原因；首轮 freshness 撤修曾绿，增加 known=true stale_wire under fresh heartbeat 后才红。原始日志/metadata 保留；旧套餐阶段报告只对旧源码有效，fractional-error 描述不是最终行为。

## r23 本地组成，不是集成线

r23 `fa07ddce2ab239307ccaf5d5ab14fc957dcf4b11` 与 B merge-tree 仅 policy.go 两段冲突（struct、parse map）；明确取并集，保留 inode 默认50/校验/env 及 B minima。raw/capacity-r23-composition.json 记录逐段取法及 resolved policy SHA；没有修改 r23，没有虚构采用 tree。

组合发现 N2 实际 login 标签不同，旧 B 读法会拒绝合法 login；26f506c 修正并加直接负格。组成夹具走实际 l.start（伪 Engine/loopback HTTP），核 login 预算、不算业务席、无40GiB floor、unknown 不启动、原 N2 hold；2 个额外夹具及全部组成 nodeagent **163 顶层 race rc0**，11.933s，无 skip。此结果只属记录的本地解冲突组成，不替代主审合并审查，不是实际 Docker 验收。

`prepare_r23_composition.py --repo <checkout> --lab <empty-private-dir>` 是固定两端源码的可移植伴随脚本；executed.txt 保留当时执行原件，绝对路径仅来源。overlay 保持非 Go 后缀，只在私有组成目录转 test.go。最早工具假设 conflict marker 名称而编译红已保留 initial 日志，不冒称候选产品缺陷。

## 交付与剩余门

待 opus50ts 审精确终交，再由主审合到 r23。本分支不重基。合并核 policy.go 并集、重算 revision、保留 A1/N2，再跑所需合并门。

无 helper/VERSION/锁/依赖改动，SOURCE.json 钉未改成员。真实 cgroup/Engine stats 版本、采样开销、文件规模、隔离 Engine、宿主 root 信任、网络/CA、维护/凭据/登录/镜像等仍需授权同环境验证。CPU 外部负载为采样估计，soft 磁盘采样期间超用/耗尽不能由 B 消除。没有发布/迁移/试点 GO，四席冻结不变。

收尾：自有夹具进程/PG pid marker 为 0；移除本席 mutant/组成目录、私有 HOME/TMPDIR/cache 共 1777293952 bytes。Go cache 先 clean 到 232 bytes，再删除；源工作树留待返修，共享 module cache/锁定 venv 不动。原始路径/字节见 raw/cleanup.json。
