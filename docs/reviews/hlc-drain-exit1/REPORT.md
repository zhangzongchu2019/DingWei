# HLC drain：对端先断的停机错误分类

资源隔离：仅分支 `opus50tc2k/hlc-drain-exit1`，工作树 `/data/ai-dev-data/hlc-drain-exit1`，基线 `bdac3593c8a578eb5002b8df9418dc93b2cb93c9`。专用 runner 以白名单环境运行，HOME、TMPDIR、SQLite、PG、通知 sock 均为当轮专有；不用在线 helper，不继承 SH_*／DINGWEI_* 或外部 PG DSN。端口由内核选择 loopback 空闲端口，`RESOURCES.txt` 在启动 Hub 前记录 Hub 双端口、PG 端口和目录，每个 View Service 起动前追加其端口与目录。运行前确认无另一路 S011／清理停机；不杀他方进程。逐轮读数与首轮红项均见下文；仅交本缺陷片段，不代表整单HLC验收。

## 修复契约

组长裁定本单修产品：客户端可在 SIGTERM 前任意时刻断开。`Hub.CloseContext` 的 WS Close 若匹配 `io.EOF`、`net.ErrClosed`、`syscall.EPIPE`、`syscall.ECONNRESET` 或 `websocket.CloseError`，记 `already_closed`；超时或取消优先保持失败。拒绝字符串判断。其余错误保留 `%w` 包装并返回，仍造成退 1；阶段截止仍按原契约退 2。

停机快照新增 `hub_ws_close`，独立于存活工作归零计数，提供 `already_closed`、`failed` 和逐条 kind/result/error；保留原错误文本，不以统一错误遮蔽。快照加锁并复制切片，不把历史记录误当存活处理器。

## 回归与反向控制

- S014：20个真实 helper WS，先 CloseNow 客户端再给真实 Hub 子进程 SIGTERM，期望退0、already_closed=20、逐条原错非空。overlay 只固定跟踪集合，模拟注销未完成的竞争一侧；不替换 CloseContext、WS Close、协调器或错误分类。此格是确定性竞争覆盖，不宣称自然发生率。
- S014-hub-first：保持客户端存活先停 Hub，期望退0、already_closed=0；作为对照。
- m8 独立子进程三连接格：真实 SIGTERM 后调用实际 CloseContext；对端先断退0、Hub先停退0；存活客户端读取期间只在测试 net.Conn 注入写超时，真实 Close 必须失败、进程退1、保留三条原错。
- 撤去 already_closed 分类的 overlay：对端先断格必须在退出0断言红，实际退1。恢复后重新验证同格。

## S006 42/8 的原始证据限制

原执行流 `docs/reviews/hlc-process-random50/S006-first.log` 与 `PRE-SIGNAL-FAILURES.json` 记42通过、8失败。8次失败均在 SSE 起步的 Fatal，代码尚未到 hlcStop，清理由 Process.Kill 执行，并无 SIGTERM 可对比；原 logger 没有409绝对时间。因此409时刻、SIGTERM时刻及差值应记不可用，不能虚构时间。此处不改逐席ready屏障、不据此判夹具问题；等待主流r47同夹具起步对照与组长判定。

## 逐格读数（race，单路）

| 原件目录 | 次数 | 实际进程退出 | already_closed / failed | 时间 |
| --- | ---: | --- | --- | --- |
| s014-fixed | 3/3 | 0 | 每轮20 / 0 | 1212～1220ms |
| hub-first | 3/3 | 0 | 每轮0 / 0 | 1217～1218ms |
| live-failure | 3/3 | 1 | 每轮0 / 20 | 217～223ms |

live-failure完整Hub格在测试listener层对实际WS关闭帧的Write注入os.ErrDeadlineExceeded，客户端事前保持真实读取；CloseContext、协调器及实际main退出未替换。关闭失败后底层连接由库回收，处理器实际归零，drain记录operation_failed，进程退1。不是把阶段超时退2冒充关闭失败退1。

`final-checks/go-race.json`记录相关m8 Hub.Close、分类/子进程及workpulse Shutdown/队列/飞书接线race回归rc0；`final-checks/withdrawn/receipt.json`记录最终源码撤分类3/3目标红（各子进程退1，父测试红在S014_EXIT got1 want0）。`final-checks/spec.json`记录OpenSpec 1.16.0 strict rc0。SUMMARY.json逐轮列信号UTC、实际退出、耗时及快照计数。最终短目录完整仓库门rc0，见gate-short/receipt.json；不据此申请整单HLC验收。

## 保留的首轮失败与修正

