# S-R6-IMPL 自测与交接证据

本件是T-04～T-06实施自测，不是T-07独立验收或T-08预发。仓库`wego-ai/dingwei`，只推工蜂，分支`opus50tc2k/r6-impl`。基线`2af469dd6919d8eb6064aa7da0e646f0851becf8`；普通merge规格`bf3ac8a347a780ae0126ef50b21e208888d85db1`的合并点`0622c50ce`。实现commit `fb9318c7868480396c19303dbe28b458737cb9f8`，tree `303cf75ddc2d83fd15c9ed2489f3392c6666b159`；交件最终tip另含本证据/任务交接，不改实现代码。

## 隔离与复跑

普通dev用户；测试构造全新临时HOME、SQLite路径、loopback监听及临时PG。环境仅保留工具路径、LANG/TZ、Go模块/编译缓存；不继承SH_、DINGWEI_、DW_、WP_、FCS_、实际数据库/渠道变量。PATH首位为`/data/ai-dev-data/helper-deps-lock-venv/bin`，Go1.22，Node22.23.1；浏览器仅显式复用`/home/dev/.cache/ms-playwright`已安装测试二进制，不读私人状态。没有真实飞书/生产连接。

复跑命令须在同样清洁环境运行：

```sh
HUB_TEST_PG_STRICT=1 go test -json -count=1 -timeout=15m ./internal/m8 ./internal/store ./internal/identity ./internal/coordinatereview ./internal/specread ./internal/specconfirm ./cmd/workpulse
HUB_TEST_PG_STRICT=1 go test -race ./internal/coordinatereview ./internal/m8 ./internal/store ./internal/identity -run '^TestR6' -count=1 -timeout=240s
bash scripts/run-build-tests.sh
node view-service/coordinate-review-browser-regression.js
python3 scripts/test-r6-mutations.py --output /tmp/r6-target-mutations
OPENSPEC_TELEMETRY=0 openspec validate --all --strict --no-interactive
```

`test-r6-mutations.py`只通过Go overlay与浏览器内存变异，不修改工作树或部署。每个故障必须非0、命中指定断言且不是编译失败；环境/安装失败不计作反证。严格校验固定OpenSpec1.16.0。

## 全场景映射（本地受控自测）

| 场景 | 正格与反格、测试入口 | 定向变异 |
| --- | --- | --- |
| R6-001-S01 | `TestR6ActualReliableEnvelopeCoordinateOnlyAndNoACKCompletion`抓实际WS；正文五字段，无源码/服务凭据/附件/下载能力；浏览器写请求也只含坐标/身份/幂等关联 | 001-envelope、browser-sha |
| R6-001-S02 | `TestR6CoordinateValidationAndIndependentReads`及`TestR6DefaultOffMissingDependenciesAndHTTPBounds`拒main/短SHA/穿越/任意URL/额外source字段/双JSON/过大输入；R5提供方夹具另测跨仓SHA与缺失变更，无派审落库 | 001-sha、001-envelope |
| R6-002-S01 | `TestR6R5IndependentACLRevocationAndForeignCommits`、`TestR6HTTPPinnedReadOpenResultsAndMultiReviewer`：owner/Agent两套绑定与R5核权；Agent专属bearer在原C读取 | 002-agent |
| R6-002-S02 | 上述ACL测试独立子格owner无权、Agent无权、跨owner资格拒绝、缺Agent映射、ACL不可用、跨仓/缺变更；投后撤Agent read同时拒read/open/元数据，无新blob读取；`TestR6AgentBindingsNeverInheritOwner`拒继承 | 002-agent、008-visible |
| R6-003-S01 | `TestR6PinnedReadResultsIdempotencyAndIndependentPeople`、HTTP同名格：71683字节含END，context/spec/changes/timeline顺序，读到的所有SHA固定C，不混HEAD | 003-version、003-read-proof |
| R6-003-S02 | `TestR6UnreadableIsUnableNotPass`：历史/必要文档读失败不fallback；未读不能PASS，UNABLE另记且completed_at为空 | 003-read-proof、007-unknown |
| R6-004-S01 | 核心结果测试：主体来自指派，时刻来自UTC服务端；同operation同事实幂等、冲突拒绝、新operation修订追加；`TestR6ConcurrentResultsAndImmutableHistory`并发八次只一份，禁止改历史 | 004-idempotent |
| R6-004-S02 | 错Agent、错C2分别拒绝；真实WS ACK不标opened/completed或填结果 | 004-idempotent、003-version |
| R6-005-S01 | `TestR6NoticeBudgetsPersistenceSpacingAndFailures`在t0+30/90/150及重复槽调度；`TestR6WorkerPersonalOnlyTrustedContactAndMissingContactFailure`核现有可信私聊身份、出站仅五字段坐标，未打开待办保留 | 005-open-stop、005-spacing |
| R6-005-S02 | `TestR6OpenStopsOnlyOwnUnreadAndEndedStates`独立open/completed/cancelled/superseded；29分钟不外发；缺私聊身份与发送失败记unknown不冒充sent，消耗原槽预算 | 005-open-stop |
| R6-005-S03 | 同本人/他人/GET预取/真实ACK分格；`TestR6ConcurrentOpenAndSendLinearization`与`TestR6AcrossServicesOpenAndSendUseSameDatabaseFence`验证同实例/跨服务数据库屏障，打开后旧作业不得发送 | 005-open-stop、005-database-fence |
| R6-005-S04 | 重复调度、重建Service模拟恢复、第四次、89/149分钟不足间隔、暂停10小时恢复不连发，失败槽不回收；SQLite最多三槽 | 005-spacing、005-open-stop |
| R6-005-S05 | `TestR6DurableEventOwnershipAndIndependentR9`及store事件测试：R9先占同事件拒R6、R6占后拒R9，独立事件分别存在，打开R6不改变R9归属/答复；R9运行时不在本单，未来必须复用共用仲裁 | 005-event-owner |
| R6-006-S01 | `TestR6ComparisonPolicyMaterialExemptionsForcePushAndUnknown`独立requirement/scenario/scope/task四格；旧SHA保持，迟到仍只归C，C2重审提示一次且无新通过 | 006-material |
| R6-006-S02 | 同测试独立unrelated/checkbox/handoff/archive四格不误失效，另有exemption-plus-change夹带任务实质修改必旧；调用R7 A-conservative-exemptions-v2，不自造豁免 | 006-material |
| R6-007-S01 | 对不要求祖先关系的C2直接比较，实质不同仍旧版本；原指派和结果C不迁移，无委员会确认写入 | 006-material、003-version |
| R6-007-S02 | 历史不可读→unknown，跨仓比较拒绝；`TestR6TrackRechecksHistoryWithoutUnboundedDuplicateFacts`同head周期重新核历史、解析失败unknown，相同结论不膨胀历史 | 007-unknown、007-recheck、browser-state |
| R6-008-S01 | 核心/HTTP两Agent独立记录，A结果不填B；浏览器分别显示已审/待审/提醒、当前和原始SHA | 008-visible、browser-state |
| R6-008-S02 | 当前仓库撤权不返回历史；浏览器拒绝响应清空、迟到旧响应不能恢复记录；不产生委员会确认、验收、发布 | 008-visible、browser-html、browser-freshness |

