# S-R62-EXTSEAT-TTL 作者交件

状态：原TTL实施与作者门完成；17:33Z追加核项已补常驻PG测试，另发现既有已投影队列边界，先报组长待裁定。不是发布GO。

补件入口：[组到期但消息未到期的分阶段行为](group-expiry-followup/REPORT.md)。产品实现字节仍2fd，新增测试锚0b9；原全门不冒称新源重跑，增量测试及canonical独立记录。

## 锚与授权
冻结产品/测试源 `2fd6aebacbb3dae3ac3fa82d84968367552151ca`，tree `d0cabfc1cacf98fed570e633084d3298d1549263`，parent `9576cf5600b4d6a51938376fb75fdb266d50ce27`；基main `206d41b26dab646c0e50a111eed5a45ccf7c72f6`。设计 `124d24ee3818762f977920d5a015e4dfdca80663`、组长2026-11-18T16:17:02Z单独派单第1片。最终文档tip以交审信为准，不与冻结源混称。

## 实施与边界
生产仅internal/extshared/extshared.go的GroupTTL从72h、MessageTTL从24h改为168h；TokenTTL336h及其它常量不变。既有消费者仍按每行expires_at判断，now>=expiry即到期。不新增迁移、不回填旧expiry、不改VERSION、依赖、Hub协议与鉴权。MessageTTL的既有expiry亦传给FeishuCopyEvent，故既有抄送重试截止随新消息变7天；没有实现摘要、偏好或其它r62切片。

原2fd共四个产品/测试路径，见product-manifest.json；0b9补测另加一个常驻测试文件，见group-expiry-followup/test-manifest.json。真实PG回归使用生产tempgroup+extseat+identity服务，不用stub Groups；投影边界使用记录调用的测试Projector。固定时钟以PG精度一微秒检验前/恰/后，不等待现实七天。

## Spec与测试映射
- R62TTL-001/S001：TestR62SevenDayGroupBoundaryPG三格，创建168h、请求即时拒绝与扫描一致；Token仍336h。
- R62TTL-002/S002：TestR62SevenDayMessageBoundaryIndependentOfRenewalsPG三格，inbox/outbox/send_key168h；先合法续组/续token仍不能续旧消息，实际Wait/Ack及ProjectOne边界。
- R62TTL-002/S003：TestR62PersistedLegacyExpiryNotRewrittenPG，隔离库预置旧3天AG与24h消息，服务对象重建、鉴权/读取/扫描均不回填；新受理消息才用168h。正常新发送可续仍有效旧AG，与批量回改区分。
- R62TTL-003/S004：TestR62PollingAndIdempotentEventsDoNotRenewGroupPG，Check/Wait/WaitReply、重复ACK、同ID重试不续组；首次ACK/新Send续168h。
- R62TTL-003/S005：TestR62FourteenDayTokenBoundaryPG三格，合法发送保组到day19，令牌day14±1us独立拒绝/续期。

## 旧夹具与前驱尝试如实保留
原TestCorePG/MessageExpiryAndRevokedWait隐含MessageTTL<GroupTTL；两者改为同168h后，组先到期遮挡原消息与撤权断言。9576源原夹具单格rc1；仅在消息截止前1h用合法Send续组，再前进1h，原断言全部保留，新夹具单格rc0。无生产Receipt/Wait修改。前驱9576canonical完整rc0，旧整包刚开始即因确认夹具问题主动停止（143、未完成），原始文件单列attempt-9576，不拼接为正式门。随后在2fd新源从变异到全部门重新运行。

## 正式门
命令、UTC、rc、原始SHA逐项见evidence/runs.json；白名单环境见evidence/env.json，Go1.22.2、锁定Python3.12.3、普通用户、数据盘独立HOME/TMPDIR，无继承在线SH_/DINGWEI_。HUB_TEST_PG_STRICT=1，真实临时PG；从本次runner后代postmaster实读版本PostgreSQL18.3，见evidence/pg-runtime.json（不接现网）。

- canonical：SOURCE_COMMIT=2fd6aebacbb3dae3ac3fa82d84968367552151ca ./build.sh --test-only，rc0；BUILD_TESTS_OK helper_profiles=2 python=27 shell=2；Claude/Codex各1270/1270，inventory97。
- extshared/extseat/tempgroup/m8整包：-count=3 -p1 -timeout90m -v，rc0；顶层PASS 3081，含各层PASS 5250，FAIL 0，顶层SKIP 30、含各层SKIP 84，interrupted(9) 0行。逐名跳过及条件见SKIPS.md，未把SKIP记为绿。
- OpenSpec1.16.0 validate --all --strict：42 passed/0 failed、rc0；最终纯文档补记后另留strict记录。

## 删保护两刀
GroupTTL单独退3天、MessageTTL单独退24h，分别compile_rc0/test_rc1，TestTTLConstants及对应真实PG到期业务断言红；无编译失败/panic。修改前后文本、日志SHA、原始/变异/恢复SHA见evidence/mutations.json；恢复后基线rc0，工作树clean。仅计最终源两刀，不与前驱样本累计。未宣称其覆盖其它不变授权逻辑。

## 范围核验与回退
cmd/tools/scripts/deploy/view-service、三条迁移目录、go.mod/go.sum/VERSION与main逐项对象相同，见structure.json。无迁移，因此旧行继续按已存expiry；回退常量只影响后续新受理/合法续期，不缩短已存7天期限。无部署、构建发布件、main推送或r61集成写入。

## 下一步
qwen以固定2fd产品与交审文档tip只读复核；组长验收后另定集成。作者不勾验收。原始日志经gzip无损压缩，raw-log-manifest.json保存压缩前后SHA；VERIFY.py同时核Git对象、当前产品字节、原始日志SHA与变异判据。
