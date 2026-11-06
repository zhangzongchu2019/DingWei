# C 路飞书网页登录实现证据

本件记录 `k31t2075/r53-feishu-login` 的实现与复核证据。父基线为 `262f4747b3d7def371b44d2feb07e4683f8f87e0`；接口追加依据为 `opus50tc2k/r53-integration` 的 `593ed5efdaa51bf671cc41fa5d8dc0b7c20ed78e` 第 6 节。A 路仅以独立 fetch 的 `2e5e0b02b5d77db0428c5d357152ce7e418f54bf` 做接口阅读，不合入、不修改，也不据此宣称 A 路通过最终验收。


## S-R53-CONNECT-PAGE 接入页追加

当前分支为 `k31t2075/r53-connect-page`，父提交 `97671b3a887901724c34056388ef687515c431b2`；此前 `k31t2075/r53-feishu-login` 已复核冻结，本分支只在追加提交中实现接入页，不改写旧分支。

### 页面与信任边界

- `ServeConnectPage` GET 渲染无脚本、无第三方资源的短名／目标全名表单；页面不注入 Agent 清单。前端短名 pattern 与服务端 `extshared.ValidShortName` 一致。
- GET 签发一次性 CSRF state，POST 原子消费后调用既有 `Start`。`StatePurpose` 区分 `connect_form_csrf` 与 `feishu_oauth`，防止两类 token 交叉使用；AttemptID、App ID 与飞书回调地址也随 CSRF 绑定。
- 目标只按 `名称.属主简称` 做形状校验，存在性、活跃状态与编组权限不通过页面探测，仍交 A 现核。
- CSP `frame-ancestors 'none'` 与 `X-Frame-Options: DENY` 禁止嵌入；另设 no-store、no-referrer、nosniff。POST 成功仅 303 到飞书授权 URL。
- `CallbackRequestFromQuery` 只提取 state 与 code。伪造 short_name、target_agent、redirect_uri query 不会覆盖 state 中的签发意图；`CallbackResult.AttemptID` 供 B 找回外层 MCP OAuth 事务。
- B 自行保存并现核 MCP 客户端 redirect_uri，完成后以自己的一次性 authorization code 回跳；C 不接收该 redirect_uri，本地 Bearer 不进入浏览器回跳 URL。完整字段顺序见 `INTERFACE.md`。

### 新增用例

| 判据 | 用例与读数 |
|---|---|
| GET 表单、前端短名规则、外部名提示、无脚本及无目标枚举 | `TestConnectPageGETRendersSelfContainedValidatedForm` |
| 服务端非法短名拒绝 | `TestConnectPageRejectsInvalidShortName`：空、大写、下划线、连字符、超长、中文均 400 且无 OAuth state |
| 目标为空或形状非法，不查目标清单 | `TestConnectPageRejectsMalformedTargetWithoutLookup` |
| POST 将短名、目标、AttemptID 绑定 OAuth state | `TestConnectPagePOSTBindsIntentToOAuthState` |
| 回调 query 篡改短名／目标 | `TestCallbackIgnoresQueryShortNameAndTarget`：恶意值存在时签发仍为 mybot／Helper.zzc |
| CSRF 重放 | `TestConnectPageCSRFReplay`：首次 303，第二次 403，只有一个 OAuth state |
| CSRF 缺失与 AttemptID 篡改 | `TestConnectPageRejectsMissingCSRFAndAttemptOverride` |
| CSRF 与 OAuth state 交叉使用 | `TestConnectAndOAuthStatesAreNotInterchangeable`，均在飞书换码前拒绝 |
| 禁嵌入头及非 GET／POST 方法 | `TestConnectPageRejectsOtherMethodsWithSecurityHeaders`，并在 GET、POST 拒绝及重放响应检查头 |

### 接入页两刀变异

执行日期 2026-11-06，UTC；每刀后均立即还原并复跑目标用例至通过。

