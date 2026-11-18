# S-BROWSER-MUTANT-REVIVE 交件报告

## 一、坐标
- 派单：dw-dlv_6c7cf7d16eb61ad67918c8406f61754b（组长 2026-11-18T05:50:17Z received；本席 06:02Z read＋ack，06:04:58Z 回「已收＋date -u」e880116dc943）。
- 分支 qwen38mvb4/browser-mutant-revive；基 e26d7ee0046ef008cfa850a240d453869b56daa5（qwen38mvb4/browser-followup，冻结）；先 merge main 206d41b26dab646c0e50a111eed5a45ccf7c72f6（openspec r60 结项方案甲归档）＝merge commit 08630dd67，无冲突。
- 代码 commit ad3e0bb7505612236eb1bd92c795aa29743763bd（2 件：view-service/console-browser-regression.js、tools/run-browser-regressions.py；＋28/−7）＝门锚（G/ML receipt commit_sha 逐字同）。交件 tip＝含本报告与证据的提交，见交件信与 ls-remote。
- 不改产品、不改 VERSION、不推 main、不部署。工作树 /tmp/wt-mutant-revive；原始证据 /tmp/mutant-revive-evidence/；交件证据 docs/reviews/browser-mutant-revive/evidence/。

## 二、任务1：unhandled-group-action 复活
- 漂移史：旧锚 status.textContent='编组操作失败: '+String(err&&err.message||err);return false; 已随产品 groupAction→groupFeedback 重构（internal/m8/console_auth.go:797）不存在；旧刀用裸 html.replace 静默 no-op ⇒ 基锚变异全绿＝假刀（S-BROWSER-FOLLOWUP REPORT §四披露、组长 03:53:38Z 裁定2 另单复活）。
- 新锚（现行 catch 路径，Go 源中唯一，grep -c=1）：groupFeedback(feedback||'group-status','请求失败: '+String(e&&e.message||e));return false; → 替换为 throw e;。语义＝网络失败时不写「请求失败: 」反馈而抛未处理异常（与原刀「未处理」语义同形）。
- 守卫：改用既有 mutate()（锚缺失即 throw `mutation anchor missing: <锚>`，与其它刀同口径）；白名单项 unhandled-group-action 原已在列、不动。
- 判别路径：格3「shows group load and network action failures beside their controls」的 failGroupAction='network' 段，expectText('#groupNameFeedback','请求失败:','请求失败:')——条件等待超时被 .catch(()=>{}) 吞掉后 assert.ok 业务断言红（criterion='请求失败:'，非 TimeoutError、非进程 throw）；实得值＝前段遗留 'fixture create denied'（catch 被变异后反馈不再写入）。HTTP 500 段（!r.ok 路径）不受本刀影响；silent-group-action-feedback 刀（HTTP 路径）与本刀互补、互不消费对方锚。
- 零负载静态读数（06:1xZ，窗口内安全项）：
  - node --check / py_compile 绿；runner --selftest-env-coverage rc=0。
  - guard-diag.js（抽取文件内真实函数体 eval，无浏览器）：GUARD-DIAG OK——未变异 html 含锚；变异后锚被消费、.catch(function(e){throw e;}); 恰一处；silent 刀不影响 catch；把锚换回旧漂移串即 throw mutation anchor missing（假刀不可复发）。
  - 破锚副本实跑（console-broken-anchor-diag.js，锚串改「请求失败漂移: 」）：rc=2，Error: mutation anchor missing: …于 main:110 HTML 构建期抛出（chromium.launch 在 246 行之后）⇒ 零浏览器。证据 broken-anchor-run.log。
- 浏览器读数（Phase Q，07:51–08:07Z，见 §四）：未变异三连 0/0/0（各 20/20）；--mutate=unhandled-group-action rc=1 红在格3 业务断言（19/1）；插桩副本转储实得值 #groupNameFeedback actual="fixture create denied"（原件断言不改）；基锚 e26d7ee0 旧刀 rc=0（20/20）＝假绿对照成立。

## 三、任务2：flood receipt 记 loadavg
- tools/run-browser-regressions.py：新增 read_loadavg()（/proc/loadavg 前三值，OSError→'unavailable'）；main 循环在 renderer_flood_stress 开跑前/结束后各读一次；receipt() 增两行 suite.renderer_flood_stress.loadavg_start=… / loadavg_end=…（None 不写、缺值 unavailable）。阈值、判法、aggregate 逻辑、SUITES 均不动；无新增 env 读 ⇒ env 登记自测仍绿。
- 单元读数（receipt-unit.log）：read_loadavg()=4.44 4.38 4.33（06:14Z 宿主基线）；伪造 results 下两行逐字正确；None 无行；{} 双 unavailable。RECEIPT-UNIT OK。
- 全量 runner 实 receipt 样本（G-full.receipt，08:07:45Z，commit_sha=ad3e0bb75…）：
  - suite.renderer_flood_stress.rc=0
  - suite.renderer_flood_stress.loadavg_start=5.65 5.87 5.54
  - suite.renderer_flood_stress.loadavg_end=5.23 5.74 5.52
