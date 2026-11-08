# S-R56-MCPGW-CODEX-COMPAT 交审记录

CODEX_THREAD_ID=01a1bacf-9317-7b91-bf86-0a0c88c834f6；to=opus50tc2k.zzc。基线opus50tc2k/r55-integration f2a5db5fa；分支coder56solzzc/r56-mcpgw-codex-compat，依据rulings㊿-补10/0877df37。只本地，不部署、不碰预发/现网或在线席位；最低Codex0.167.0，不为0.145 issuer兼容。完整commit/tree见经ls-remote核对的回执，不自验收。

## 实现范围

- internal/mcpgw/codec2025.go按单个版本头或无头initialize的版本选择独立严格codec，支持initialize、notifications/initialized、ping、tools/list、tools/call。2026原解析器、头/_meta/版本/resultType/方法保留。输入仍1MiB有界，不增加依赖/迁移/版本号。
- 五类2025请求每次走同一Authenticate/Seats，Origin共用guard；新codec不存Mcp-Session-Id或握手令牌快照。四工具仍g.call，现核tenant/scope/audience/AG/过期/吊销。OAuth scope/resource/PKCE/token绑定不动。
- clients.go只对相同127.0.0.1或::1忽略有效端口，其余scheme、escaped path、query及URI组件精确；localhost拒绝，非回环逐字匹配。token仍绑定授权时实际redirect原文。
- codex_client.go生成本域GET /mcp/clients/codex.json，client_id来自配置PublicOrigin，redirect_uris为http://127.0.0.1/callback与http://[::1]/callback，无端口。只对此精确自有URL直接解析同一生成值，适用于本地/预发高端口且不进行私网网络访问；外部resolver的HTTPS443/全公有IP/无跳转防回绑仍保持。不是允许任意本域路径/私网URL。
- PM接入见docs/guides/CODEX-DINGWEI-MCP.md，nginx两环境九类路径见docs/runbooks/MCPGW-CODEX-PATHS.md。包含add/login、外部CIMD与显式本域client ID命令，不配oauth-resource；已有旧callback配置需新add保存稳定路径。无配置包/二进制发布。

## 实测与边界

实际0.167.0二进制＋真实cmd/workpulse wireR53开态＋真实临时PG/Hub SQLite，假飞书HTTP上游；TLS运行期根CA/叶证书仅进入私有子进程信任，不改系统。白名单环境、不读真实凭据或旧HOME，所有监听仅loopback。本单MCP探针无turn/start/推理请求。

**原生正向**：真实add与同配置再次显式login均rc0，使用默认随机端口、不预置固定端口；读取的是产品自有CIMD，没有CIMD resolver替身。真实app-server初始化/列工具得四工具，mcpServer/tool/call list_online成功，isError=false、agents=[]。空清单源于本地没有在线目标，不冒充真实团队。RPC/schema均来自实际二进制；原文在raw/native。

**四刀**：全部只覆盖到私有Go overlay或进程内测试文件，工作树原产品字节不改。

| 单刀 | 目标结果 |
|---|---|
| codec入口去掉 | 真实add/login仍可成功，原生initialize返回400，目录空；native探针rc1红在CODEX_2025_INITIALIZE，Hub原始initialize请求/拒绝响应可核 |
| loopback端口退回逐字匹配 | 真实add的授权URL默认随机端口，不匹配无端口文档，authorize400 invalid_client；rc1红在NATIVE_OAUTH_AUTHORIZE_MUST_SUCCEED |
| 放行localhost元数据 | 同一TestLoopbackPortMatchingAndLocalhostRejection基线rc0，变异rc1红在LOCALHOST_METADATA_MUST_BE_REJECTED |
| 放宽tenant校验 | 既有真实C Callback流程TestCallbackRejectsWrongTenant基线rc0，变异rc1红在wrong tenant was not rejected before issuance |

tenant单刀隔离C层 guard，其issuer为该既有测试的替身，验证在签发前拒绝；不声称删除这一层会同时越过A层的独立tenant防御。另有**全Hub＋真实Codex外组织登录负测**：假飞书上游返回foreign-team，回调400 access_denied、没有native callback或换票，probe rc0（正确拒绝），见raw/foreign。不删其他组织防御来凑“一刀”。

新增单测核：2025生命周期/四工具；五方法每次401 challenge/非法Origin403；未知字段、非法params/notification、跨版本头；初始化后逐次吊销/到期/tenant/AG/capability拒绝及跨组；IPv4/IPv6随机端口与错scheme/IP/escaped path/query/userinfo/fragment、localhost与非回环精确；自有文档不受请求Host污染、精确URL以外仍走外部resolver。既有2026测试全保留。

现有测试改动仅：r53_wiring_test.go精确路由集合新增一项；oauth_test.go Origin负测路径清单新增该文档；没有删除/放宽旧判据。第一个新增测试试跑夹具用了错误wait/ack参数和缺初始化版本，按既有合同修夹具后通过；原文保留，不算有效变异。

## 测试入口

