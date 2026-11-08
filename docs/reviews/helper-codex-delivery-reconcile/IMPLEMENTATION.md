# S-HELPER-CODEX-DELIVERY-RECONCILE 第二步实现交件

实现提交c0123016，精度追加843a75ce，乱序持久合并追加aae6222d，完整门兼容修正35515ea1，库存计数同步ec7f259a；基于第一步5ed477a5153b066f221dd2b1ae2af85d2b7ab1f5同分支追加，不改写已有提交。分支coder56solzzc/helper-codex-delivery-reconcile；仓库git@git.code.tencent.com:wego-ai/dingwei.git。唯一写者实取CODEX_THREAD_ID=01a1bacf-9317-7b91-bf86-0a0c88c834f6，原子锁/tmp/coder56-helper-reconcile-impl.lock。

冻结来源为rulings〔51〕-补3 fe4ad167990bb5bfd7593cf6145462f93de661d8；提交前已fetch/read后续0779177153808c57ce7b94ca812d2e84b34f8d9d，仅追加r54准备/预发裁定，没有改变本单。最终推送锚、树及ls-remote核对在交件回执给出。

## 改动与边界

- Codex真实stdio/WebSocket写锁内开始写出turn/start时记录提交时钟；原生忙等待和等写锁保持本地processing，不消耗入场期限。健康忙回合不受旧stall窗口截断。健康传输仅等待响应不替换；真实断连或写出卡死才恢复。
- 既有deliveries账本承担pending集合、历史dispatch thread、最近查询时刻和原生证据。detail只追加精简reconcile JSON；无schema迁移、无新配置键。没有driver行不赋予unknown重投权；仅尚未入场的本地processing可继续。
- 原生只读对账串行（同席对象锁与flock），同thread至少15秒，跨全部可信dispatch thread/opaque分页共200条预算；触顶incomplete，不下未执行结论，不start/resume，不改变当前绑定。完整扫描后唯一精确clientId、原thread/turn、有限原生时间与原ACK代次快照齐备才合并；其他情况pending。
- 完整证据先CAS落既有driver账本，再发事件给inbox原事务，验证原ACK代次、dispatch intent、request ID；进程重启可不依赖旧对象/通知恢复。完整native秒时间可升级原代次中此前记录的浮点接收观察；已processed的时刻和outcome不变。
- 原生秒与浮点intent容差由NATIVE_TIME_TOLERANCE_SECONDS=1.0固定。拒绝nullable、NaN/Inf、bool、负数、倒序和超出容差的旧时刻，冲突turn/outcome不替换。inProgress只补admitted；普通done receipt不再凭当前时刻补Codex processed。
- 输出补取复用_take_completed_item_backfill，保留原能力协商和稳定item替换，不以显示完成推进ACK。Hub、View、VERSION、生产数据均未改；未操作在线helper/daemon、ts3dev55数据或部署，不开始子Agent设计。

## 原生完整原文

入口tests/helper-codex-delivery-reconcile/real_protocol.py，二进制/tmp/coder-stale-protocol-01592/codex，SHA256 1748767b230ebfc3d4ab7e4e254920d0c0ad9691fd8c11f190e7d44511a4a92e，钉版0.159.2。每格私有HOME/CODEX_HOME、清空继承环境、loopback不可达推理端点，不带凭据；只创建并终止自己的stdio子进程，用自己的turn/interrupt取得interrupted终态。

最终原文[raw/impl/native.log](raw/impl/native.log)，逐格完整原生请求/响应和本地账本[raw/impl/native/](raw/impl/native/)，汇总[RESULT.json](raw/impl/native/RESULT.json)。共8/8 PASS、FAIL0/ERROR0/SKIP0；原先7格全部保持原用例名，新增第8格专验真实nullable turn/start响应后浮点接收观察与原生秒终态恢复。

| 首步目标 | 第二步结果 |
| --- | --- |
| A history原生时间丢失 | PASS，保留原startedAt/completedAt并落ACK |
| A driver替换后终态缺失 | PASS，持久账本补终态 |
| B 当前绑定clientId unknown | PASS，只读恢复，无turn/start重投 |
| B 历史绑定clientId unknown | PASS，恢复旧thread且不改当前绑定 |
| 健康等忙触发替换 | PASS，turn/start=0、replacement=0、inbox processing |
| 无匹配/缺时间输出/完整原生证据3控制格 | 全部PASS |

等忙入口tests/helper-codex-delivery-reconcile/timeout_feedback.py，原文[timeout.log](raw/impl/timeout.log)，1/1 PASS。测试从同步等待超时返回改为在独立线程观察旧期限之后仍处本地processing，再释放屏障并join自己的worker；没有expectedFailure或屏蔽目标断言。

## 反测与逐刀变异

