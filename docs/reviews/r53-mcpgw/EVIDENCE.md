# S-R53-MCPGW-IMPL 实施交件证据

执行者 coder56solzzc；真实 CODEX_THREAD_ID=01a1b01a-3bc9-7003-998c-53b56b27c2ec。B路实现及定向验证完成，**不自验收、不部署、不重启**。原样 canonical／迁移包的0107棘轮失败按组长16:16:37Z裁定保留，集成时由组长统一调整0105/0106/0107；本分支未改这些测试。

## 源码冻结与验证对应关系

仓库 git@git.code.tencent.com:wego-ai/dingwei.git；分支 coder56solzzc/r53-mcpgw。新分支直接父为144b215e32e718553d9fef2e79c83ece164f6514。

最终源码冻结后实际执行 `git rev-parse HEAD HEAD^{tree}`：

```text
b82d221a380ee23dcc4a52cee2cba8d2b1281b89
1e229d605eeec4a2b200dac714bba7ca5ff9960d
```

初始中间锚2f1a65298b84f2eb1282ccfd8ded29340613fbd5，tree9054ef2671e3d484ee450c65599bc87b9bd1d9a6，已实际push且ls-remote一致，组长16:22:23Z已独立fetch/试合。b82只追加新版MCP应用错误码、整数ID/Accept边界与其测试、文书；0107、A/C/D/helper和迁移/claim测试不变。后续交件提交只追加本目录证据和OpenSpec交接，产品源码与b82逐字相同；最终交件tip/tree及ls-remote由DingWei回执提供，避免文件自引用自身提交号。

[raw/SOURCE-BINDING.json](raw/SOURCE-BINDING.json)钉住所有B源码/测试、接口文书、cmd注册及0107的SHA256；[RAW-MANIFEST.json](RAW-MANIFEST.json)列实际原始日志的字节数/SHA256。均走Git交换，没有文件服务源码上传、二进制上传或运行环境打包。

## 范围与实现

接口详见 [internal/mcpgw/INTERFACE.md](../../../internal/mcpgw/INTERFACE.md)。实现范围为：

1. 2026-07-28无状态Streamable HTTP的server/discover、tools/list、四工具tools/call，逐请求_meta/能力/版本/头一致性与resultType；没有initialize、会话、GET流、订阅或WSS。
2. REST同四工具，共用认证/分发/参数边界，不增加执行、文件或管理能力；WaitMax采用extshared常量，ACK仅received，不把返回当ACK。
3. Hub OAuth授权服务器的两类metadata、CIMD、PKCE S256、精确client/redirect/resource/完整四scope绑定、一次性code及兑换时A现核；真实C处理器经extwire签发，B仅加密暂存，回跳不含Bearer。
4. 所有本包路径统一Origin接口；缺省只信PublicOrigin。cmd依赖显式注入，密钥读取失败/长度权限不符只关闭本路并打印一行日志，不影响已有Hub/helper。
5. extshared错误集中映射，业务码1001/1003/1008/1009/1029置于JSON-RPC保留区外；仅标准头/版本错误使用MCP保留码。

0107来自组长2026-11-06T16:13:02Z分配，唯一mcpgw_oauth表；每行expiry+索引，每次Put清本resource过期行；DELETE RETURNING原子消费code。AES-256-GCM密钥来自DW_MCPGW_OAUTH_KEY_FILE指向的仓外32字节私有文件，测试密钥crypto/rand生成；无运行时DDL。MVP不支持轮换，换钥使在途登录失效。C StateStore适配留在B包，不改C签名；可选持久适配不撤回㊹-补14的单进程MemoryStateStore许可。

`git diff 144b215e..b82 -- internal/extseat internal/extshared internal/extwire internal/feishulogin internal/tempgroup tools/sessionhelper`为零。main统一A/D装配、r52受信来源适配及extwire.CheckSchema追加0107均归组长；本路提供注册函数而未擅自打开生产入口。