| 变异 | 定向命令 | 有效读数 |
|---|---|---|
| query 适配临时接受 short_name／target_agent，Callback 临时用其覆盖 state | `go test -count=1 ./internal/feishulogin -run '^TestCallbackIgnoresQueryShortNameAndTarget$' -v` | rc=1；签发请求变为 attacker／Other.evil，目标用例红；还原后 rc=0 |
| 删除 `Start` 的 `extshared.ValidShortName` 服务端检查，并移除因此未用的 import | `go test -count=1 ./internal/feishulogin -run '^TestConnectPageRejectsInvalidShortName$' -v` | rc=1；六类非法短名均得到 303 且生成 OAuth state；还原后 rc=0 |

第二刀首次仅留下未使用 import 导致编译红，不计行为变异；移除该 import 后得到上表有效行为读数。


### 接入页最终门禁

完成于 2026-11-06T05:43:32Z，Go 工具链为 `go1.22.2 linux/amd64`；所有命令均在上述父提交加本分支工作树上执行。

| 命令 | 结果 |
|---|---|
| `gofmt -l internal/feishulogin` | rc=0，无输出 |
| `go vet ./internal/feishulogin ./internal/identity` | rc=0 |
| `go test -count=1 -v ./internal/feishulogin` | rc=0；30 个顶层用例、32 个一级子用例 PASS，0 FAIL、0 SKIP |
| `go test -count=1 ./internal/identity` | rc=0，整包 92.448 秒通过 |
| `go test -race -count=3 ./internal/feishulogin` | rc=0，三轮通过 |
| `PATH=/tmp/k31-feishu-build-venv/bin:$PATH TMPDIR=/tmp/k31bt.rYbYSb ./scripts/run-build-tests.sh` | rc=0；2026-11-06T05:33:34Z 开始，05:43:32Z 完成；`BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`，helper 双配置各 1097 条预期用例 |

接入页没有跳过用例，没有连接真实飞书或枚举真实 Agent；目标存在性与活跃状态留给 A 路现核，本页测试不作这些 A 路行为的独立验收。

## 消费者结构与集成字段表

C 包保持独立消费者接口，不引用 `internal/extseat`：

```go
IssueToken(ctx context.Context, req IssueRequest) (IssuedToken, error)
```

### IssueRequest

| 字段 | 类型 | 含义与信任边界 | 集成层到 A 路的映射 |
|---|---|---|---|
| `Principal` | `extshared.Principal` | 回调从服务端 `user_info` 取得并通过 tenant、open_id、显示名校验后的主体；state 暂存时为零值，绝不接受浏览器声明 | 作为 A `IssueToken` 的 `principal` 参数 |
| `ShortName` | `string` | 外部 Agent 短名，按 `extshared.ValidShortName` 校验 | 作为 A 的 `short` 参数 |
| `TargetAgent` | `string` | 目标 Agent 全名；C 只检查非空及无首尾空白，活跃状态由 A 现核 | 作为 A 的 `target` 参数 |
| `AttemptID` | `string` | 可选的调用方登录尝试关联／幂等标识；开始时绑定 state，回调原样传递 | 当前 A 中间签名无对应参数；只保留关联语义，不虚构 A 侧持久幂等 |

### IssuedToken

| 字段 | 类型 | 含义与集成来源 |
|---|---|---|
| `Token` | `string` | A 首次签发的本地 Bearer 明文；只在瞬态签发结果及交给 B 的 `CallbackResult.Token` 中出现，不写 state、日志、审计或错误 |
| `SeatID` | `string` | A 的内部席位 ID；映射 `extseat.Seat.ID` |
| `ExternalAgentFullName` | `string` | 外部 Agent 规范全名；映射 `extseat.Seat.FullName` |
| `Group` | `extshared.AGCode` | 外部席位所属临时 AG；集成层解析并校验 A 返回的 group 字符串 |
| `ExpiresAt` | `time.Time` | 当前本地令牌过期时刻；映射 `extseat.Seat.ExpiresAt`，后续续期仍由 A 权威管理 |
| `Audience` | `string` | MCP 受保护资源标识；由集成统一配置提供，暂定 `https://dw123.cmjrt.com/mcp`，C 不另设默认值 |
| `Scopes` | `[]string` | 固定四项 `list_online`、`send_message`、`wait_for_messages`、`ack`；由集成按第 6 节传入，不做子集授权 |

