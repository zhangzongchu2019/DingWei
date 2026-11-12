# S-HELPER-BUSY-ADMISSION：r59 候选第二项

在已冻结继承线程交件 c0777e4b0d26cabc588f22bdf3ae7ce90d412110（tree 5c635f14，实现 f17fa090）之上追加普通提交，同分支 coder56solzzc/helper-recover-inherit-thread。需求 53812402782b，开工令 source_message_id 6cff6cb58e0842ae8c23b220e0b26731；入箱 request_id=dw-dlv_9a06e6f79a310aa29380979352cdc2d1，不冒充未保存在入箱的发送端编号。写者01a1cf05-9d3d-75e2-9233-eb9d75994424已核远端同值、fetch和干净工作树，在原任务已交且原锁不存在后重新原子mkdir记交接；未删除任何历史锁备份。未改VERSION，不构建升级包、不部署、不重启，不自验收。

## 结果与边界

健康旧回合执行时，新信仍留本地等待，不在60秒变成unknown；旧回合结束后才进入提交。Codex依据既有原生turn状态，Claude依据串行active的terminal_seen；Claude去掉180秒后冒险写下一信的路径。两驱动都只有明确的旧回合证据才能取得忙等待豁免，不从进程在线或View状态猜测。

新增 SH_BUSY_ADMISSION_MAX_SECONDS，默认1800秒；load_config和config.example均同步。adapter从首次确认忙等待起计上限，每20ms观察，健康等待期间跳过原入场超时。实际写出后仍使用独立入场期限（原默认60秒），Claude新增真实stdin写出起点；账本截止窗口按实际起点延展，避免90秒忙等待后driver虽成功、inbox却被旧dispatch意图截止拒绝。

每driver的BusyAdmission将waiting/cancelled/submitting放在同一把锁下：超限仅cancel_waiting，成功后才记录unknown、释放FIFO，后台worker以及同ID后续调用不能晚到入场；进入before_submit后的本信不能再按未提交等待者取消或重新排队。原unknown仍按既有持久意图/原生证据只读对账，不新增自动重投入口。取消标记只在同一driver生命周期内保留；跨进程仍由持久dispatch意图阻断重投。

超限固定一行告警：

```text
[delivery][ALERT][BUSY_ADMISSION_TIMEOUT] id=<delivery-id> driver=<codex|claude> limit_seconds=<上限>; no automatic replay
```

随后沿原transport_unknown事件/unknown账本路径，reason=busy_admission_timeout；不重建健康驱动。该固定告警本身不夹原消息正文或服务端异常。传输失活、真正写入卡住及已可能提交优先于忙豁免：Codex确知未写出仍defer及受控恢复，Claude保留原unknown/恢复规则。其它前置RPC超时和终态watchdog不在本单重新定义。

## 等待事件与View

既有adapter事件队列输出state_change，data.change=delivery_waiting_agent_idle，lifecycle/state=waiting，reason=agent_busy，status_text=排队等待 Agent 空闲，带原delivery_id、wait_seconds、limit_seconds；首次及之后每30秒一条，cursor以queue序号和周期区分。不发虚假injected/processed或unknown，不改Hub及ACK版本。

本地真实renderer核验发现原View会把lifecycle=waiting放进等待区，却不展示该文案。因此补最小消费：固定文本显示「排队等待 Agent 空闲」；心跳不替换canonical原输入、权限或消息关联。显示归一处理dw-前缀差异，两种到达顺序只保留一个等待项；原有已入场保护继续阻止迟到等待事件重新排队。来源图标flag开/关均验证。复用现有renderer-model-admission-regression.js并补入tools/run-browser-regressions.py的SUITES，不新增浏览器脚本。

## 测试与语义撤防

所有用例为普通uid1001，env -i、锁定venv、临时HOME/SQLite、本地合成协议或localhost renderer；90秒和1799/1801秒由虚拟时钟驱动，不是线上等待或真实推理。没有访问运行helper的协议端点、账号或生产数据库。

- 新增BusyAdmissionTest共13例，全部通过，最终定向连入口位置检查共15例通过；相关四模块151例通过（该轮在最后4个边界探针追加前执行，最终全量门覆盖全部13例）。
- 原生形状Codex RPC及Claude内存stdin，仅替换传输与时钟，实际driver串行等待门、adapter和临时inbox共同执行：90秒后仍processing且无提交，释放旧回合后admitted，再调同ID只有一次写出；1799秒仍等待，1801秒unknown＋固定告警，释放旧回合和同ID再调用仍无晚到提交。
- 两驱动传输坏仍走旧路径；另独立探针验证已写出、失活和卡住写入绝不取得忙等待豁免；原子取消和提交边界互斥；配置默认/覆盖均验证。
- 浏览器原15项加等待相关11项，共26项通过；额外真实adapter生成事件喂当前真实renderer，等待区可见、文案可见、无未知、无pageerror。
- Python绿控制12例rc0、浏览器绿控制26项rc0；最终19/19刀均红在指定判据，Python rc1/FAIL，浏览器rc2且明确FAIL项，不用环境错误充红。每刀及可复跑脚本见mutations.py、raw/mutations/mutations.json和逐刀日志。撤防只在一次性源码副本，未改受测工作树。

