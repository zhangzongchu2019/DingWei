# 脱敏真实首屏夹具回放

日期：2026-11-12
夹具：`review/prodops/view-history-fixture-20261112` @ `7f9ca6b936cfaec3e1358d36f67050dfc95a0565`（tree `9a1a91a4`）
修复基线：`8528ed8cf7e43c1bec97102927c92975ed714609`（r59 / `8d39be9b`）

## 结果

真实脱敏首屏响应按 metadata 外层字段组装：generation `0fda970aaff3905c61684736ec808452`，`high_water=4057`，`events` 为 seq 3958–4057 的接口顺序（100 条），`gaps=[]`、`has_older=true`、`before_seq=3958`、`gap_before=false`。事件分类含 22 条 `assistant_text`；其余为 32 `user_input`、29 `tool_call`、17 `state_change`。脱敏内容保留 UTF-16 长度，不代表原文。

| Renderer | 直接 View | Console 四 iframe：刷新后、后台切前台 |
| --- | --- | --- |
| r57 `8426af56` | 0/22 assistant 输出 | 0/22；各会话 history 请求均带自身会话名 |
| r59 `8d39be9b` | 0/22 assistant 输出 | 0/22；各会话 history 请求均带自身会话名 |
| r59 修复后 | 17 个渲染 turn group，覆盖 22/22 assistant 事件、9015 脱敏字符 | 刷新前、刷新重开后、切前台后均为 17 group / 22 事件 / 9015 字符 |

Console 使用四个独立 renderer iframe：`opus50tc2k.zzc`、`ts3opus55.zzc`、`opuszzcgmail.zzc`、`dataconnector.zzc`。请求轨迹为每个 iframe 各发一次 history，刷新并重开后各再发一次，路径分别为 `/view/<同名会话>/history`。没有发现取消、错会话路由或跨标签游标/generation 复用；夹具回放没有触发超时或限流。后台 iframe 保持隐藏后切到前台，修复版完整渲染历史首屏。该 harness 验证的是 iframe/renderer 行为，不模拟浏览器真实网络限流策略。

## 根因与改动

接口响应中的 `event` 是 JSON 对象（服务端 `json.RawMessage` 输出），但两个版本的历史加载循环仅调用 `JSON.parse(row.event)`；对象输入触发异常并被外层静默捕获，导致整页历史项被丢弃。修复在 `view-service/renderer-canary-sse.html` 增加解析器，接受对象并保留字符串兼容；无效行单独跳过。`view-service/history-event-object-regression.js` 覆盖真实外层 envelope、直接 View、Console 四 iframe 刷新/切换、字符串兼容及无效行；已登记到 browser regression runner。

验证：

- `HISTORY_REPLAY_BASELINE=1 NODE_PATH=/tmp/dw-view-playwright/node_modules PW_CHANNEL=chrome node view-service/history-event-object-regression.js`：r57 与 r59 在直接及 Console 场景均复现 0 输出，退出码 0（baseline red 是断言预期）。
- `NODE_PATH=/tmp/dw-view-playwright/node_modules PW_CHANNEL=chrome node view-service/history-event-object-regression.js`：修复版通过，退出码 0，直接及 Console 均完整呈现 22 条 assistant 事件。
- `MUTATE_HISTORY_EVENT_OBJECT=1 NODE_PATH=/tmp/dw-view-playwright/node_modules PW_CHANNEL=chrome node view-service/history-event-object-regression.js`：一刀变异拒绝对象输入后按预期失败，断言观测到 0/22 输出，退出码 1。
- `MUTATE_HISTORY_EVENT_OBJECT=string ... node view-service/history-event-object-regression.js`：一刀变异拒绝字符串输入后在字符串兼容格按预期失败，实得 0/22，退出码 1。
- `scripts/run-build-tests.sh`：在 `env -i`、Node 22 优先 PATH、venv Python、`GOMODCACHE=/home/dev/go/pkg/mod`、`GOPROXY=off` 下通过；`BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`，退出码 0。helper profile 各 1194 测试，额外连接循环 9 测试；Claude verbose bounded 重跑为 `Ran 1194 tests in 475.823s / OK`，10 分钟 timeout 内完成。超过 6 分钟处记录过 `test_self_update_offer.OfferTests.test_new_guard_no_pin_prepare_allows_group_writable_parent`，随后继续跑完；末段 SQLite fd 长跑也通过。verbose 全量在 `raw/helper-verbose-bounded.txt`。
- `tools/run-browser-regressions.py --receipt ...`：42/42 suites `rc=0`，`aggregate_rc=0`；包含 `md_render_core_sync` 与 `md_render_core_parity`，均通过。Playwright 预检要求的 Chromium build 1194 已装入本机缓存（无仓库依赖文件改动）。

## 若业主现场仍缺输出，最短抓证步骤

刷新 Console 前打开 DevTools → Network，勾选 Preserve log；筛选 `history`，选中 `opus50tc2k.zzc` 对应的 `/view/opus50tc2k.zzc/history` 请求。截取：① Request URL 与 Query String（确认会话名、generation/cursor/before_seq）；② Status、Timing（确认取消/超时）；③ Response 的外层 `generation/high_water/has_older/before_seq/gap_before/gaps` 与 `events` 首尾 seq、`events[].event` 实际 JSON 类型及 `kind` 计数；④ Console iframe 的 `src` 和对应标签前后台状态。不要截图或外传未脱敏正文。