`CallbackResult` 另携带 `AttemptID` 供 B 找回外层事务，并原样承载以上签发结果供 B 完成标准 OAuth 响应；C 不解释或缩减 audience／scope。

## 接口核验

- 远端 `coder56solzzc/r53-extseat` 的 `git ls-remote` 指向 `2e5e0b02b5d77db0428c5d357152ce7e418f54bf`。
- 独立 fetch 后 `git rev-parse FETCH_HEAD^{tree}` 为 `2b099a6150b6db951453a7c033c019025c77177d`。
- `internal/extseat/types.go` 的 `Groups` 接口与 `e261aaa5f667ae3a59dcd05e86053be646dc696b` 逐行比对无差异。
- A 的实际公开方法为 `IssueToken(ctx, principal, short, target) (string, extseat.Seat, string, error)`；签名差异由组长在集成层写薄适配器处理，C／A 包互不引用。

## 当前验证读数

以下为本轮结构接线检查（2026-11-06T05:06:31Z），不替代原单最终完整门与独立复核：

- `gofmt`：已执行。
- `go test -count=1 ./internal/feishulogin ./internal/identity`：两包均通过；identity 整包读数约 96.577 秒。
- `go vet ./internal/feishulogin ./internal/identity`：通过。
- `gofmt -l internal/feishulogin` 无输出，`git diff --check` 通过；C 的 Go 源码无 `internal/extseat` 引用。
- 成功用例 `TestCallbackSuccessUsesServerProofAndBoundRequest` 覆盖可信 Principal、短名、目标全名、AttemptID，以及 Token、SeatID、ExternalAgentFullName、AG、ExpiresAt、Audience、四项 Scopes 的端到端透传。

## 环境依赖

`go.mod`／`go.sum` 新增 `github.com/mozillazg/go-pinyin v0.21.0`。构建机需在模块缓存预取该模块；测试使用假飞书服务器与假签发器，不连接真实飞书，不需要真实令牌。

## 调研 7.1 八条验收映射

| 验收点 | 本包用例与结论 |
|---|---|
| 错租户、空 tenant、缺身份键、空显示名均不签发 | `TestCallbackRejectsWrongTenant`、`TestCallbackRejectsInvalidIdentityWithoutIssuance`、`TestResolvePrincipalRejectsMissingIdentityFields`；拒绝路径假签发调用次数均为零 |
| state 不匹配、过期、重放及 code 重放 | `TestCallbackRejectsStateMismatchAndExpiry`、`TestCallbackStateReplay`、`TestMemoryStateStoreConcurrentConsume`；相同回调不能取得第二个本地令牌，32 路并发只有一次消费成功。新 state 重放同一 code 由飞书授权码一次性语义拒绝，测试未连接真实飞书 |
| App ID 与登录尝试绑定 | `TestStateBoundToApplicationAndRedirect`；跨 App ID 或跨回调地址均在换 code 前拒绝 |
| tenant 只信服务端 user_info | `TestCallbackSuccessUsesServerProofAndBoundRequest` 与 `CallbackRequest` 结构；浏览器输入没有 tenant、身份或目标字段，成功证明只来自假服务端 `user_info` |
| 令牌明文不进入持久层、日志、审计或错误 | state 只存哈希；`TestCallbackErrorsDoNotDiscloseTokens`、`TestOAuthFailuresAreSanitized`、`TestOAuthExchangeAndUserInfoWithFakeServer` 覆盖错误净化、令牌格式打码及只在签发结果返回本地明文 |
| 吊销立即生效、十四天滑动且不续过期或吊销令牌 | `TestCallbackDelegatesRevocationAndExpiryToA` 只覆盖 C 到 A 的拒绝接线与不明文凭据泄露；真实持久化、吊销及续期语义属于 A 路，不以本包假实现冒充验收 |
| 新应用身份迁移不混并 | `TestPrincipalKeyDoesNotMergeAcrossApplications`、`TestResolvePrincipalEnglishNameAndStableKey`；键包含 tenant、App ID、open_id，union_id 与显示名均不替代稳定键 |
| 新版网页 OAuth 与所有 callback 的一次性 state | `TestAuthorizationURLUsesCurrentFeishuOAuth` 固定新版端点并确认不申请 offline_access；全部 callback 用例先经 state 消费。旧二维码 SDK 的 matchOrigin／matchData 因㊹-补9明确不用旧 SDK 而不适用 |

