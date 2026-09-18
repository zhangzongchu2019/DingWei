# D1 交付件 · console 会话滑动续期（业主第 7 条）· `opus50tc2k`

- **基线**：`27abfcf032db271a414eeea9ed08685c81b16a12`（H 发版锚 = 现网那一版，与 A 线同基线）
- **裁定读到**：`ed567ffcb5ca47de6a77a0d17a2a42ede4290778`（D1-a 12h 滑动 · D1-b 30 天绝对上限 · D1-c 不要求真人活动）
- **交付分支**：`opus50tc2k/d1-console-session-renew`
- 🔴 **未合并、未推 `tasks/v0.1-B` 的产品代码**（闸仍红；设计件 `DESIGN-D-console-session.md` 已在主干，本件是实现）
- 🔴 **若 H 的 `E_回流` 落地导致基线变化，本分支需 rebase** —— 基在 `27abfcf0` 是因为它是现网那一版。

## 1. 改了什么（5 个产品文件，132 增 5 删）

| 文件 | 改动 |
|---|---|
| `internal/model/model.go` | `ConsoleSession` 加 `CreatedAt` —— **30 天绝对上限的起算点**，没有它就只能纯滑动 |
| `internal/store/console_auth.go` | `ResolveConsoleSession` 带回 `created_at`；新增 `TouchConsoleSession`（三道守卫，见 §2） |
| `internal/m8/console_auth.go` | `newConsoleSessionCookie` 唯一构造器；`renewConsoleSession` 续期判定；`consoleTouchAllowed` 写放大闸；`h.now()` 可注入时钟 |
| `internal/m8/console_login.go` | 登录改用同一个 cookie 构造器 |
| `internal/m8/hub.go` | `NowFunc`（测试注入）、`consoleTouched` 节流表（已在 `New` 里初始化） |

**前端零改动** —— console 页每 15s 打 `/api/console/agents`、view 页每 5s 打 `/view/{s}/status`，
两者都过 `authenticateConsole`，续期挂在那里即可。

## 2. 三个口径怎么落的

- **D1-a 12h 滑动**：`newExpiry = now + consoleSessionTTL`，**只在剩余不足一半（<6h）时才续**
  ⇒ 每会话最多每 6 小时写一次库，与开了几个标签页无关；另加 1 分钟进程内节流，防同一瞬间多标签并发。
- **D1-b 30 天绝对上限**：`newExpiry = min(now+12h, created_at+30d)`；**贴顶后不再延长**，到点按原样过期。
- **D1-c 不要求真人活动**：页面在轮询即续 —— 业主原话就是「只要一直打开」。

`TouchConsoleSession` 的三道守卫（都是「少了也不会有任何测试变红」的那类，故逐条阴性对照）：

    revoked_at IS NULL   已吊销的不得被续活
    expires_at > now     🔴 已经过期的不得被续活（否则「过期」形同虚设）
    expires_at < 新值    只延不缩（并发多标签下，晚到的旧值不得把已续的窗口拉回来）

另有 fail-closed 一条：`CreatedAt` 取不到（老行或格式异常）⇒ **不续**，让用户重登，
而不是让一个上限算不出来的会话被无限延长。

## 3. 🔴 组长那道硬闸：cookie 属性一致

组长要求「续期时的 `Set-Cookie` 与登录时逐属性比对」。
**我没有用「比对」来满足它，而是让不一致在结构上不可能发生**：
`newConsoleSessionCookie(token)` 是**全仓唯一**构造该 cookie 的地方，登录与续期共用。

    Name=dw_console_session · Path=/ · MaxAge=43200 · HttpOnly · Secure · SameSite=Lax

断言仍然逐项写死（`TestRenewedCookieMatchesLoginCookieAttributeByAttribute`），
因为「共用构造器」这件事**本身也可能将来被人拆开**。

🔴 **该断言首版是空的**（组长 2026-09-18 复核打变异发现）：`want` 用 `newConsoleSessionCookie()` 造，
与 `got` **同源** ⇒ 测的是「续期 == 构造器」而非「续期 == 登录」，登录侧怎么改它都不红。
已改为 `want` 取自**一次真实登录流程的 `Set-Cookie`**；`Value` 另立一条断言
（续期必须重发手上那张 token，不得无声轮换）。两发变异（登录侧降级 / 续期侧降级）现各自变红，
见 `runbooks/EVIDENCE-D1-mutation-20260918.md` §2.1。

## 4. 证据（值，不给结论）

### 4.1 单测

    go test ./internal/m8/ ./internal/store/ -count=1     ok / ok
    go test ./...（全仓）                                  39 个包全 ok，0 FAIL

