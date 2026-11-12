# S-VIEW-UNKNOWN-UX 实施与版本来源复核材料

## 范围

基线 `153bac4dfe6794a8f2d9438eb4ac4e76d6aa400d`。改动限于 View renderer、已有投递状态浏览器回归、OpenSpec 变更和本审查证据；没有改 Hub、helper、服务端协议或版本号。浏览器回归使用自托管页面与拦截请求，未连接在线 Hub。

未知状态使用固定文案“已送达，Agent 忙时无法确认是否已接收”；计时器继续排除 `model_admission_unknown`。重发弹窗以“不重发”为首个默认提交动作并获初始焦点。processed 阶段或有关联的 `assistant_text`／`mention_reply` 到达后，本页撤掉重发入口；等待弹窗期间收到此类事件，提交前检查也会阻止请求。“不再跟踪”只修改本页内存集合。未知条目超过 `UNKNOWN_DELIVERY_MAX_AGE_MS`（24 小时）后从等待区隐藏，并保留在未知列表且标“已超 24 小时”。回归通过 `window.__viewUnknownUxNow` 注入时钟。

## 浏览器回归与语义变异

定向浏览器回归：`targeted-browser.txt`，12 项全 PASS，rc=0；2026-11-12T04:24:23Z–04:24:24Z。时钟推进到 24 小时以上时未等待真实时间。

每项语义变异都在隔离临时副本执行；原始输出在 `mutations/`：

| 规格 | 变异 | 结果 |
|---|---|---|
| VUX-001 | 移除未知状态对排队计时的排除 | rc=2，红在 `UNKNOWN_COPY_AND_NO_QUEUE_TIMER` |
| VUX-002 | 绕过重发确认 | rc=2，红在 `RETRY_DIALOG_COPY_AND_DEFAULT_NO_RETRY` |
| VUX-003 | 移除本地“不再跟踪”集合写入 | rc=2，红在 `UNTRACK_IS_LOCAL_ONLY` |
| VUX-004 | 让过期判定恒为未过期 | rc=2，红在 `EXPIRED_HIDDEN_FROM_WAITING_RETAINED_IN_UNKNOWN_LIST` |

每份变异记录均含变异名、预期判据名、进程退出码及完整浏览器输出。变异没有写入工作分支。

## View 与 Hub 页面版本来源（只读）

- View 健康接口：`view-service/view_service.py:91-97` 的 `_health_version()` 读取部署环境 `VS_VERSION`，只接受四段数字；`view_service.py:1098-1105` 在认证通过后由 `/internal/health/auth` 返回该版本。此值用于健康接口，不会由此自动写入 View HTML。
- View 页面响应：`view_service.py:1122-1149` 读取 `renderer-canary-sse.html` 并执行路径、public origin、provenance 和成本模式注入。此服务端路径没有注入 `VS_VERSION` 的逻辑；当前 `renderer-canary-sse.html` 也没有版本角标或版本数字模板。因此本 checkout 中 View 页脚没有由 View 代码注入的版本数字。派单提到的截图未随任务提供，不能仅凭描述确定截图数字对应的 DOM。
- 若“页面上的数字”指 Hub 管理台页脚角标：`internal/m8/console_auth.go:263-269` 将 `dwversion.Version` 和 `dwversion.Commit` 传给 `renderConsolePage`；`console_auth.go:311-323` 用它们替换版本模板，HTML 占位符位于 `console_auth.go:409` 的 `#app-version`。因此该角标由 Hub 注入，不来自 View 的 `VS_VERSION`。
- Hub 健康接口：`internal/m8/health_auth.go:20,39-46` 的内部认证处理器返回 `internal/version.Version`。这与 View 健康接口配置来源不同。

要把 Hub 版本显示在 View 页面，可选两种方案：

1. **Hub 提供专用只读版本端点，View 页面读取。** 需改 `internal/m8` 注册/处理、Hub 对外路由策略，以及 View renderer 或 View 服务端代理。页面得到在线 Hub 的权威版本；代价是增加 Hub 接口、暴露版本信息的访问策略，并让角标读取依赖 Hub 可用性。不要把现有健康 HMAC 密钥放入页面。
2. **发布配置向 View 注入 `HUB_VERSION`。** 在发布清单/服务环境中从已批准的同一 `VERSION` 产物设置值，View `_serve_file` 安全替换 renderer 占位符。涉及 View 注入代码、renderer 模板、部署配置和相关验证。无需页面额外请求，故障面较小；如果部署变量没有与 Hub 同步更新，显示可能落后于运行中的 Hub。

两种方案都超出本任务实现范围；本次仅记录调研结论。

## 验证记录

- `openspec validate --all --strict`：OpenSpec 1.16.0，35 passed、0 failed，2026-11-12T05:05:19Z，见 `openspec-strict.txt`。
- 定向浏览器回归：12/12，rc=0，2026-11-12T04:54:00Z–04:54:01Z，确认用例含 `reason=model_admission_unknown` 与 `status=unconfirmed_retryable`，见 `targeted-browser.txt`。四个语义变异复跑均 `rc=2` 且各自红在对应判据，原始输出见 `mutations/VUX-001-queue-copy.txt` 至 `VUX-004-expiry.txt`，完整 runner 输出另有同名 `.log`。
- 全量浏览器 runner：最终兼容运行见原始字节压缩件 `browser-compat-run.txt.gz` 与 `browser-compat-receipt.txt`，2026-11-12T04:54:18Z–05:02:31Z，40/40 套件 `aggregate_rc=0`、runner `rc=0`。`mcp_zero_consent` 的四链均为 `issued=true`，并确认 `realHub=true`、`realTemporaryPG=true`。md-render sync/parity 子门均为 `rc=0`。首次全量尝试见 `browser-run.txt.gz`／`browser-receipt.txt`：其他套件通过，但 `mcp_zero_consent` 子进程选到 Node 18.19.1，而该次安装的 Playwright 要求 Node 20，环境门失败；改用兼容 Node 18 的 Playwright 后完整 runner 全绿。两份 `.gz` 都是 runner 原始 stdout/stderr 字节，仅用 gzip 保存。
- `scripts/run-build-tests.sh`：rc=0，2026-11-12T04:41:33Z–04:50:41Z，`BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`，原始日志 `run-build-tests.txt`。
- `go test ./internal/m8 -run '^TestConsolePage' -count=1 -v`：rc=0，2026-11-12T04:50:54Z–04:50:56Z；其中 `TestConsolePageSourceCarriesNoVersionLiteral`、版本跟随、handler 和传常量四项均 PASS，见 `page-version-gate.txt`。
- `git diff --check`：rc=0，2026-11-12T05:02:54Z，见 `diff-check.txt`。
