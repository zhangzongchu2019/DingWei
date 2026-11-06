# S-R53-MAIN-WIRING 交件证据

执行者 coder56solzzc.zzc，实取线程 01a1b1a2-3a7e-7520-8486-a085b53806fe。2026-11-06T19:34:40Z 派单，19:34:51.149377Z 起原子持有 r53-main-wiring.writer.lock；锁保持至推送核验和交件回报完成。仅本席自测结果，独立复核归组长；没有部署、重启或使用真实飞书登录。

## 提交与范围

- 指定基线：opus50tc2k/r53-integration，2f423e6c2be8453087433918c54cbfab25abab67。
- 源码冻结：d0fdf47fb22a4b88addc61b7c9119cc2522ba9ac，由 git rev-parse HEAD 实取。以下全部正式门与三刀变异均绑定该提交；raw 的 JSON 分别保存命令、起止 UTC 与 rc。
- 分支：coder56solzzc/r53-main-wiring，仓库 git@git.code.tencent.com:wego-ai/dingwei.git。源码已于 19:40:12Z 经 git ls-remote 核为上述精确提交。最终交件 tip 为该提交的仅证据子提交，以 DingWei 回执实取 git rev-parse 的全值为准。
- 源码仅四文件：cmd/workpulse/main.go、r53_extseat.go、r53_extseat_test.go、r53_wiring_test.go。A/B/C/D、helper、迁移及共享合同/期望串均未修改。本派单限定只改 cmd/workpulse，故证据和交接也收在本目录，未修改共享 OpenSpec 文书。
- 源码提交后重新 fetch 集成分支，FETCH_HEAD 仍是指定 2f423e6c；本能力 OpenSpec 目录与 R53-MVP-CONTRACT.md 相对派单基线无变化。

## 装配结果

- main 使用 Hub 的既有新平台 publicOrigin 配置，复用 config.LoadBootstrap 的 FEISHU_APP_ID／FEISHU_APP_SECRET；不新增飞书密钥、不读取请求头拼配置。
- DW_R53_EXTSEAT 不为 1 时保持不执行装配；已有 r53ConfigFromEnv 继续拒绝未开启新平台的组合。wireR53 另设关闭态早返回，实际 ServeMux 全注册路径集合在空值、0、true 三种关闭值前后完全相同。
- cfg.resource 必须精确等于 cfg.publicOrigin＋/mcp，且 origin 必须是无路径、查询串、片段和用户信息的 HTTPS origin；不一致返回明确配置错误，不降级成 B 单路关闭。
- wireR53 的 extseat.Groups 参数由调用者注入。main 明确传 nil，D 未并入时维持 errR53GroupsMissing，拒启动且不注册半套路由；组长合入 D 后替换该参数即可。本包没有 D 生产实现。
- extseat.Service 使用同一 PG Pool、SystemCtx、Hub.Identity、统一 TenantKey/Resource 和 m8.HubHelperPresence。C Handler 使用统一回调 PublicOrigin＋/mcp/oauth/feishu/callback、extwire.Issuer 与 mcpgw.LoginStates；B 经 registerOptionalMCPGateway 注册，Origins 显式 nil，CIMD 默认 mcpgw.MetadataClients。
- 密钥文件缺失等错误由 B 的既有入口记录明确日志并关闭 B，wireR53 不传播该错误去关闭 Hub；未注册 MCP 路由，也不安装 ExternalSeats/后台 worker。测试密钥由 crypto/rand 生成。
- B 成功注册后安装 Hub.ExternalSeats，并启动 A 的 Run＋HubExtseatProjector；worker 纳入既有 lifecycle.Group，Hub 取消时退出，测试在关闭 PG 前等待退出。日志不记录凭据或底层错误正文。

## 正式门