## 验证与UTC

测试均普通用户运行。真实Hub/helper测试剥掉继承SH_*/DINGWEI_*，临时HOME/DB/监听由既有隔离套件管理；PG使用pgtest临时实例，HUB_TEST_PG_STRICT=1，不使用生产DSN。TMPDIR均为/tmp或短的/tmp/b*。

| 门 | 对应源码锚 | 结果 | 原始日志 |
|---|---|---|---|
| go test -json -count=1 ./internal/mcpgw ./internal/extshared ./internal/extwire ./internal/feishulogin | b82 | rc0，189 RUN/PASS，0 SKIP/FAIL；mcpgw119项 | raw/packages-b82.log |
| go test -race -count=3 -timeout=90m ./internal/mcpgw | b82，16:24:46Z–16:25:04Z | rc0，357 RUN/PASS；真PG每轮执行，0 SKIP | raw/race-protocol.log/json |
| cmd/workpulse两类MCPGateway注册用例 | b82 | rc0，5 RUN/PASS含缺钥/短钥/权限宽，helper路由不变 | raw/cmd-b82.log |
| go vet ./internal/mcpgw ./cmd/workpulse；go build ./...；gofmt/diff-check | b82 | rc0，无新增依赖/版本改号，无发版制品 | raw/vet-b82.log、raw/build-b82.log |
| m8 -run TestExternal（普通/三轮race） | 首轮同一基线B实现；m8子树与2f1/b82逐字相同，另b82普通复跑 | rc0，普通7项、race21项，0 SKIP | raw/m8-directed.log、raw/race-m8-directed.log、raw/m8-b82.log |
| OpenSpec单变更/全仓 --strict（锁定1.16.0） | b82及证据交接后复核 | rc0，全仓24项 | raw/openspec-change-locked.log、raw/openspec-all-locked.log |
| canonical原样，系统Python | 早期探索轮 | rc1，四个helper模块缺依赖导致收集失败；原件保留，未假称产品FAIL | raw/canonical.log/json |
| canonical原样，锁定venv | 最终b82 | rc1，仅claim迁移期望不含0107，五子项同一失败；不改任何测试期望（16:25:59Z–16:34:44Z） | raw/canonical-b82-raw.log/json |
| canonical，仅临时改0107测试期望 | 2f1源码；16:20:24Z–16:29:21Z | rc0，BUILD_TESTS_OK helper_profiles=2 python=27 shell=2；两profile各1097实际=期望，0用例SKIP；真实Hub/claim五分支全过 | raw/canonical-expectations-only.log/json |
| migrate原样/临时期望调整 | 2f1，migrate子树与b82相同 | 原样rc1，42 RUN/33 PASS/9 FAIL；临时仅期望调整后rc0，42 PASS，0 SKIP | raw/migrate-raw.log/json、raw/migrate-expectations-final.log |
| 仓储边界附加门 | b82与精确父144b215e对照 | 两者同为rc1，10处既有建库＋1处句柄逃逸，逐项位置相同，本路无新增；不放宽棘轮 | raw/boundary*、raw/boundary-comparison.json |

原样canonical唯一失败族是TestDeviceClaimAcrossActualClientAndPostgres的new/reuse/lost_ack/takeover/cancel_then_new，均为claim_client_integration_test.go:75：`production Hub migration ledger did not initialize expected identity/claim schema`。其余真实Hub、MCP、project_context、blocker联测通过。此项标 **待集成改棘轮（0107）**，不是隐藏成PASS。

真实PG的TestPGStore包括16路完整HTTP code兑换只成功1路、重复兑换失败、整点expiry拒绝、过期清理/索引、resource隔离、新连接重开读取；快速SQLite夹具也读取同一0107 SQL，未另造生产schema。租户与AG真实策略仍由A/D负责，B工具测试使用A边界替身注入吊销/到期/越权/跨AG错误，验证两协议同权与错误映射，不冒称已完成A/B/C/D真链路验收。