- 负载段实 receipt 样本（ML-full.receipt，09:07:10Z，2x nice19 负载下）：
  - suite.renderer_flood_stress.rc=1（负载敏感红，见 §4.3/§7；静默权威 rc=0）
  - suite.renderer_flood_stress.loadavg_start=196.88 155.58 79.79
  - suite.renderer_flood_stress.loadavg_end=197.53 161.54 85.40
  - 两行如实记录负载段起止 loadavg ≈197（2x＝192 spin＋runner），即任务2 的预期效果：负载可见、阈值不动。

## 四、门与读数
### 4.0 开窗与限额依据
- 切换窗口结束：组长 dw-dlv_1b976c81（r60 第5次切换 04:14:09Z SWITCH_PASS）；勿压测令解除。
- 负载限额：e8539dfe461f＋组长 02:44:24Z 修订（2x＝192 spin 全 nice19；段前后 conn_seq；段内重连 >2 停段并报）。本单派单门＝console×3＋全量×1（限额照旧）。
### 4.1 Phase Q 静默段（07:51:04–08:07:45Z，run-mutant-gates.sh）
- console 未变异 ×3：rc=0/0/0，各 passed=20 failed=0（G-console-1..3.log/rc）。
- 刀跑 --mutate=unhandled-group-action：rc=1，FAIL 格3「shows group load and network action failures beside their controls: 请求失败:」，passed=19 failed=1（G-knife.log/rc）＝业务断言红、非进程崩。
- 插桩副本（console-instrumented-mutant-diag.js，expectText 转储实得值；诊断件不入产品路径）：rc=1 同格红；EXPECTTEXT-DIAG #groupNameFeedback actual="fixture create denied"（G-knife-instrumented.log/rc）。
- 基锚对照（/tmp/wt-followup＝e26d7ee0，旧刀）：rc=0，passed=20 failed=0（G-anchor-oldknife.log/rc）＝旧刀假绿实证，与新刀红对照成立。
- 浏览器全量 runner ×1：aggregate_rc=2，46 套件 44 绿（G-full.log/rc/receipt）。两红：
  - r15_session_history_pagination rc=2（r15:96 waitForFunction 3000ms 超时）；
  - r15_session_history_mutations rc=1（drop_history_node_move: wrong assertion failed）。
  - 判定：两红均属冻结时序窗分支 415b9bf5 的既有负载/时序窗范围；本 commit 对 r15 两文件 diff 为空 ⇒ 继承、非本单回归。flood_stress rc=0、console_browser rc=0、provenance_icon rc=0、retry_role rc=0（现网热修与 r60④ 格原样绿）。
### 4.2 Phase B 构建门（scripts/run-build-tests.sh，AGENTS.md 交件门）
- 轮1（G-build-r1-systempython.log，rc=1）：系统 python3 误选 ⇒ helper 4 模块无收集（与 N3 作者首轮同型环境误选）；质量三闸已过。存档披露。
- 轮2 venv PATH（08:18:17–08:29:33Z，G-build.log，rc=1）：质量三闸绿；HELPER_INVENTORY_OK files=97；
  - claude profile：HELPER_GATE_RESULT rc=0，actual=1270/expected=1270（groups 1261＋9）；
  - codex profile：rc=1，failures=1，唯一红名 test_guide_staging.GuideRealAdapterBoundaryTest.test_claude_stream_json_user_frame_boundary_counts_zero_one_two（AssertionError '[DingWei platform context…COND' != 'SECOND'）；
  - helper 闸 set -e 先停 ⇒ python/shell 段未及（独立补跑见下）。
- 基线对照（e26d7ee0，--root /tmp/wt-followup，08:36:12–08:47:21Z，G-build-base-control.log，rc=1）：claude rc=0（1270/1270）、codex rc=1 **同一红名同一断言** ⇒ 继承既有红、非本单回归（实证＋diff 双证：本 commit 不触 helper/guide）。
- python/shell 独立段（08:48:12–08:51:05Z，run-python-shell-gates.sh，G-ps-driver.log）：27 python＋2 shell＝29/29 RC=0 全绿。
- 处置：继承红记名不修、不重试至绿；BUILD_TESTS_OK 因 helper set -e 未达，披露请裁归桶（S-HELPER-FLAKE-2 或另）。
### 4.3 Phase L 负载段（2x nice19，08:53:55Z 起，run-mutant-load.sh）
- 负载：192 spin 全 nice19（ps ni=19×192 核；pgrep 启动瞬计 193 含一过态包装、稳态 192；marker DWLOAD2XM、5400s 自 expiry）；段内 loadavg 峰值约 197（09:02Z 采样）。
- 连接监视（monitor_conn_ml.py，10s 轮询 helper hub TCP identity＋IPC 探针；重连 >2 自动停段）：
  - 段前 ML-conn-before.json（08:53:56Z）：helper PID555221，hub TCP inode 1212273325，local port 37134（0x910E），remote 43.129.232.140:443，ipc_connect_ex=0。
  - 段内监视日志 inode 恒同；ML-reconnect-events.log 不存在（零事件）。
  - 段后 ML-conn-after.json（09:07:19Z）：helper PID555221 不变、inode 1212273325 恒同、ipc_connect_ex=0；monitor_end reconnects=0 ⇒ **段前后 conn_seq 同一连接、重连 0**（限额合规）。
