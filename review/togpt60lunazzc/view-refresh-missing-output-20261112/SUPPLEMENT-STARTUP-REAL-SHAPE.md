# S-VIEW-REFRESH-MISSING-OUTPUT｜真实历史响应对象形状补件

日期：2026-11-12 UTC。基线分支 `togpt60lunazzc/view-refresh-missing-output-fix`，r57 `329eb9f9c45d2d785a0429b01eaa0fa5264fe15d`，r59 `8528ed8cf7e43c1bec97102927c92975ed714609`。本补件只做只读代码检查与 loopback mock 重放；未连接现网、未读生产数据库、未改动产品代码。

## 结论

根因属于 **b：数据已在首屏 history 响应中，但 renderer 没有重放**。关键形状是 Hub 返回的每条 `events[].event` 为 JSON 对象，而非 JSON 编码字符串。r57 与 r59 的 View renderer 都执行 `JSON.parse(row.event)`，对象被转换成 `"[object Object]"` 后解析失败，catch 后跳过该行。prodops 最近首屏页有 100 行，其中 22 条 `assistant_text`、合计 9,015 字符；r57/r59 对同一真实结构对象页在直接 View 与 Console 四 iframe 重放中均渲染 0/22。故障并非 payload `session_id`、`turn_id`、generation、kind 过滤、首屏条数、4 MiB 截断或 Hub gap。

此前提交的 `REPORT-REAL-IDENTITIES.md` 和 `repro-real-identities.js` 将 `event` 错构造成 JSON 字符串，所以得到的“正常显示”结果不代表 Hub 实际首屏响应；其中刷新、实时、翻页及 session 切换结果应视为无效的对象响应复现结论。`raw/real-identities-repro.stdout.txt` 和相应旧 trace 保留作历史记录，本补件以 `repro-history-object-envelope.cjs` 及 `raw/prodops-object-envelope-baseline.stdout.txt` 为准。

## 证据与复现

prodops 提供的首屏响应来自 generation `0fda970aaff3905c61684736ec808452`，seq 3958–4057 连续 100 行；首屏包括 22 条 `assistant_text`。捕获已去除正文，仅保留输出 UTF-16 字符数和渲染所需结构。原始响应和元数据在 `raw/prodops-first-page-latest100.json` 与 `raw/prodops-first-page-metadata.json`，其 SHA256 见同目录来源校验文件。

`repro-history-object-envelope.cjs` 将捕获页装配为 Hub 实际 JSON envelope：`events[].event` 直接是对象，包含顶层 `kind`、`session_id`、`turn_id`、`delivery_id`、`origin_delivery_id`、`text` 和 `data`；按 r57/r59 指定 commit 读取 renderer。它同时以 `internal/m8/console_auth.go` 中 Console HTML mock 为页面源，验证直接 View 与四个 Console iframe，检查完整 `/view/opus50tc2k.zzc/history` 请求。输出记录于 `raw/prodops-object-envelope-baseline.stdout.txt`。结果：r57、r59 均为直接 View 0/22、Console reload/切回前台 0/22；每个路径的 history URL 都使用完整 `.zzc` 名称且首屏无 generation/before/cursor 参数。测试结尾标记 `HISTORY_EVENT_OBJECT_BASELINE_RED` 表示缺陷基线复现成功。

原始 47 行窗口的事件字段、身份长度、正文字符数、record_bytes 与 generation gap 证据仍见 `raw/prodops-shape-supplement.txt`、`raw/prodops-window-3907-3953.json`、`raw/prodops-COMBINED-ADDENDUM.md`。该窗口 seq 3907–3953 共 47 行；user_input 的 session_id 为空，首个 turn_id 为空；输入与 assistant_text 有 7 个非空 turn_id 交集。payload.session_id 切换只发生在同一历史键/generation 中，不能解释首屏全体对象行被跳过。窗口 record_bytes 合计 49,673、最大 3,602；generation oldest/newest seq 为 1/4025，`gap_through_seq=0` 且 gap 表为空。

## Hub 与 renderer 路径