## 变异

脚本 [mutations.py](mutations.py) 用Go overlay逐刀改生产保护，原源码不动；每刀仅运行对应原判据，要求真实测试断言失败，编译失败不计杀死。[raw/mutations/RESULTS.json](raw/mutations/RESULTS.json)含命令、UTC、原源码SHA、日志SHA与命中标记。

| 刀 | 原判据 | 结果 |
|---|---|---|
| 去掉PKCE比较 | TestOAuthPKCEAndBindings/pkce | rc1，code_verifier not bound |
| code从Take改为Get可重放 | TestOAuthCodeReplay | rc1，authorization code reusable |
| REST额外开放execute | TestDiscoveryWithoutInitializeAndNoExtraCapabilities | rc1，extra capability execute allowed |
| 去掉Origin守卫 | TestOAuthMetadataAndOrigin | rc1，Origin guard absent |
| 去掉头/正文一致性 | TestRequestMetadataAndOriginGuards/method_mismatch | rc1，guard missing |
| 放行旧协议版本 | TestRequestMetadataAndOriginGuards | rc1，bad metadata response |
| 去掉WaitMax上限 | TestArgumentsAndCrossAG | rc1，25.01秒错误返回200 |
| 跳过Authenticate | TestToolBoundaryMatrix/list_online/mcp/revoked | rc1，吊销错误返回200 |
| 业务错误使用MCP保留码 | TestApplicationErrorsAvoidMCPReservedRange | rc1，application error uses reserved MCP code |
| 去掉code到期检查 | TestOAuthExpiryAndRevocationBeforeExchange/code_expired | rc1，expired/revoked grant accepted |

十刀均语义红，原实现普通包与三轮race为绿；未把变异编译失败或空测试当覆盖。

## 0107期望对照（不提交临时测试补丁）

组长16:16:37Z明确要求B不改正式测试期望，允许一次性树仅同步0107的期望后对照。临时树/tmp/r53-mcpgw-expectations，父2f1；只动internal/store/migrate/{domains,ledger,migrate}_test.go与internal/m8/claim_client_integration_test.go。除清单追加0107，还须把新表对应的数量、轨A schema参照和已知新表过滤同步；否则双轨/保表断言仍会正确报mcpgw_oauth额外一表。未改测试执行路径/失败条件、生产实现或迁移SQL。最终此树migrate整包42项、canonical两profile全套均rc0。

[MIGRATION-FAILURES.json](MIGRATION-FAILURES.json)逐条保存原始失败用例全名、行号及失败行原文；完整原始日志见raw/migrate-raw.log。下列git diff --unified=0原文仅作为对照证据文字，不应用到本分支、不提交测试文件、不推临时树：