| 判据 | 撤防与定位 |
|---|---|
| Codex/Claude忙90秒且一次入场 | 各删驱动的明确waiting证据，BA_WAIT_NOT_OBSERVED |
| 两驱动等待上限 | 各关掉上限分支，BA_LIMIT_NOT_ENFORCED |
| 固定告警/等待事件 | 各撤文案标记或change，BA_LIMIT_WARNING_MISSING/BA_WAIT_EVENT_MISSING |
| 传输坏仍旧处理 | 撤Codex本地未提交恢复的两个触发点；撤健康检查，BA_BAD_TRANSPORT_NOT_RECOVERED/BA_DISCONNECTED_EXTENDED |
| 已可能提交/卡住写入 | 撤提交检查及两驱动写入健康检查，BA_POSSIBLE_SUBMISSION_EXTENDED/BA_STUCK_WRITER_EXTENDED |
| 取消与提交互斥 | 分别撤取消后拒提交、仅waiting可取消，BA_CANCELLED_CROSSED_BOUNDARY/BA_POSSIBLE_SUBMISSION_CANCELLED |
| 配置生效 | 硬编码默认值，BA_CONFIG_LIMIT_IGNORED |
| View文案/同项/关联/先后/迟到 | 分别撤对应消费保护，BUSY_VISIBLE_WAITING、BUSY_MERGE_PREFIX_ONCE、BUSY_CANONICAL_INPUT_PRESERVED、BUSY_BEFORE_PRIMARY_MERGED、BUSY_LATE_EVENT_NO_REQUEUE |

复跑：临时HOME/TMPDIR下，以env -i与锁定Python运行 PYTHONPATH=tools/sessionhelper:tools/sessionhelper/tests python3 -m unittest -v test_dispatch_lifecycle.BusyAdmissionTest。语义撤防：python3 docs/reviews/helper-busy-admission/mutations.py --output <新目录>；需node/playwright/Chromium，NODE_PATH与PLAYWRIGHT_BROWSERS_PATH显式指向本机已安装依赖，缺环境不算通过。本机依赖路径分别/tmp/coder-a3-playwright/node_modules与/home/dev/.cache/ms-playwright；浏览器只连本地夹具。单独浏览器复跑 node view-service/renderer-model-admission-regression.js。

## 完整门与交件

初轮双PATH全门的四profile均收集并执行1203例，唯一失败为test_main_guard_last检查新增测试类位于main守卫之后；已把守卫移到文件末尾，定向15例通过后重跑全门，初轮日志留本机canonical-*-initial.log，不冒报初轮通过。

附加View回归：delivery-status 11项通过、T-074 5项通过；C2报告HARD PASS25，已有T-166已知缺陷4项、形态变化0，不把这些已知缺陷算通过。

双PATH完整scripts/run-build-tests.sh最终均rc0；每路claude/codex profile各1203/1203（94个测试文件），后续27个Python脚本与2个shell脚本全部完成，BUILD_TESTS_OK helper_profiles=2 python=27 shell=2。两路均无named SKIP，不把缺环境算通过。命令、PATH、uid、开始/结束UTC、rc、完整日志字节/hash与四profile原文见raw/gate-summary.json；逐行计数与结果摘录见raw/canonical-*-excerpt.log。完整原始长日志按仓库大文件规则留本机/tmp/busy-cf05/raw/canonical-{normal,no-codex}.log，不入Git；可按所附命令独立复跑。OpenSpec 1.16.0 validate --all --strict为38 passed/0 failed，git diff --check当前rc0。最终commit/tree/父与工蜂ls-remote同值坐标随交件回执提供。


复跑完整门：env -i HOME=<临时目录> TMPDIR=<短临时目录> PATH=<锁定venv/bin及测试PATH> HUB_TEST_PG_STRICT=1 LANG=C.UTF-8 TZ=UTC PYTHONDONTWRITEBYTECODE=1 sh scripts/run-build-tests.sh。普通PATH含/usr/local/bin（codex可见），无codex PATH以仅含node的临时bin替换/usr/local/bin；canonical内部再隔离SH_/DW_/DINGWEI_/DATAD_与CLI状态。完整源文件SHA见raw/source-sha256.json；所有交件文件清单见SHA256SUMS。日志副本仅剥行尾空白以满足diff检查，原始字节hash见raw/original-log-hashes.json，不改写读数/断言。

实现与完整门/证据提交5395a1f5a1a730bbc52041c95314c1295e43cc69，tree 0ae16693b54207cccaf33a8bfe453257db6279e4，父c0777e4b0d26cabc588f22bdf3ae7ce90d412110，已快进push并核ls-remote同值。随后仅追加交付记录/已提交Git对象核验件，不改已测源码；最终tip/tree以平台交审回执为准。