- unit/first.log：组合错误中先出现非超时net.ErrClosed使errors.As漏掉后续超时，分类边界红。修为遍历错误链各分支查超时；unit-fixed及final-checks/go-race随后通过。
- s014首轮真实Hub三次均退1，19条closed+1条broken pipe。实际类型为syscall.EPIPE；补EPIPE与ECONNRESET后s014-fixed三次均退0，实际也记录了connection reset by peer原错。未按字符串放行，写超时仍红。
- 第一条裸openspec命令因PATH无CLI返回127，随后显式使用已钉1.16.0的node CLI strict通过；工具路径问题不充作业务反证。
- 历史s014、s014-fixed、hub-first receipt的overlays文案沿用了旧runner常量，未提到S014的跟踪集合固定；原件保留不改。实际脚本SHA及本报告均明确overlay边界，最终runner已修正文案。S014/hub-first通过后仅在同一夹具追加隔离的live-failure分支和资源/receipt文案，不改已通过格的次序、断言、产品字节。

## S006 补证只读核对

主流后续原件位于其工作树docs/reviews/hlc-readiness-comparison/REPORT.md与hlc-readiness-r47-control、hlc-readiness-current-control的readiness.json/receipt.json。已逐条读取JSON核100轮22次及30轮9次409、全部sigterm_sent=false，两个receipt同fixture SHA256 fbf300503ed60479c1cad4afefcd5bc35a041bbcb5e1bd2f756ad29327886eb7。新对照有逐轮409绝对UTC；SIGTERM未发送，时差不适用，不替代原8轮缺失的UTC。不复制或改写主流证据，不改ready屏障，由主流向组长交其Git证据锚和完整混合回归。

## 首次仓库门红与环境复核

`final-checks/build-tests.json`：2026-11-02T02:24:42.068717Z～02:34:23.052799Z，rc1；helper Claude/Codex各1097项均rc0、Python检查通过，最后desktop跨运行时shell中的长名子格失败，包含设备认领cancel_then_new/lost_ack及enrolled Desktop MCP。未改或删这些断言，原门红保留，不将其报绿。

这些Go测试会剥掉父DINGWEI变量，真实helper socket取其临时HOME/mcp.sock。首轮较长TMPDIR导致部分Unix socket路径超长；`gate-environment/socket-length.json`记录同形路径113字节实绑报AF_UNIX path too long，短根95字节实绑成功。仅换短独立TMPDIR后，`gate-environment/short-tmp-shell.json`原shell完整复核rc0（27.136s）；没有修改产品或测试文件。随后在短独立HOME/TMPDIR/IPC完整重跑仓库门，原件单列gate-short，最终完整仓库门rc0，helper双profile各1097项、Python27／shell2完成。

本轮Go以普通dev UID1001运行。无部署、重启在线helper/Hub、迁移、apply/archive、合入或改原树；只在指定独立分支准备可复核片段。

## 独立复跑入口

普通用户在独立工作树、确认另一执行流无S011/清理停机/仓库门后依次运行：

```sh
python3 tests/hlc/process_matrix.py --case S014 --repeat 3 --race --output /tmp/hlc-review-peer
python3 tests/hlc/process_matrix.py --case S014-hub-first --repeat 3 --race --output /tmp/hlc-review-hub
python3 tests/hlc/process_matrix.py --case S014-live-failure --repeat 3 --race --output /tmp/hlc-review-live-failure
python3 tests/hlc/ws_close_mutation.py --output /tmp/hlc-review-withdrawn
```

前三项runner rc0表示各自预期实际退出码已核（0、0、1），不是全部子进程退0。变异runner rc0表示三次父断言均红且实际子进程退1，详见receipt的target_red；不是变异产品通过。

相关Go及仓库门的精确命令见final-checks/go-race.json和gate-short/receipt.json。仓库门须白名单环境、专有短TMPDIR/HOME/IPC，不沿用在线helper环境，也不把较长临时根传给这些长名Unix socket集成用例；资源先记gate-short/resources.json。源码只经工蜂Git分支交接，不上传源码包或补丁。

## 最终交接状态

短目录完整门：2026-11-02T02:37:33.771472+00:00～2026-11-02T02:46:16.764989+00:00，rc0，末行BUILD_TESTS_OK helper_profiles=2 python=27 shell=2。仅因已实证的测试临时socket路径长度更换环境后完整重跑，首轮rc1保留，不覆盖、不洗绿。相关Go race／strict／实际进程三格各3轮与撤分类3轮均完成；源码产品SHA与SUMMARY.json一致。交独立分支供唯一写者fetch核证及合入，不修改原树或自合入；S006原50轮仍为42/8，不改为通过，逐席ready屏障由主流按对照证据收口。