- console ×3（负载下）：rc=0/0/0，各 passed=20 failed=0（ML-console-1..3.log/rc，08:54–08:55Z）。
- 全量 runner ×1（并行，08:53:56–09:07:10Z）：aggregate_rc=2，46 套件 **43 绿 3 红**（ML-full.log/rc/receipt）：
  - r15_session_history_pagination rc=2、r15_session_history_mutations rc=1：与 Phase Q 同型同格，冻结 415b9bf5 时序窗范围继承红（本 commit r15 diff 为空）。
  - renderer_flood_stress rc=1：FAIL [flood] X0「灌入真走完：页面收齐最后一帧、推入速率不低于每秒 5000 条」20000 !== 100002；STRESS READINGS page_unresponsive=true、receive_rate=4613<5000、loadavg≈197。判定＝既有负载敏感（组长 2026-11-17T22:31:52Z 已把 flood_stress 负载敏感归后续统筹单；冻结令 03:53:38Z「flood 始终跑＋静默权威」）；**静默段 Phase Q 同代码 rc=0 为权威绿**；本 commit 只加 receipt loadavg 两行、阈值判法不动 ⇒ 非本单回归。
  - console_browser rc=0、provenance_icon rc=0、retry_role rc=0、r15_follow_release_after_replay rc=0（现网热修来源图标格、r60④ 重发格、followup 新格负载下原样绿）。
- 收段（09:07:13Z）：ML.done 在、ML-STOPPED 无；burner pkill 后 ML-load.killed-count=0、ps ni=19 计数 0 ⇒ 负载全清。

## 五、与 415b9bf5（时序窗 v2，冻结）hunk 相交
- 本单唯一 hunk：@@ -67,11 +67,16 @@（e26d7ee0 坐标 67–77，刀区）。
- 415b9bf5 对共同产品基 501e5b8a 的同文件 hunk：@@ -69,7 +69,11 @@（legacy-group-deadlock 复活，刀区）＋@@ -530,7 +534,12 @@、@@ -559,6 +568,12 @@（零组格等待）。
- **相交**：刀区 hunk 相邻/共享上下文（e26d7ee0→415b9bf5 反向 diff @@ -68,15 +68,12 @@ 覆盖 68–82，含本单改动行）。两复活语义独立（unhandled-group-action＝catch 路径；legacy-group-deadlock＝重试控件挪进隐藏区），集成者取两者＋保留 silent-group-action-feedback 刀与白名单项（415b9bf5 基无此刀故其白名单少一项，合并时以本分支白名单为准）。零组格等待 hunk 与本单不相交。

## 六、边界与复核入口
- 只改两测试/runner 件＋docs；诊断副本（破锚、插桩）不入产品路径、归档 evidence/；不自验收。
- 复核命令：cd 工作树；NODE_PATH=/home/is3/pwrun/node_modules node view-service/console-browser-regression.js [--mutate=unhandled-group-action]；python3 tools/run-browser-regressions.py --receipt <path>；node evidence/guard-diag.js（静态守卫诊断）。
- 证据清单与 UTC 读数：evidence/ 内 G-*（Phase Q/B）、ML-*（Phase L）、诊断件与脚本；SHA256SUMS 交件时生成。

## 七、披露与请裁
1. 全量 runner 两红（r15 pagination/mutations）＝冻结 415b9bf5 时序窗范围继承红，本 commit r15 diff 为空；负载段若再现同型红按同口径判。
2. helper codex profile 1 红（test_guide_staging…zero_one_two）基线对照同红 ⇒ 继承；记名不修不重试至绿，请裁归桶（S-HELPER-FLAKE-2 或另）。BUILD_TESTS_OK 未达由此红＋set -e，非本单改动所致。
3. Phase B 轮1 系统 python 误选（rc=1）为环境误选、轮2 venv 重跑为准；两日志均存档。
4. 负载段限额合规：2x＝192 全 nice19、段前后 conn_seq 同一连接（inode 1212273325）、重连 0（monitor_end 实证）、burner 收段全清。
5. 负载段 flood_stress rc=1＝既有负载敏感红（X0 速率/收齐断言，receive_rate 4613<5000、page_unresponsive、loadavg≈197）；按冻结令「静默权威」以 Phase Q rc=0 为准；阈值未动、非本单回归；归后续统筹单口径（组长 22:31:52Z 已分类）。
