# S-MCP-ZERO-CONFIG 本地交审报告

执行者 coder56solzzc.zzc；实际 CODEX_THREAD_ID=`01a1bd1c-c28e-7c60-9594-7894d0078533`。
唯一写锁 `/tmp/coder56-mcp-zeroconfig.lock`：本线程持有，其他会话只读、不碰本单工作树。
to=opus50tc2k.zzc

基线是发布提交 `68505cd3d5a5fd9753a1c843655c8387bf2ea15c`，分支 `coder56solzzc/mcp-zero-config`。依据工蜂 rulings `2412f2e1` ㊿-补18；提交前同步至 `c9581bdb` 补19～21，记录于本变更交接。没有基于 r56 后续 tip，没有合入 r57/M3，没有改版本或部署，没有对现网／预发／在线席位执行运维操作。

## 实测结论与适用范围

先于产品改动，使用官方 0.167.0 原生 CLI 在全新隔离 HOME/CODEX_HOME 中执行 `mcp add dingwei --url <隔离服务器>/mcp`。两格都只填 URL，没有 oauth-client-id 或 oauth-resource。随后从官方 npm registry 独立安装 `@openai/codex@0.167.0`，对两格复测。npm 原生二进制与既有 standalone SHA256 一致：`cdcb45aaa01d0f14e298985e09465aebccd47f64ecd298aea44814af7b62bbda`。安装锁与取件坐标见 `raw/official-npm*.json`。

| 服务器元数据 | 实际 client_id | redirect_uri | DCR |
|---|---|---|---|
| CIMD=true，无 registration_endpoint | https://chatgpt.com/oauth/codex/client.json | http://127.0.0.1:随机端口/callback | 没有 POST 注册 |
| CIMD=true，有 registration_endpoint | 同上 | 同一形态，随机端口不同 | 没有 POST 注册 |

两格均 PKCE S256。首次实测原文 `raw/discovery.json`（产品改动前）；官方 npm 重测 `raw/discovery-official-npm.json`，含授权参数、HTTP 请求、stdout、UTC 与二进制 SHA。注册端点只是测试服务器能力，没有加入产品。此结论限定于记录中的 CIMD=true 元数据，未外推其他发现配置。

**Codex 桌面版：未测。** 当前 Linux 工作区没有安装 Codex 桌面应用；检查了应用目录及 /opt 中的应用文件，仅有 CLI/helper。未安装、登录或操作线上桌面席位。CLI 及 app-server 是独立结果，不证明桌面行为。随后㊿-补20澄清历史桌面形态为 `/callback/<12 位安全字符>`，并批准两个精确本地 Codex 身份的有界单段扩展。已按裁定实现；空尾斜杠仍拒，`TestZeroConfigRedirectGuards/path` 专门覆盖。未扩大其他外部身份。**本件是可复核候选，不能宣称桌面“只填地址”目标或 dw123 现网接入已经验收通过。**

