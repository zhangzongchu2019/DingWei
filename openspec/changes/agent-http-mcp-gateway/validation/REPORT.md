# S-R53-AGENT-GATEWAY-PROPOSAL-REV 文档验证

## 范围与坐标

- 执行者：coder56solzzc，会话01a19ca0-0622-7743-a995-767c05e49c78。
- 分支：coder56solzzc/r53-agent-gateway-proposal-rev；直接父基线 f95fa0b3a4a6e693d21c8de4b052e3d3a7424293。
- 代码只读基线：f7f31fca1d1f1468285138578c3f6fafacfb5537；需求裁定读取2577d91a、4accf167、5ed6d2ae、51133ad3。
- 限定：仅本变更目录的Markdown。本次没有产品实现、数据库迁移、真实飞书/扫码、OAuth互操作、产品变异或独立验收。源码/证据一并在此文档分支走Git交付。

## 九点追溯

| 派单点 | 文档与固定需求 |
|---|---|
| 1 网页飞书扫码＋本团队租户＋可吊销令牌 | design第1节；GW-002-S03、S07、GW-009 |
| 2 英文/拼音归属名与open_id／union_id唯一身份 | design第1节；GW-002-S04、S05 |
| 3 AG持久全局序号、双成员、清理通知与在途 | design第2、3、5节；GW-008-S03、S05、S07、GW-006-S03 |
| 4 短名和组合名、无需用户查重理由 | design第2节；GW-008-S04；context普通名称校验兼容边界 |
| 5 24小时/90秒/25秒、显式全名、标准OAuth | design第1、3、4节；GW-003～009 |
| 6 Hub模块/表/代码坐标、未核实项 | context影响清单及r51续期复用一节；design第7节 |
| 7 Console相关AG/属性/授权解散与变异 | design第6节；GW-010-S03～S06；T-11、T-12 |
| 8 14天/3天滑动、节流时钟、重启边界 | design第3节；GW-002-S06～S08、GW-008-S05～S07 |
| 9 无状态协议、ACK重投、订阅取舍与私有WSS分期 | design第4a节；GW-003-S03～S05、GW-006-S04～S05；T-13；context官方复核 |

保留十个原Requirement编号，当前41个计划场景；没有增加第11条需求。T-02已同步本轮裁定，但仅轮询不续编组明确保留PM-GW-01“组长解释，待业主确认”，没有冒充业主已定。序号分配/续期聚合/解散outbox等为技术建议，未写成现有运行事实。

## 工具与验证原文

OpenSpec钉版1.16.0，Node v22.23.1，OPENSPEC_TELEMETRY=0；执行目录为本分支独立工作树。严格校验原文见下方最终记录。

## 构建门

仓库AGENTS要求scripts/run-build-tests.sh，本轮用现有构建venv、独立HOME与短TMPDIR、剥继承SH_/DINGWEI_/DW_/FCS_变量执行；不启动接入真实平台的实例。本轮rc=0；claude、codex两配置均1097/1097，Python27组、shell2组全部完成。以下保留时间与原文摘要；完整本地原始日志未上传文件服务。


### Canonical 原文摘要

开始UTC：2026-11-06T03:19:53.045134+00:00；结束UTC：2026-11-06T03:29:50.560170+00:00；rc=0。

命令：`bash scripts/run-build-tests.sh`；构建venv `/tmp/coder-0232-locked-venv`；隔离根 /tmp/g53a62q_dnn。

```text
HELPER_INVENTORY_OK files=90
HELPER_GATE_RESULT {"actual": 1097, "actual_SH_DRIVER": "claude", "expected": 1097, "files": 90, "groups": [1088, 9], "profile": "claude", "rc": 0}
HELPER_GATE_RESULT {"actual": 1097, "actual_SH_DRIVER": "codex", "expected": 1097, "files": 90, "groups": [1088, 9], "profile": "codex", "rc": 0}
TEST shell deploy/test-view-service-release.sh
TEST shell scripts/test-desktop-hub-roundtrip.sh
BUILD_TESTS_OK helper_profiles=2 python=27 shell=2
```

完整本地日志：/tmp/r53gw-results-01a19ca0/canonical.log，SHA-256 3ddc3a0900b0e21b342e5301e74c456f049a9cf6fa1c9c7874cc20d44ff9d8ad。其路径只作本机原件定位；跨席复核使用本分支脚本与文档重跑，不经文件服务传源码。

