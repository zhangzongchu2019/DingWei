# S-TEST-ADMIN-ACCOUNT-9876（test-only）

基 r36 `c78c66c2fe1ccf2ce25ecca40de3819ef7d22800`；冻结源 `8084f76ab3e4a153fc6e0420040d7c591d5a46bb`，tree `5577e32436f6651c63ae11d59658f83eb921dc08`；分支 `review/coder/admin-account-9876`。非 docs 差集仅 `cmd/workpulse/hosted_accounts_test.go`。

原因：`TestAdminAccountRoutes` 序列化整条管理员列表元数据后扫描裸后四位9876；随机账号ID或其他合法元数据含9876时，误当成凭据泄漏。修正：用具名常量保存完整假凭据，扫描完整假凭据；仍禁止结构化last4字段以及原有sk-片段，并保留属主本人可见后四位、管理员元数据、路由授权、停用引用守卫等断言。登记夹具名称固定为bob-api-9876，使非秘密元数据合法包含9876，确定性防住同类误报；账号ID仍随机，不动生成器或产品数据。

验证为本机非root、Go1.22.2、本地工具链与缓存、离线Go模块、严格内嵌PG（HUB_TEST_PG_STRICT=1、SKIP_OK=0、无外部DSN）、隔离HOME及短TMPDIR。先跑该测试绿；仅撤完整凭据匹配、恢复扫描裸9876，该测试在合法名称字段上以原“管理列表不得含last4／密钥片段”断言确定性红；恢复全部测试源后跑cmd/workpulse全包。原始JSON事件流、stderr、rc和时间／命令见raw及CHECKS.json。未改变产品、helper、接口、授权、迁移或版本；不需要本件重复全仓Go／helper／浏览器门，不把包级门冒充全仓门。没有上线或席位操作。

最终门：定向rc0；撤修rc1、命中指定原断言；恢复后cmd/workpulse全包rc0，164.135秒，Go JSON通过测试事件189项（含子用例），条件跳过2项，完整名单在CHECKS.json。没有其他失败或未关闭问题。