```sh
# 普通用户，所有本地环境应剥SH_/DW_/DINGWEI_及外部DSN
PATH=/tmp/coder-0232-locked-venv/bin:$PATH sh scripts/run-build-tests.sh
go test ./internal/mcpgw -count=1
OPENSPEC_TELEMETRY=0 openspec validate mcpgw-codex-compat --type change --strict
OPENSPEC_TELEMETRY=0 openspec validate mcpgw-codex-compat --type spec --strict
# 终端A，新建私有目录，等ready.json；终端B跑native，最后touch stop
python3 docs/reviews/r56-mcpgw-codex-compat/probe_launcher.py
python3 docs/reviews/r56-mcpgw-codex-compat/native_probe.py
# 单刀codec/ports各开新Hub目录，用INTEROP_RUN_DIR指向它运行同一个native_probe
python3 docs/reviews/r56-mcpgw-codex-compat/probe_launcher.py --run-dir /tmp/new-codec-mutant --mutation codec
INTEROP_RUN_DIR=/tmp/new-codec-mutant python3 docs/reviews/r56-mcpgw-codex-compat/native_probe.py
python3 docs/reviews/r56-mcpgw-codex-compat/unit_mutations.py --output /tmp/new-unit-mutants
```

端口变异改--mutation ports同理。组织外负测启动时INTEROP_FOREIGN_TENANT=1，native_probe加--foreign；只改变假上游身份。--run-dir/INTEROP_RUN_DIR必须一致，所有目录尚不存在；负向探针预期rc1需核目标断言，不能把任意环境错记红。RPC probe只用thread/start及MCP调用，不turn/start。

本单probe_launcher仅提供本单Hub/两种原生变异，不保留前单的boundary入口。复跑必须真实0.167.0；二进制不进Git，来源/版本/SHA单独记录。raw保留UTC、命令、rc和原文，*.stdout可能无损gzip以保留原尾空格。两份SHA清单提供复算。

## 进度与交接

编解码＋回调与本域CIMD主路径已实现，真实正向和四刀验证已完成；当前canonical及相关包结果在RUNS记录。canonical rc0（双profile各1127/1127、27 Python与2 shell全过），mcpgw/extseat rc0，strict新变更/基线及全量均rc0；m8/cmd重跑进行中。相关包首轮m8两项在产品断言前因缺ec40752d6f059aa46b998b2a58965ad4bfc0607c历史Git对象失败，cmd两项因本单私有TMPDIR过长导致Unix socket bind失败；从同一工蜂仓补取后原样重跑，不修改历史用例。保留首轮失败，不当产品红或通过。

OpenSpec新变更mcpgw-codex-compat与现状基线均有独立strict；原冻结2026能力不被全局改写。后续本地结果通过后交中间坐标，再追加最终完整包验证/清单坐标；不自验收、不放行P2b、不开始子Agent主体。所有本地探针Hub/PG已正常停止。

输入追加（㊿补11 cf9235d7）：prodops报Hub同出口取外部ChatGPT CIMD四次403，属于prodops实测，本席未访问节点。本域文档现为主路径；PM说明已明确--oauth-client-id，外部解析保留但不作为本单验收前提。此项追加不改已测试产品字节。

## 中间件之后的追加

89dc872c之后自查补齐HTTPS localhost（含大小写）从旧HTTPS通用分支漏过的边界，按冻结“localhost主机名仍拒绝”处理；新增测试覆盖HTTP/HTTPS，不扩大回环IP例外。最终产品字节下重跑真实native add/login/catalog/list_online、codec/port两刀原生变异、localhost/tenant两刀及mcpgw单测，均得到相应正向/目标红。原中间原文不覆盖，最终新增原文在raw/final。

m8/cmd最终默认包测试均rc0。m8有预置可选验收跳过，完整名称/条件在raw/final/m8-skips.json及默认-v日志；canonical另跑其六项跨runtime本地集成。为排环境问题，曾额外设DW_ACK_V2_E2E_PYTHON开启非默认真实CLI/View验收，因私有环境不具备其View/模型终态条件而失败；该次原文保留在m8-optional-attempt，不作本单通过/变异或网关产品缺陷，也未修改这些用例、伪造凭据或驱动结果。最终包命令恢复白名单洁净环境，不设该可选旗标。

相关包复跑需私有但足够短的TMPDIR（避免Unix socket 108字节限制），并确保历史ec40752 Git对象已fetch；不改测试时限或跳过条件。HTTPS localhost补齐后最终cmd全包215.805s rc0；m8默认229.649s rc0；mcpgw最终4.007s rc0；extseat首轮25.751s rc0且其源码/依赖未被本次兼容变更修改。

最终状态：上述最终产品字节的canonical再次rc0，双profile各1127/1127、27 Python/2 shell；m8/cmd/mcpgw/extseat相关包默认命令rc0（预置可选跳过已列），strict全量27/0，三质量闸rc0；最终native及四刀原文在raw/final，产品SHA在RUNS.final_product_sha256。所有探针Hub/PG已关闭，不部署、不开始子Agent主体；待独立复核。