本包没有跳过用例。A 路真实生命周期和飞书真实扫码不在本路执行范围，边界如上逐项说明。

## 三刀变异读数

执行日期为 2026-11-06，UTC。每刀只改一处保护，运行对应的单条测试，随后立即恢复；恢复后三条目标用例和全包均重新通过。

| 变异 | 定向命令 | 变异读数 |
|---|---|---|
| 将 tenant 检查改为只检查非空，删除与独立配置的精确比较 | `go test -count=1 ./internal/feishulogin -run '^TestCallbackRejectsWrongTenant$' -v` | rc=1；错租户未被拒且假签发 calls=1，目标用例如期转红 |
| 删除 `MemoryStateStore.Consume` 中的删除动作，使 state 可重放 | `go test -count=1 ./internal/feishulogin -run '^TestCallbackStateReplay$' -v` | rc=1；第二次回调再次取得本地令牌，exchanges=2、issues=2，目标用例如期转红 |
| 删除拼音结果的逐字节 a-z 过滤 | `go test -count=1 ./internal/feishulogin -run '^TestPinyinDisplayNameFiltersNonAZ$' -v` | rc=1；结果为 `zhang-wei123` 而非 `zhangwei`，目标用例如期转红 |

第三刀首次读数 rc=0，发现库默认 Fallback 会忽略非汉字，原用例不能区分过滤是否存在，故该次不计有效变异。实现显式让 Fallback 将非汉字交给本包过滤阶段后重跑，得到上表 rc=1；最终正常实现仍只输出 a-z。这是加强变异有效性，不是跳过失败。

## 最终门禁命令与 UTC

最终统一门完成于 2026-11-06T05:14:40Z，Go 工具链为 `go1.22.2 linux/amd64`。

| 命令 | 结果与用例数 |
|---|---|
| `gofmt -d internal/feishulogin/*.go` | rc=0，无格式差异 |
| `go vet ./internal/feishulogin ./internal/identity` | rc=0 |
| `go test -count=1 -v ./internal/feishulogin` | rc=0；21 个顶层用例及 14 个一级子用例全部 PASS，无 SKIP |
| `go test -count=1 ./internal/identity` | rc=0，整包通过 |
| `go test -v ./internal/identity` | rc=0；输出含 77 个顶层 PASS、106 个一级子用例 PASS，多级子用例未重复计数，无 SKIP |
| `go test -race -count=3 ./internal/feishulogin` | rc=0，三轮竞态检测通过 |
| `PATH=/tmp/k31-feishu-build-venv/bin:$PATH TMPDIR=/tmp/k31bt.pxvoo0 ./scripts/run-build-tests.sh` | rc=0；`BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`，helper 双配置各发现 1097 条预期用例 |

统一门首次使用系统 Python 时 rc=1，发现阶段缺少 `websockets` 与 `pexpect`，不是测试失败。随后在 `/tmp/k31-feishu-build-venv` 创建隔离环境，严格执行 `python -m pip install -r tools/sessionhelper/requirements.txt`，由仓内哈希锁文件固定运行依赖，再用上表短 TMPDIR 完整重跑至 rc=0。未修改或跳过构建脚本、helper 清单及任何既有测试。

构建机模块缓存预取要求：在交付提交对应工作树执行 `go mod download github.com/mozillazg/go-pinyin@v0.21.0`，并保留 `go.sum` 中该版本两条校验和。该依赖只用于本地姓名转换，运行不需要访问拼音服务。