全部 PG 门显式 HUB_TEST_PG_STRICT=1。运行程序剥离继承的 SH_／DINGWEI_／DW_／DATAD_ 环境，并使用短 TMPDIR；canonical 使用既有锁定 Python 环境 helper-deps-lock-venv。实际命令和 UTC 见 raw/*.json；Go JSON 事件含本机 -08:00 时区，JSON 命令记录统一 UTC。

| 门 | 精确受测提交 | 结果与日志 |
|---|---|---|
| go build ./... | d0fdf47f | rc0；raw/build.json、build.log |
| go vet ./cmd/workpulse | d0fdf47f | rc0；raw/vet.json、vet.log |
| cmd/workpulse 整包，count=1 | d0fdf47f | rc0；264 RUN，258 PASS、6 既有 SKIP、0 FAIL；raw/cmd.json、cmd.log |
| mcpgw／extseat／feishulogin／extwire 四包，count=1 | d0fdf47f | rc0；207 RUN、207 PASS、0 SKIP；raw/related.json、related.log |
| 装配与密钥相关用例 race，count=3 | d0fdf47f | rc0；72 RUN、72 PASS、0 SKIP；raw/race.json、race.log |
| OpenSpec validate --all --strict，锁定 1.16.0 | d0fdf47f | rc0；raw/openspec-locked.json、openspec-locked.log |
| scripts/run-build-tests.sh | d0fdf47f | rc0；两个 profile 各1097项、27 Python、2 shell；claim 内存和真实PG各五子项实际 PASS；raw/canonical.json、canonical.log |

raw/go-summary.json 可复核每个 Go 包和用例事件计数；RUN 包含子用例。这里未声称四个业务包或 cmd 整包做了三轮 race，只对命令中列出的装配/配置/密钥用例执行三轮。

## 核心集成用例

TestR53WiringPG：真实临时 PG 经 openNewPlatformStore 施加正式迁移，D 以 wiringGroups 假实现注入且在 A 提供的同一 PG 事务写双成员；飞书采用 C 的真实 FeishuClient 调用 httptest HTTP 上游；CIMD 用本地 resolver 替身，不访问公网或真实飞书。

实际登记 /.well-known/oauth-authorization-server、/.well-known/oauth-protected-resource/mcp、/mcp、/api/external/、/oauth/authorize、/oauth/token、/feishu/connect、/mcp/oauth/feishu/callback。实际走 authorize → C connect CSRF → 飞书 code/state 回调 → A 落 PG 签发 → B 一次性 code → token → 带标准请求头和能力元数据的 MCP tools/list；断言四工具包含 list_online、无 RPC error，并验证兑换重放拒绝。飞书上游还核对既有 AppID、AppSecret 与统一 callback URI。

同用例先验证 nil Groups 明确拒启动且没有半注册；missing_key_keeps_hub 子例验证 key 缺失时 /healthz 保持 204，B 路由和 ExternalSeats 均未安装。TestWireR53SchemaCheckPG 保留真实表齐/缺表拒绝；TestR53ResourceMustMatchPublicOrigin 覆盖跨 origin、空 origin、尾斜杠、空查询与 HTTP；TestR53DisabledRouteSetUnchanged 比较真实 ServeMux 完整注册集合。

## 三刀独立变异

每刀用 Go overlay 替换 d0fdf47f 的单个源文件，不修改正式工作树，不提交变异源。raw/mutant-*.diff 给出唯一变化，*.json 与 *.log 保存实际命令和原始红读数；使用 -vet=off 仅避免 overlay 的独立 vet 文件定位干扰，三刀均编译通过并红在断言。

| 移除保护 | 实跑用例 | 失败原文 |
|---|---|---|
| 删除 groups == nil 拒绝 | TestR53WiringPG | missing D accepted or wrong error: <nil> |
| 删除 Resource／PublicOrigin 一致性检查 | TestR53ResourceMustMatchPublicOrigin | origin="https://other.example": want consistency refusal, got extwire: no table checker |
| 关闭态额外注册 /r53-disabled-mutant | TestR53DisabledRouteSetUnchanged | disabled r53 changed registered route set（三个关闭值均红） |

复核可在独立 d0fdf47f 工作树分别应用一个 raw/mutant-*.diff，再运行对应测试；每刀从原提交重新开始，预期 rc1。未把编译错误或环境错误算作杀死变异。

## SKIP 清单与历史诊断

cmd 整包的 6 条均为既有显式环境门，不是本次新 PG 用例跳过：

| 用例 | 原始跳过原因 |
|---|---|
| TestNPSessionPG8To9AndR50BinaryCompatibility | set independently built current and certified R50 binary paths |
| TestR31OldBinarySchemaReadCompatibility | requires independently built R30 binary via R31_R30_BINARY |
| TestHLCFaultProcessMatrix | isolated fault/watchdog runner tests/hlc/fault_matrix.py |
| TestHLCBindFailure | isolated startup failure runner |
| TestHLCLoadMatrix | isolated actual-process matrix: tests/hlc/process_matrix.py |
| TestHLCS007Full | isolated full mixed fault: tests/hlc/s007_full.py |

草稿用例的首次两次运行曾分别因测试请求漏 SSE Accept（406）和漏 clientCapabilities 元数据（400）失败，均在 d0fdf47f 提交前修正；原始 raw/draft-accept.log、draft-capabilities.log 保留，不当作正式提交读数。OpenSpec 首次从 PATH 调用不存在（127）；随后定位本机既有 1.16.0 安装并全量 strict rc0，原错误 raw/openspec-path-missing.log 保留。未掩盖、改写或以失败草稿冒充通过。

## 交接

完成：cmd 内主装配、统一配置、D 注入、默认关闭、真 PG 跨 A/B/C 登录与三刀变异；所有正式读数绑定 d0fdf47f。阻塞：无本次实现或自测阻塞，待组长独立复核。下一步：全部门已通过；仅追加证据提交、推送并核 ls-remote，与 git rev-parse tip/tree 同报组长；回报持久入队后只释放本线程的锁。组长后续接 D 参数、r52 受信来源并做独立集成复核；本路不宣称真实飞书或第三方客户端互操作、D 生产实现、预发/上线已经完成。