常规库存新增tools/sessionhelper/tests/test_codex_delivery_reconcile.py，24/24 PASS，见[boundaries.log](raw/impl/boundaries.log)。库存登记scripts/helper-test-files.txt；现有dispatch组33/33 PASS，见[dispatch.log](raw/impl/dispatch.log)。旧dispatch夹具曾用100/123/124原生时刻配真实当前intent，在新时间门下应拒绝；现已对齐对应合成intent99/122，其他队列夹具取真实intent，并补真实native terminal事件，不靠receipt done伪造完成。

变异入口tests/helper-codex-delivery-reconcile/mutations.py，只在一次性本地目录复制Git跟踪Python文件，检查排除非.py文件，不打包env/凭据/私有状态，不上传。21刀全部rc1且目标断言FAIL、ERROR0，见[变异RESULT](raw/impl/mutations/RESULT.json)与同目录完整日志。

| 判据 | 单刀日志 |
| --- | --- |
| 重复clientId/历史重复 | duplicate / historical-binding |
| 200条边界/跨页唯一性 | budget / pagination |
| nullable与非有限时间 | nullable-time |
| 错thread/旧ACK代次 | thread / generation |
| 冲突终态 | terminal-conflict |
| 进程重启持久恢复 | restart |
| 通知先于响应 | reordered |
| 1秒容差/超容差旧时间 | second-tolerance / old-time |
| 替换不重置15秒/跨进程单请求 | rate / seat-concurrency |
| 健康长回合/实际写锁时钟/本地不计时 | busy-stall / transport-clock / local-clock |
| 空driver账本unknown不重投 | unknown-no-ledger |
| 健康响应等待不替换 | healthy-response |
| 原生秒终态与浮点观察 | fractional-observation |
| 晚响应保留inProgress证明与查询时刻 | late-response-metadata |

## 规范门与过程记录

最终完整规范门在35515ea1产品字节（测试库存同步ec7f259a）上rc0：2026-11-08T11:41:44.860429Z～11:50:34.456585Z，三闸、双profile各1121/1121、27 Python组及2 shell组全过，末行BUILD_TESTS_OK。原文[canonical.log.gz](raw/impl/canonical.log.gz)无损压缩，原始SHA256 5121f264ba4af6368234ef5a157fe977ebeb9d6b5a0161733863d120b580d7db，命令/时间/源文件指纹见[canonical.json](raw/impl/canonical.json)；本变更OpenSpec strict rc0。首个完整轮aae6222d的两profile各1121项同3红、rc1，原文canonical-first-complete-rc1.log.gz/JSON保留：新计时对轻量__new__ adapter缺省_which不兼容导致blocker两红，现已恢复缺省兼容；旧reconnect夹具在已断线条件下仍要求turn/start，现保留锁不交叉断言，并改为要求queued和turn/start=0。独立blocker16/16、stale23/23均绿，日志见raw/impl/blocker.log、stale.log。完整门第二轮双profile各1121/1121绿，随后显式库存数90未同步实际91导致总rc1；该计数已同步，逐文件缺失负测保持，独立build-entrypoints17/17绿。原文canonical-second-complete-rc1.log.gz/JSON保留。另有实际201条源的分页验证1/1绿：只读200条、1请求、incomplete且ACK不变，见pagination-201.log。最终前开发试跑原文保留为raw/impl/development-*.log.gz，包含初版原生旧binding样本FAIL1、旧合成时间导致dispatch红及已终止的中间全量试跑，不把它们记成最终绿。初版历史失败来自把未materialized且从未dispatch的全新binding纳入扫描；现范围按先journal后turn/start的真实dispatch thread，不把其不可读当成历史证据的阴性结论。

最终完整规范门、原生组及反测只证明隔离环境实现自测，不代替别席验收；未实跑自然completed/failed推理、在线daemon故障重启或对方161条批量修复。它们仍由后续独立复核或专门运维授权处理。

补充过程证据：独立诊断剩余测试时把一个Bash脚本用sh调用造成解释器错误，按标准入口bash重跑rc0；该诊断不作为总门PASS，原文保留development-remainder-diagnostic与development-view-bash-fixed。最终标准脚本已自行完整执行其bash入口并rc0。

最终漂移核验（2026-11-08T11:53:06.359838+00:00）：工蜂本分支仍5ed477a5153b066f221dd2b1ae2af85d2b7ab1f5；rulings最新5ec4905447446982159a7965601e6c181e53c54f，已读相对fe4ad167的追加，仅〔52〕r54运维准备/预发裁定，不改变本单〔51〕-补3。无并发源分支改动；不合入其他派工。

原始nullable-time变异日志含unittest原生尾空格，已无损压缩为raw/impl/mutations/nullable-time.log.gz；解压字节SHA与RESULT.json的原始log_sha256一致，不修剪原文。其余逐刀原文保留.log。