当前官方 OAuth 说明可参阅 [Authentication](https://developers.openai.com/plugins/build/auth)；本件所有钉版行为结论都取自上述实测，不用当前文档代替 0.167.0 原文。

## 甲乙比较与实现

| 方案 | 实测相关性 | 代价／边界 | 选择 |
|---|---|---|---|
| 甲：精确默认 client_id 的本地原文副本 | 两格原生客户端均使用该身份 | 固定副本需人工更新；只做精确映射，无在线获取 | 采用 |
| 乙：仅回环公开客户端 DCR | 有注册端点仍没有注册请求 | 新增注册状态与守卫也不能改变当前客户端请求 | 不采用，不增加产品注册端点 |

原文匿名实取 `https://chatgpt.com/oauth/codex/client.json`，HTTP 200；开始 `2026-11-09T02:14:27.652875+00:00`，结束 `2026-11-09T02:14:27.812599+00:00`。SHA256：

```text
88c1677cc7dc907c07184571bba9ecaa342904424823d883270f9c8f62940004
```

逐字节原文为 `internal/mcpgw/codex_client.original.json`，通过 go:embed 内嵌；`raw/codex-client.fetch.json` 记录 HTTP/时刻/SHA。响应头副本只脱敏匿名 Set-Cookie；元数据正文未修改，没有凭据取件。

原文包含 localhost 和 refresh_token，不能原样作为有效策略。`defaultCodexClient` 保留原文身份及名称，取与本网关 **none、原文中有效 IP 回环、authorization_code/code** 的交集，当前文档中的有效登记为 `http://127.0.0.1/callback`。匹配层另按补20有限扩展单段路径，不改原文、不合成 IPv6。坏件、缺必需项和不支持的身份失败关闭，无网络兜底。精确默认 client_id 在 authorize 本地解析；任意近似 ID 仍经既有 resolver，其 HTTPS443、公有 IP、无跳转与防回绑规则不变。显式本域 codex.json 路径原样可用。

代码自查另补相对 URI、无 scheme／host 的拒绝用例：必须先验证完整回调再规范化，避免不存在 scheme 时索引越界。最终 mcpgw、34 刀和原生均复跑，m8/canonical 最终字节复跑均 rc0（raw/FINAL-RESULT.json）。

没有改 registered、tenant、audience 或请求鉴权的产品代码：新增 Codex 专用匹配器仅对两个精确身份将受控路径临时规范到 /callback，复用既有 IP、scheme、端口、query、fragment、userinfo 守卫；原始 scheme 拼写保留校验。单段仅 `[A-Za-z0-9_-]{1,64}`，token 仍绑定授权中未经规范化的实际 client_id/redirect/resource/verifier。没有 DCR、刷新令牌或新增路由。

## 原生 E2E

`raw/final-guard-native/url-only/`（最终产品，补20阶段在 `raw/native-final/normal/`，早期正向在 `raw/native/positive/`）：官方 npm 的真实 0.167.0 `mcp add --url` 自动开始首次 OAuth，配置服务器字段只有 URL；另跑 `mcp login` 验证重新授权，不是 PM 首次必须增加的步骤。真实 Hub/main r53 wiring、真实临时 PostgreSQL 和 Hub SQLite；仅飞书上游为本地合成身份替身。完成授权、四工具目录、`list_online`，`native_e2e_passed=true`、`isError=false`。

`rpc.json` 是原生 app-server 实际 JSON-RPC 原文。只启动 ephemeral thread 读取 MCP 状态并调用通讯工具，没有 turn/start、真实推理或真实账号。工具目录为 list_online/send_message/wait_for_messages/ack；本次只调用 list_online。

`raw/final-guard-native/default-client-cut/`（最终产品，补20阶段在 `raw/native-final/default-client-cut/`，早期刀在 `raw/native/default-client-cut/`）：同一探针，只用 Go overlay 移除精确默认身份本地分支，授权 400 invalid_client，rc1 红在 `NATIVE_OAUTH_AUTHORIZE_MUST_SUCCEED`。

`raw/final-guard-native/explicit-suffix/`：官方原生 CLI 显式使用本域 client_id，测试包装层仅移除 issuer 声明以触发真实客户端安全段回调，add/login、目录、list_online 完整通过。此格验证既有显式路径及补20安全段，不作为 URL-only 或桌面实测。

`raw/native/foreign/`：同一默认身份及原生流程，模拟飞书返回外 tenant，回调 400 access_denied，`foreign_rejected=true`，没有兑换令牌或工具调用。所有隔离 Hub/PG 均正常停止；hub-exit.json rc0。没有使用 dw123 作为测试服务器。

补充发现配置对照：`raw/native-final/issuer-fallback/` 仅在测试 HTTP 包装层移除 issuer 支持声明；CLI 改用 `/callback/<12安全字符>`，同时 client_id 变为 `https://chatgpt.com/oauth/codex/<同段>/client.json`。严格探针在“必须为默认精确 ID”的断言停下，rc1，尚未发 authorize；此格是观察结果，不计入 E2E 成功。产品元数据保持 issuer=true，最终正常格 URL-only E2E rc0。没有擅自接纳动态前缀身份，桌面复核还需观察实际 client_id。

## 守卫与一刀变异

`mutations.py` 用独立 Go overlay，不编辑产品工作树。每刀先跑对应正向基线，再跑变异；要求 rc1、对应断言字符串和测试 FAIL，编译错误不算。最终原文 `raw/final-guard-mutations/RESULT.json`：**34/34 target_assertion_red**；原补18阶段 28 刀另存 `raw/mutations-complete/`。

| 判据 | 专用测试／断言 | 对应变异 |
|---|---|---|
| 默认身份本地解析、精确 ID、旧本域路径 | ExactLocalResolution / ZERO_EXACT_ID | default-local-resolution、exact-id |
| 原文字节与有效策略 | PinnedDocument / ZERO_PROVENANCE、ZERO_EFFECTIVE_POLICY | pinned-original、grant-intersection、response-intersection |
| 坏件无网络兜底、仅公开客户端、仅 IP 回环 | InvalidCopyFailsClosed / ZERO_COPY_* | copy-fails-closed、public-client、ip-only-copy |
| 端口可变、合法范围 | RedirectGuards / ZERO_REDIRECT_port* | random-port、port-range |
| localhost 拒绝 | 既有 LoopbackPortMatchingAndLocalhostRejection / LOCALHOST_METADATA_MUST_BE_REJECTED | localhost |
| 同 IP、scheme、path、query、fragment、userinfo | RedirectGuards / ZERO_REDIRECT_* | same-ip、scheme、path、query、fragment、userinfo |
| S256、challenge、固定资源与 scope | AuthorizationGuards / ZERO_AUTH_* | pkce-S256、pkce-challenge、authorize-resource、scope |
| 补20启用路径／64位上限／安全字符／单段／精确身份、http副本 | NativeCallbackPathsBothClients、CallbackExtensionDoesNotApplyToOtherClients、InvalidCopyFailsClosed / NATIVE_CALLBACK_*、ZERO_COPY_https_loopback | native-callback-enabled、native-length-bound、native-safe-alphabet、native-single-segment、native-exact-clients、copy-http-only |
| token 对身份／真实回调／资源／verifier 绑定 | TokenBindings / ZERO_TOKEN_* | token-client、token-redirect、token-resource、token-pkce |
| 每请求重新鉴权 | EveryRequestReauthenticates / ZERO_REQUEST_AUTH | request-auth |
| 飞书 tenant 与 token audience | 既有 CallbackRejectsWrongTenant、TokenAudiencePG | tenant、audience |

首次探针工具修正的记录保留在 `raw/mutations/` 和 `raw/mutations-final/`：Go 1.22 对嵌入文件的 JSON overlay 未改变内嵌字节，故改为在 Go overlay 的初始化处单刀修改副本字节；单去掉 codec 提前错误分支仍被后续错误处理拒绝，故改为单刀绕过真实 Authenticate 调用。两次未检出都不算目标红，也不是产品失败；补20初版脚本在执行过程中修改了子用例标签，已启动进程仍使用旧标签，`raw/callback-mutations/` 保留“no tests to run”的未检出，不算目标红；脚本补拒空选集后，最终完整重跑 34 刀，断言及工作树字节不变均有记录。

## 回归与复跑

- mcpgw 最终全包：218 PASS、0 FAIL、0 SKIP；`raw/final-guards/mcpgw.*`。
- m8 默认全包：1510 PASS、0 FAIL、37 预置 SKIP；命令与返回码 最终产品 `raw/final-guards/m8.json`，每个跳过名称和原因 `raw/final-test-counts.json`。未修改跳过条件，canonical 另执行其登记的跨 runtime 集成。
- OpenSpec 全量 strict：30 PASS、0 FAIL；`raw/strict-final.*`。
- canonical 首轮 helper 两 profile 各 1129/1129 通过；末尾跨 runtime runner 因本单 TMPDIR 过长失败，原文保留在 `raw/checks/canonical.*`。只缩短隔离测试目录后，完整跨 runtime runner 已 rc0（`raw/cross-runtime-recheck.*`）；完整 canonical 短路径重跑 **rc0**（`raw/canonical-final/canonical.*`，相对 URI 守卫补齐后的最终字节重跑见 `raw/final-guards/canonical.*`），两个 profile 各 **1129/1129**，27 项 Python 和两个完整 shell runner 通过，BUILD_TESTS_OK。原 m8/canonical 测试断言、时限、跳过条件均未改。

环境失败对照 `raw/socket-path-control.json`：只用 Go overlay 记录合成 socket 路径字节数，不改行为、断言或时限；长 TMPDIR 两格路径 109/108 字节，rc1，短 TMPDIR 两格路径 97/96 字节，rc0。未改任何产品或既有测试源码。

复跑（输出路径须不存在；实际官方二进制可通过 --binary 指定）：

```sh
python3 docs/reviews/mcp-zero-config/discovery_probe.py --output /tmp/zc-discovery.json
python3 docs/reviews/mcp-zero-config/mutations.py --output /tmp/zc-mutations
python3 docs/reviews/mcp-zero-config/run_checks.py --output /tmp/zc-checks
OPENSPEC_TELEMETRY=0 openspec validate --all --strict
```

原生探针先在独立终端启动 `python3 docs/reviews/mcp-zero-config/probe_launcher.py --run-dir /tmp/zc-native-review`，等待 ready.json；另一终端执行 `INTEROP_RUN_DIR=/tmp/zc-native-review python3 docs/reviews/mcp-zero-config/native_probe.py --label review`。完成后创建该 run-dir 下 stop 文件并等 launcher rc0。默认分支移除刀给 launcher 增加 `--mutation default-client`，期望 native rc1/指定断言；外租户给 launcher 环境 `INTEROP_FOREIGN_TENANT=1`，native 增加 `--foreign`。不得继承线上配置或使用真实飞书凭据。

Go 由普通 uid1001 执行。canonical 使用 `/tmp/coder-0232-locked-venv/bin/python3`，环境白名单、私有 HOME/TMPDIR；仅复用公开 PostgreSQL 下载缓存。说明稿更新在 `docs/guides/CODEX-DINGWEI-MCP.md`，桌面未测状态明确。补19、21的官方直接设置入口和默认短名／默认目标全量影响、权限及旧版本回退方案在 `DEFAULT-CONSENT-PROPOSAL.md`；只出方案，不提前实施表单、数据库 CHECK 或权限改动。补22只涉及组长后续发布顺序，本件仍不改版本或部署；〔51〕-补15 的 M3 回退自检另排，不在本分支混入。待独立复核，不自验收、不释放写锁，后续只追加提交。

最终状态：最终字节 mcpgw 218/0、m8 1510/0/37 SKIP、canonical 双 profile 各1129/1129及完整27 Python/2 shell、全量strict 30/0，34刀全部目标红；原生 URL-only 与显式本域安全段完整 E2E 通过，默认本地解析移除刀同探针目标红。桌面仍未测，默认授权仍只是方案。stdout和响应头按gzip保留原文（保留原有尾空白，不改写日志），SOURCE-SHA256SUMS与ARTIFACT-SHA256SUMS可独立复算。