辅助保护：`TestR6MigrationLedgerAndBindingIsolation`核0071完整DDL账补回、删触发器不得补账、映射不串主体与删除/审计；`TestR6PGCurrentAgentOwnerAndPersonalContactLock`使用真实临时PG，观察身份删除等待锁并证明持至出站边界，删除后无旧身份缓存、停用Agent先提交则拒；全路由扫描确认新增三条Agent接口由专属bearer门覆盖。

## 读数与限制

最终读数如下；原始本地日志路径固定为`/tmp/r6-delivery-clean.log`、`/tmp/r6-gate-clean.log`、`/tmp/r6-fenced-race-clean.log`、`/tmp/r6-browser-clean.log`、`/tmp/r6-target-mutations/`与`/tmp/r6-impl-strict.log`。仓内只存聚合读数/跳过条件/目标断言，不提交大型原始日志或私态。

R5、真实GitHub/Agent主体、飞书个人绑定/回执及上线前置仍须T-07独立验收；工蜂仍失败关闭。R9尚无运行时，本单验证共享事件归属契约，不能称其双渠道产品已验收。默认关闭；没有部署、重启、开关激活或生产迁移。

## 最终读数（2026-11-01T15:59:49.995938+00:00）

- 最终严格Go七包rc0，测试源码为上述实现commit；计数含子场景父/子Test事件。

| 包 | PASS | SKIP | 秒 |
| --- | ---: | ---: | ---: |
| m8 | 1224 | 36 | 197.137 |
| store | 523 | 2 | 73.294 |
| identity | 209 | 0 | 113.438 |
| coordinatereview | 24 | 0 | 3.801 |
| specread | 74 | 0 | 0.13 |
| specconfirm | 51 | 0 | 0.005 |
| workpulse | 197 | 1 | 218.566 |

- R6新增格无skip；全包既有跳过逐项及条件见`SKIPS-S-R6-IMPL.md`，无PG不可用跳过。
- `scripts/run-build-tests.sh` rc0：helper claude/codex两profile各1096（1087+9），清单90件；Python27/shell2全部完成。新增工具总数26、传输脱敏夹具和main guard的首轮失败已修正后完整重跑通过，原始首轮日志`/tmp/r6-gate-initial-failed.log`保留。
- 文件屏障阶段R6 race四包rc0：coordinatereview95.021s、m8 69.809s、store9.616s、identity8.165s；最终GET同屏障接线又经最终严格整包验证。
- 浏览器正格11/11；18个目标变异全为预期断言红、runner rc0，见`MUTATIONS-S-R6-IMPL.json`。首次环境/断言定位失败不计入有效反证。
- OpenSpec1.16.0/Node22.23.1/telemetry0：单变更及全仓strict rc0，全仓12/12；增量spec与bf3ac8a3逐字节一致。
- 只交实施和证据，不自验收；总开关默认关，工蜂API仍不可用，T-07/T-08均未勾选。