```diff
diff --git a/internal/m8/claim_client_integration_test.go b/internal/m8/claim_client_integration_test.go
index baf458eeb..ae5004fca 100644
--- a/internal/m8/claim_client_integration_test.go
+++ b/internal/m8/claim_client_integration_test.go
@@ -74 +74 @@ func claimClientIntegration(t *testing.T, python, mode, backend string) {
-		if err != nil || strings.Join(report.Applied, ",") != "0001_core_baseline.sql,0100_identity_baseline.sql,0101_group_communication.sql,0102_device_claim.sql,0103_agent_last_source_r31.sql,0104_np_session_created_at.sql,0105_extseat.sql,0200_hosted_r30_baseline.sql,0201_hosted_node_access_r31.sql,0300_console_transport_slot.sql" {
+		if err != nil || strings.Join(report.Applied, ",") != "0001_core_baseline.sql,0100_identity_baseline.sql,0101_group_communication.sql,0102_device_claim.sql,0103_agent_last_source_r31.sql,0104_np_session_created_at.sql,0105_extseat.sql,0107_mcpgw_oauth.sql,0200_hosted_r30_baseline.sql,0201_hosted_node_access_r31.sql,0300_console_transport_slot.sql" {
diff --git a/internal/store/migrate/domains_test.go b/internal/store/migrate/domains_test.go
index 71a9024ef..a56cad9c5 100644
--- a/internal/store/migrate/domains_test.go
+++ b/internal/store/migrate/domains_test.go
@@ -117 +117 @@ func TestLedgerInDomains_PartitionsRealLedger(t *testing.T) {
-	if strings.Join(hub, ",") != coreEntry+","+frozenIdentityEntry+",0101_group_communication.sql,0102_device_claim.sql,0103_agent_last_source_r31.sql,0104_np_session_created_at.sql,0105_extseat.sql,0200_hosted_r30_baseline.sql,0201_hosted_node_access_r31.sql,0300_console_transport_slot.sql" {
+	if strings.Join(hub, ",") != coreEntry+","+frozenIdentityEntry+",0101_group_communication.sql,0102_device_claim.sql,0103_agent_last_source_r31.sql,0104_np_session_created_at.sql,0105_extseat.sql,0107_mcpgw_oauth.sql,0200_hosted_r30_baseline.sql,0201_hosted_node_access_r31.sql,0300_console_transport_slot.sql" {
@@ -218 +218 @@ func TestMigrate_DomainScopingOnRealLedger(t *testing.T) {
-		if strings.Join(rep.Applied, ",") != coreEntry+","+frozenIdentityEntry+",0101_group_communication.sql,0102_device_claim.sql,0103_agent_last_source_r31.sql,0104_np_session_created_at.sql,0105_extseat.sql,0200_hosted_r30_baseline.sql,0201_hosted_node_access_r31.sql,0300_console_transport_slot.sql" {
+		if strings.Join(rep.Applied, ",") != coreEntry+","+frozenIdentityEntry+",0101_group_communication.sql,0102_device_claim.sql,0103_agent_last_source_r31.sql,0104_np_session_created_at.sql,0105_extseat.sql,0107_mcpgw_oauth.sql,0200_hosted_r30_baseline.sql,0201_hosted_node_access_r31.sql,0300_console_transport_slot.sql" {
@@ -232 +232 @@ func TestMigrate_DomainScopingOnRealLedger(t *testing.T) {
-		if len(rows) != 10 || rows["0103_agent_last_source_r31.sql"].digest == "" || rows["0200_hosted_r30_baseline.sql"].digest == "" || rows["0201_hosted_node_access_r31.sql"].digest == "" || rows["0102_device_claim.sql"].digest == "" || rows[coreEntry].digest == "" || rows[frozenIdentityEntry].digest == "" || rows["0101_group_communication.sql"].digest == "" {
+		if len(rows) != 11 || rows["0103_agent_last_source_r31.sql"].digest == "" || rows["0200_hosted_r30_baseline.sql"].digest == "" || rows["0201_hosted_node_access_r31.sql"].digest == "" || rows["0102_device_claim.sql"].digest == "" || rows[coreEntry].digest == "" || rows[frozenIdentityEntry].digest == "" || rows["0101_group_communication.sql"].digest == "" {
@@ -293 +293 @@ func TestMigrate_DomainScopingOnRealLedger(t *testing.T) {
-		if strings.Join(rep.Applied, ",") != "0101_group_communication.sql,0102_device_claim.sql,0103_agent_last_source_r31.sql,0104_np_session_created_at.sql,0105_extseat.sql,0200_hosted_r30_baseline.sql,0201_hosted_node_access_r31.sql,0300_console_transport_slot.sql" {
+		if strings.Join(rep.Applied, ",") != "0101_group_communication.sql,0102_device_claim.sql,0103_agent_last_source_r31.sql,0104_np_session_created_at.sql,0105_extseat.sql,0107_mcpgw_oauth.sql,0200_hosted_r30_baseline.sql,0201_hosted_node_access_r31.sql,0300_console_transport_slot.sql" {
@@ -303 +303 @@ func TestMigrate_DomainScopingOnRealLedger(t *testing.T) {
-			if table != "np_device_claim" && table != "np_external_principal" && table != "np_extseat" && table != "np_extseat_token" && table != "np_extseat_inbox" && table != "np_extseat_outbox" && table != "np_extseat_send_key" && table != "console_transport_slot" && !strings.HasPrefix(table, "g4_") {
+			if table != "mcpgw_oauth" && table != "np_device_claim" && table != "np_external_principal" && table != "np_extseat" && table != "np_extseat_token" && table != "np_extseat_inbox" && table != "np_extseat_outbox" && table != "np_extseat_send_key" && table != "console_transport_slot" && !strings.HasPrefix(table, "g4_") {
@@ -307 +307 @@ func TestMigrate_DomainScopingOnRealLedger(t *testing.T) {
-		if len(after) != len(before)+22 || strings.Join(before, ",") != strings.Join(filtered, ",") {
+		if len(after) != len(before)+23 || strings.Join(before, ",") != strings.Join(filtered, ",") {
@@ -311 +311 @@ func TestMigrate_DomainScopingOnRealLedger(t *testing.T) {
-		if len(afterRows) != len(beforeRows)+8 {
+		if len(afterRows) != len(beforeRows)+9 {
diff --git a/internal/store/migrate/ledger_test.go b/internal/store/migrate/ledger_test.go
index e6da90be3..614af2594 100644
--- a/internal/store/migrate/ledger_test.go
+++ b/internal/store/migrate/ledger_test.go
@@ -55 +55 @@ func TestMigrate_RealLedgerSatisfiesIdentityCheckSchema(t *testing.T) {
-	if strings.Join(rep.Applied, ",") != "0001_core_baseline.sql,0100_identity_baseline.sql,0101_group_communication.sql,0102_device_claim.sql,0103_agent_last_source_r31.sql,0104_np_session_created_at.sql,0105_extseat.sql,0200_hosted_r30_baseline.sql,0201_hosted_node_access_r31.sql,0300_console_transport_slot.sql" {
+	if strings.Join(rep.Applied, ",") != "0001_core_baseline.sql,0100_identity_baseline.sql,0101_group_communication.sql,0102_device_claim.sql,0103_agent_last_source_r31.sql,0104_np_session_created_at.sql,0105_extseat.sql,0107_mcpgw_oauth.sql,0200_hosted_r30_baseline.sql,0201_hosted_node_access_r31.sql,0300_console_transport_slot.sql" {
@@ -87 +87 @@ func TestMigrate_RealLedgerSatisfiesIdentityCheckSchema(t *testing.T) {
-	if len(rep2.Applied) != 0 || len(rep2.Skipped) != 10 {
+	if len(rep2.Applied) != 0 || len(rep2.Skipped) != 11 {
@@ -202 +202 @@ func TestLedgerShapeMatchesDomainDDL(t *testing.T) {
-		if entry.Name == "0105_extseat.sql" {
+		if entry.Name == "0105_extseat.sql" || entry.Name == "0107_mcpgw_oauth.sql" {
@@ -211,2 +211,2 @@ func TestLedgerShapeMatchesDomainDDL(t *testing.T) {
-	if len(aTables) != 35 {
-		t.Fatalf("轨 A 建出 %d 张表（%v），要 35 张＝外部席位 6＋hosted 14＋身份域 9（G3 终版 800f24d8：原 6 张＋编组两张＋设备认领一张）"+
+	if len(aTables) != 36 {
+		t.Fatalf("轨 A 建出 %d 张表（%v），要 36 张＝MCP OAuth 1＋外部席位 6＋hosted 14＋身份域 9（G3 终版 800f24d8：原 6 张＋编组两张＋设备认领一张）"+
diff --git a/internal/store/migrate/migrate_test.go b/internal/store/migrate/migrate_test.go
index 53a1ef06f..f39c09534 100644
--- a/internal/store/migrate/migrate_test.go
+++ b/internal/store/migrate/migrate_test.go
@@ -148 +148 @@ func TestLedger_EmbeddedEntries(t *testing.T) {
-	want := []string{"0001_core_baseline.sql", "0100_identity_baseline.sql", "0101_group_communication.sql", "0102_device_claim.sql", "0103_agent_last_source_r31.sql", "0104_np_session_created_at.sql", "0105_extseat.sql", "0200_hosted_r30_baseline.sql", "0201_hosted_node_access_r31.sql", "0300_console_transport_slot.sql", "0400_fanout_baseline.sql"}
+	want := []string{"0001_core_baseline.sql", "0100_identity_baseline.sql", "0101_group_communication.sql", "0102_device_claim.sql", "0103_agent_last_source_r31.sql", "0104_np_session_created_at.sql", "0105_extseat.sql", "0107_mcpgw_oauth.sql", "0200_hosted_r30_baseline.sql", "0201_hosted_node_access_r31.sql", "0300_console_transport_slot.sql", "0400_fanout_baseline.sql"}
@@ -242 +242 @@ func TestLedger_RegisteredEntriesSortWithEmbedded(t *testing.T) {
-		"0101_group_communication.sql", "0102_device_claim.sql", "0103_agent_last_source_r31.sql", "0104_np_session_created_at.sql", "0105_extseat.sql", "0200_hosted_r30_baseline.sql", "0200_hosted_test.sql", "0201_hosted_node_access_r31.sql", "0300_console_transport_slot.sql", "0400_fanout_baseline.sql", "0900_cross_test.sql"}
+		"0101_group_communication.sql", "0102_device_claim.sql", "0103_agent_last_source_r31.sql", "0104_np_session_created_at.sql", "0105_extseat.sql", "0107_mcpgw_oauth.sql", "0200_hosted_r30_baseline.sql", "0200_hosted_test.sql", "0201_hosted_node_access_r31.sql", "0300_console_transport_slot.sql", "0400_fanout_baseline.sql", "0900_cross_test.sql"}
@@ -608 +608 @@ func TestMigrate_ConcurrentRunnersAllSucceed(t *testing.T) {
-	if len(entries) != 11 {
+	if len(entries) != 12 {
@@ -652 +652 @@ func TestMigrate_ConcurrentRunnersAllSucceed(t *testing.T) {
-	if len(rows) != 11 {
+	if len(rows) != 12 {
@@ -668 +668 @@ func TestMigrate_ConcurrentRunnersAllSucceed(t *testing.T) {
-	if len(rep.Applied) != 0 || len(rep.Skipped) != 11 {
+	if len(rep.Applied) != 0 || len(rep.Skipped) != 12 {
```

## 交接与限制

组长对2f1的只读试合：opus50tc2k/r53-integration d47ffa48，代码零冲突、两份OpenSpec文书取并集；其独立go build/vet/四包/真PG/注册用例通过（2026-11-06T16:22:23Z报告）。此项只记录对方报告，不替代b82的最终独立复核。

本路假飞书和手写HTTP协议客户验证未外推为真实飞书帐号或第三方MCP SDK互操作；CIMD正例用注入HTTP transport验证协议/文档校验，默认真实resolver验证私网/保留地址拒绝，不冒称公网CIMD实站互操作。MVP仅公开PKCE客户端，精确HTTPS或IP-loopback redirect，默认CIMD连接443，无DCR/refresh/confidential client/缓存。A/C/D主装配、完整跨路E2E、真实登录及发布均留待集成与独立验收，不自行关T-04/T-08/T-10跨路总项。最终回报持久入队且Git远端核验完成后按owner校验释放写锁。