- Console 路由 `internal/m8/newplat_view.go` 注册 history handler；`internal/m8/terminal.go:287–350` 的 `HandleSessionHistory` 根据 URL 中完整 session name 与 owner/agent identity 读取 history。Store `internal/store/session_history.go:693–760` 按 owner、agent key、session name 与 generation 查询，可选 `seq < before`，按 seq 倒序取 100 条、受 4 MiB 上限约束，最终升序返回。没有按事件 kind 或 payload.session_id 过滤。
- Hub 响应结构 `internal/store/session_history.go:83` 的 `SessionHistoryEvent.Event` 是 `json.RawMessage`；它在页面封装时原样保留事件 JSON。JSON marshal 后响应里的 `event` 因而为对象。这里与生产捕获的响应 envelope 相符。
- 首屏 renderer URL 由 `view-service/renderer-canary-sse.html` 的 `loadSessionHistory` 构造：`/view/{full session name}/history`，首屏不带 generation、before 或 cursor；后续页才带 generation 和 before。Console iframe URL 从 `internal/m8/console_auth.go:605–610` 将完整 `full_name` 编码为 `/view/<name>`，包含 `opus50tc2k.zzc`。
- r57 与 r59 renderer 对事件行的处理均在 history loop 调用 `JSON.parse(row.event)`；历史基线代码位置分别为两 commit 的 `view-service/renderer-canary-sse.html:4519` 与 `:4683`。对象入参触发 `JSON.parse` 异常并被本行 catch 忽略。实时 SSE 路径收到的已解析对象直接进入事件处理，因此“新消息到来时能显示”与现场描述相符；这是从两条代码路径和历史复现得出的机制解释。

## 修复建议、风险和回归

只改 renderer 的 history 解码边界：接受对象（直接使用）以及旧版本/兼容服务可能给出的 JSON 字符串（解析一次）；拒绝 null、数组、不可解析字符串或缺少有效 kind 的行，并继续隔离单行错误。保留现有 generation、seq 排序、事件去重及 session 聚合逻辑。预计 0.5–1 人日含浏览器回归。主要风险是错误地把 malformed envelope 当有效事件、或字符串/对象双路径造成重复处理；逐行 schema 校验与 event identity 去重可控制风险。Hub 查询、Console session 路径和 helper 均无需因本证据改动。

建议回归与一刀语义变异：

1. `HISTORY_OBJECT_EVENT_REPLAY`：以 100 行 prodops 脱敏页作为对象 envelope，刷新 direct View 和 Console iframe 后均须渲染 22/22 assistant_text。**变异：**在 history loop 强制 `JSON.parse(row.event)`；应回到 0/22。
2. `HISTORY_STRING_EVENT_COMPAT`：字符串 envelope 仍渲染正确。**变异：**移除字符串解析兼容；旧格式夹具应转红。
3. `HISTORY_BAD_ROW_ISOLATION`：无效对象/JSON 行被跳过但后续合法 assistant_text 仍显示。**变异：**把单行 catch 改为中断整个 history loop；后续输出断言应转红。
4. `HISTORY_REQUEST_IDENTITY_AND_PAGING`：首屏请求 full session URL 且不带 before/generation；向上翻页带正确 generation 与最旧已显示 seq。**变异：**截短 `.zzc` session path 或删掉 generation；相应请求断言应转红。

这些是修复建议和预期 mutation，不表示本补件已把产品 fix 纳入本分支。工作区中出现了其他并发未提交的 renderer/回归改动；本补件没有 stage 或提交那些文件。

## 校验与坐标

- 对象 envelope 重放：`NODE_PATH=/tmp/r52-node-deps/node_modules PW_CHANNEL=chrome node review/togpt60lunazzc/view-refresh-missing-output-20261112/repro-history-object-envelope.cjs`，结果 0/22 × r57/r59 direct/Console；进程退出 0。
- `node --check review/togpt60lunazzc/view-refresh-missing-output-20261112/repro-history-object-envelope.cjs` 通过。
- `scripts/run-build-tests.sh` 使用隔离 venv 成功，结果 `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`；日志 `raw/build-tests.stdout.txt`。这是在包含并发未提交工作区改动时运行的仓库门，不是 renderer 专项修复验证。
- 请求所给 `d51020b0e79e` 不存在于本地 Git object database；此前 fetch 也找不到该远程 ref。此工作仍基于明确给出的 r59 commit `8528ed8c`，不会声称已并入 d51020b0e79e。
