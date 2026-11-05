# S-R51-NP-SESSION-RENEW 交件报告

分支：`review/togpt60lunazzc/r51-np-session-renew-20261104`
基线：`6a7ed27e7eadd152a7ac420f32ca87b9e585039d`（分支起点）

## 实现

- 新平台 identity 会话首次创建时记录 `created_at`；12 小时滑动 TTL，仅剩余时间严格小于 6 小时时续期；每进程、每会话最多每分钟一次；不访问到期会话不会被轮询复活。`CreatedAt` 为零时不续。会话绝对上限为创建后 30 天。主逻辑在 `internal/identity/onboarding.go:229-271`。
- MemStore 与 PostgreSQL 的 `TouchSession` 都原子拒绝已撤销、已过期、无创建时刻、缩短时限的写入，并封顶 30 天：`internal/identity/memstore.go:311-330`、`internal/identity/pgstore.go:487-504`。PG8→9 迁移新增 `np_session.created_at timestamptz NOT NULL DEFAULT now()`；迁移注释说明旧行按迁移时刻填值：`internal/store/migrate/migrations/0104_np_session_created_at.sql:1-7`。新会话 PG 写入及读取 `created_at` 见 `internal/identity/pgstore.go:476-485,507-527`。
- 登录和续期共享一个 cookie 构造器，只有存储确认延长后才重发 cookie；HTTP API 的 N3 登录/认证路径分别见 `internal/identity/httpapi/n3.go:58-96,102-120`。Sibling Console API 的认证中间件在 Actor 校验前调用续期回调：`internal/identity/httpapi/httpapi.go:213-237`。
- m8 浏览器入口通过同一个 `resolveNewPlatformHTTPSession` 续期并重发 cookie：普通 Console 认人、view 认人和能力投影见 `internal/m8/newplat_view.go:111-172,226-253`；session history 见 `internal/m8/terminal.go:280-305`；会话投影以最新 `expires_at` 更新本地 Console 投影，见 `internal/m8/newplat_shim.go:163-200`、`internal/store/newplat_console_projection.go:120-131`；spec clarification 登录见 `internal/m8/spec_clarification_login.go:42-58`；console mux 浏览器 cookie 路径在 PG 授权前续期，见 `internal/m8/console_mux.go:168-187`；spec-agent 的能力投影路径见 `internal/m8/newplat_spec_agent.go:39-49`。m8 的 `RequireConsolePlatformAdmin` 会在外层 `RequireConsolePrincipal` 已续期后再只读确认角色/会话（`internal/m8/obs_hooks.go:26-50`）；能力令牌校验 `newPlatformSessionValid` 没有浏览器 cookie，只做当前会话有效性核验，不续浏览器登录时长（`internal/m8/newplat_shim.go:183-190`）。
- Hosted 人工浏览器路由也调用 N3 续期回调；quota monitor 仅在非 Authorization bearer 请求时尝试浏览器会话续期：`cmd/workpulse/hosted_access.go:87-104`、`cmd/workpulse/hosted_quota_routes.go:54-72`。Agent bearer 路径不触碰浏览器 cookie。
- 独立的旧 `dw_console_session` 续期逻辑原本已有自己的 12 小时/30 天规则（`internal/m8/console_auth.go:20-97`）；本改动补的是 N3 `dw_np_session` 在上述入口的覆盖。

## PG8→9 与 R50 二进制实证

新增 opt-in 测试 `cmd/workpulse/np_session_migration_binary_test.go:20-188`：从确切的 8 条 Hub migration ledger 开始，建立旧 session 后用当前 workpulse 二进制实际迁移到 9；检查旧 ledger digest 未变、旧 session `created_at` 接近 0104 的迁移时刻。随后把认证到该 PG9 的地址交给已认证的 R50 二进制，从 R50 实际执行 password login，并用其签发的 cookie 访问 `/api/console/platform/users`。

实测：`prior_entries=8 unchanged=true total_entries=9; existing_session_created_at~migration_time=true; certified_R50_password_login=200 authenticated_API=200`。R50 二进制版本 `0.2.3.25 eaf8f01cb3eadfadc40180bdca9a500b6d6a4a2a`，SHA-256 `4c6362df58ea1791cd581c7383d7c466067993385857ee528aeffe9b80deeed1`。

## 验证读数

- `go test ./internal/store/migrate -count=1`：通过。
- 最终普通全量 `go test ./internal/store/migrate ./internal/identity/... ./internal/m8 ./cmd/workpulse`：通过；最终测试代码加入 R50 password login 实证后，opt-in PG8→9/R50 二进制测试也通过，退出码 0。
- `scripts/run-build-tests.sh`（隔离环境）：退出码 0，`BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`。
- 变异复核均按原断言变红：移除 TouchSession 写入，`TestSessionSlidingRenewalClockBoundaries` 报 `below 6h remaining: renewed=false`；移除 cookie 重发，HTTP 测试报 `renewal Set-Cookie count=0`；移除 MemStore 或 PG 的 30 天封顶，各自在 `absolute cap session` 断言失败。生产文件已恢复。
- 相关 race 子集 `-race -count=3`：`./internal/identity/...` 通过；m8 的 NewPlatform auth/projection、session history owner、clarification login 用例通过；workpulse 的 hosted human-route 与 quota monitor 用例通过。全量 race 命令 `go test -race -count=3 ./internal/identity/... ./internal/m8 ./cmd/workpulse` 中 identity 包通过，但 workpulse 的包级集成套件在 600 秒包级超时退出；未输出 race detector 报告。受限 race 子集均通过。
- `git diff --check`：通过。

## 真实环境验收

R50 二进制兼容和 PG8→9 已在本机实际二进制/数据库完成。真实 R51 预发 Console 登录与续期仍需按派单由业主在预发验证；本报告不代替该环境验收。