SKIP：无unittest或Go用例SKIP；日志中的bridge resubscribe skipped是事件过滤计数，不是跳过测试。

### 第9点追加前单变更 strict 原文

开始UTC：2026-11-06T03:31:08.897237+00:00；结束UTC：2026-11-06T03:31:09.095106+00:00；rc=0。

命令：`OPENSPEC_TELEMETRY=0 openspec validate agent-http-mcp-gateway --strict --no-interactive`

```text
Change 'agent-http-mcp-gateway' is valid
```

### 第9点追加前全仓 strict 原文

开始UTC：2026-11-06T03:31:09.095157+00:00；结束UTC：2026-11-06T03:31:09.381460+00:00；rc=0。

命令：`OPENSPEC_TELEMETRY=0 openspec validate --all --strict --no-interactive`

```text
✓ change/agent-http-mcp-gateway
✓ spec/change-lifecycle-evidence
✓ spec/change-risk-reminders
✓ spec/console-coordinate-review
✓ spec/console-spec-reading
✓ spec/console-version-confirmation
✓ change/feishu-agent-sticky-default
✓ change/helper-first-offer-idempotence
✓ change/hub-graceful-shutdown
✓ spec/hub-lifecycle
✓ spec/legacy-console-spec
✓ spec/pm-decision-request
✓ change/r1-identify-high-risk-changes
✓ change/r10-task-dispatch-lock
✓ change/r11-freeze-legacy-console-spec
✓ change/r15-session-history-persist
✓ change/r5-binding-admin-page
✓ change/r5-render-openspec-readonly
✓ change/r51-console-timeline-order
✓ change/r6-coordinate-review
✓ change/r7-confirm-immutable-version
✓ change/r8-evidence-driven-change-lifecycle
✓ change/r9-pm-decision-request
✓ spec/task-dispatch-lock
Totals: 24 passed, 0 failed (24 items)
```

## 提交前第9点追加

03:21:04Z补令在提交前delivery_inbox只读查收时发现；已读51133ad3裁定原文并同步，回执request_id 94cdbee93d41。官方RC博客为历史页，本次官方release标stable；两者在context分别引用，并保留“定稿状态开发前须再核”。canonical测试覆盖的产品/脚本与最终文档分支一致（产品零改动），不因只改文档重复整跑；最终strict在第9点写入后重跑，结果在后续最终记录。

### 最终单变更 strict 原文（已含第9点）

开始UTC：2026-11-06T03:34:54.591316+00:00；结束UTC：2026-11-06T03:34:54.790375+00:00；rc=0。

命令：`OPENSPEC_TELEMETRY=0 openspec validate agent-http-mcp-gateway --strict --no-interactive`

```text
Change 'agent-http-mcp-gateway' is valid
```

### 最终全仓 strict 原文（已含第9点）

开始UTC：2026-11-06T03:34:54.790417+00:00；结束UTC：2026-11-06T03:34:55.078692+00:00；rc=0。

命令：`OPENSPEC_TELEMETRY=0 openspec validate --all --strict --no-interactive`

```text
✓ change/agent-http-mcp-gateway
✓ spec/change-lifecycle-evidence
✓ spec/change-risk-reminders
✓ spec/console-coordinate-review
✓ spec/console-spec-reading
✓ spec/console-version-confirmation
✓ change/feishu-agent-sticky-default
✓ change/helper-first-offer-idempotence
✓ change/hub-graceful-shutdown
✓ spec/hub-lifecycle
✓ spec/legacy-console-spec
✓ spec/pm-decision-request
✓ change/r1-identify-high-risk-changes
✓ change/r10-task-dispatch-lock
✓ change/r11-freeze-legacy-console-spec
✓ change/r15-session-history-persist
✓ change/r5-binding-admin-page
✓ change/r5-render-openspec-readonly
✓ change/r51-console-timeline-order
✓ change/r6-coordinate-review
✓ change/r7-confirm-immutable-version
✓ change/r8-evidence-driven-change-lifecycle
✓ change/r9-pm-decision-request
✓ spec/task-dispatch-lock
Totals: 24 passed, 0 failed (24 items)
```

范围检查：6份Markdown均在本变更目录；固定需求10条、计划场景41格、spec为13661字节（小于30KB），各需求正文小于500字；git diff --check rc0。没有实现/独立验收勾选，没有目录外产品或脚本改动。