新增用例：

    m8    ConsoleSessionSlidesWhenPageKeepsPolling            剩 1h ⇒ 滑到 now+12h，且重发 cookie
    m8    RenewedCookieMatchesLoginCookieAttributeByAttribute  续期 cookie 与登录 cookie 七个属性逐项相等
    m8    ConsoleSessionNotRenewedWhileMoreThanHalfRemains     剩 11h ⇒ 不写库、不发 cookie
    m8    ConsoleSessionCappedByAbsoluteTTL                    距 30 天上限 2h ⇒ 封顶
    m8    ConsoleSessionAtAbsoluteCapStopsRenewing             已贴顶 ⇒ 不再延、不发 cookie
    m8    ConsoleSessionTouchThrottled                         30 秒内第二次轮询不再写库
    m8    RevokedConsoleSessionIsRejected                      已吊销 ⇒ 401
    m8    ConsoleSessionWithoutCreatedAtIsNotRenewed           CreatedAt 零值 ⇒ 不续（fail-closed）
    store TouchConsoleSessionGuards                            6 个子例：正例 + 4 条阴性 + CreatedAt 带回

**跨过 12 小时是用注入时钟做的**（`Hub.NowFunc`），不是真等 12 小时，也不是只看代码里有 `UPDATE`。

### 4.2 变异验证（把守卫逐条短路，对应用例必须变红）

| 变异 | 变红的用例 |
|---|---|
| 续期整段短路 | `ConsoleSessionSlidesWhenPageKeepsPolling` · `ConsoleSessionCappedByAbsoluteTTL` |
| 去掉续期时的 `Set-Cookie` | `ConsoleSessionSlidesWhenPageKeepsPolling` · `RenewedCookieMatchesLoginCookieAttributeByAttribute` |
| 去掉 30 天绝对上限 | `ConsoleSessionCappedByAbsoluteTTL` · `ConsoleSessionAtAbsoluteCapStopsRenewing` |
| 去掉「剩余不足一半才续」 | `ConsoleSessionNotRenewedWhileMoreThanHalfRemains` |

🔴 **做这一步时发现了我自己验证方法上的一个洞，记下来**：
第一轮用 `go test -run 'Console'` 跑变异，
`RenewedCookieMatchesLoginCookieAttributeByAttribute` **因名字里没有 `Console` 而被静默跳过**
⇒「去掉 Set-Cookie」看起来只被 1 条用例抓到。
**一个筛错的 `-run` 会让变异显得比实际更没被覆盖；极端情况下会让「没抓到」看起来像「抓到了」。**
上表是用 `-run 'ConsoleSession|RenewedCookie|RevokedConsole'` 重跑后的结果。

### 4.3 交付物哈希

    dfa4d6dbf122fb7e59f6b47e7ed51f5eef97473e57d15d7910de943f4564c8c7  internal/m8/console_auth.go
    b23a987981aa142aea56edaa5538f79c1fc5a9dbd3a742f86c738aeaed5ef063  internal/m8/console_login.go
    2fc0be99900f6a6a35919ee80422cc363dc0bdf4ffb7318ba6d29ed6442ac3d6  internal/m8/hub.go
    509e276367fb0c3bf34b3f56726ccf4d312b4ba543c174affbad5129bdafc2db  internal/store/console_auth.go
    68d5cbd5b96043f64cde825a7aab9db02f60f2f7f65109601276ec0740a3dda1  internal/model/model.go
    efecc2141a3677344d844aee3b0e021369539a504c61259dc85decdff672eeb6  internal/m8/console_session_renew_test.go
    18a3956f01abf584d9975d6452632bc544f2a6f8dc4b05738d5c48cc03f3add1  internal/store/console_auth_touch_test.go

### 4.4 顺带被既有不变量抓了一次

仓里有 `TestNewInitializesEveryHubMap`：**Hub 的每个 map 字段都必须在 `New` 里初始化**。
我最初把 `consoleTouched` 写成懒初始化，被它抓红，已改为在 `New` 里初始化。
🔵 那条测试是先前别人留下的，今天替我挡了一次。

## 5. 本件证明不了的

1. **没有在真浏览器里跨过 12 小时** —— 单测用注入时钟覆盖了分支与边界；
   设计件 §5 建议的「改库法 + 真页面」属部署后验证，需要可控环境与业主授权，**不归我自取**。
2. **不证明 D2（写入租约 `writerUntil`）** —— 另一个 TTL。
   🔴 **D1 上线后，一个开着但 8 小时没输入过的页面，点中断仍会 403。** 是否做 D2 请组长定。
3. **不证明生产库存量会话行可续** —— `created_at` 建表即 `NOT NULL`，按代码应可续；
   但我没有生产库可查，属实测项。
