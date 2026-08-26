# T18 平台基础升级 整版提审包

- 设计冻结锚:v2 无(T18 设计=v1.2.1)→ **D3 冻结 28ed550**(tag t18-design-frozen,三方审全过 shence 契约+prodops 部署+coordinator 写权)。
- 实现分支:feat/t18-platform-upgrade,基线=origin/main a5f7c25,**tip=eeb5b2b**。
- 取码:`git fetch origin <tip 40 位 SHA>`(⚠工蜂读副本分裂,勿信 ls-remote 分支行;必要时按 tag t18-design-frozen 或 SHA)。
- 鉴权模型=**B(业主拍,08-04):per-session token**(Bearer token→{session,role};非账号级 SH_KEY_ID——SH_KEY_ID 全同=账号级不可区分会话,prodops 挖出)。

## 1. 改动面

**新增服务 internal/datad + cmd/dingwei-datad**(3397 insertions):
- store.go(Store 接口+域类型+错误三类 404/409/412)/memstore.go(dev/测试载体)/pgstore.go(§2 DDL SchemaDDL 自迁移+CAS 事务+jsonb_set+audit REVOKE 纵深注释)。
- server.go(15 端点+withAuth token 鉴权+两不变量执法+me 别名+audit 含未遂+CAS+乐观锁+progress 白名单)。
- kb.go(KBProvider 接口+FSGrepProvider:原子写/防路径穿越/计分)。
- cmd/dingwei-datad(DATAD_TOKENS_FILE/DATAD_PG_DSN|STORE_MEM/DATAD_KB_DIR/SIGHUP ReloadTokens)。

**迁移 scripts/t18-migrate**:import.py(走 API 不直连 PG,tasks+dispatch+config 幂等导入)/ledger_adapter.py(task-ledger→import 格式)/data/task-ledger-imgsearch.json(30 件真账 sha256=54ecb9c8)。

**文档 docs/**:范围 v1/设计 v1.2.1(D3 冻结)/D5 迁移方案/本包。

**依赖新增**:gopkg.in/yaml.v3(配置)、jackc/pgx v5(PG 驱动)。

**M5/hub/MCS 域零触碰;与 T17/reply-origin 零交集。**

## 2. 判据对照(设计 v1.2.1 → 代码 → 测试)

| 判据 | 代码 | 测试 |
|---|---|---|
| 单入口铁律(除 datad 无直连 PG) | 所有 PG 访问在 pgstore.go;import.py 走 API | grep 全仓 pgx 连接仅 pgstore |
| §4 coordinator 单写(管理域) | server handlePutTask/Dispatch/Config/skill approve role!=coordinator→deny | TestP2WriteMatrix(member 写 403) |
| §4 自报域行级(token.session==目标行) | handlePutState/heartbeat name!=id.Session→403;coordinator 也不代写他人 state | TestSelfReportDomainRowLevel |
| progress owner-only+白名单防越权改 status | handleProgress owner 判定+DisallowUnknownFields | TestProgressOwnerOnly(status 字段 400) |
| §3 config CAS 412+版本化 | PutConfig 事务比对 generation | TestP3ConfigCAS(412+历史代次) |
| task 乐观锁 409(If-Match=updated_at) | PutTask ifMatch+首建免锁 | TestTaskOptimisticLock(无锁/过期/正确) |
| §7 skill 提案态强制+批准 coordinator | ProposeSkill status=proposed;approve role 判 | TestSkillFlowAndAuditRead |
| §4 audit 每写一条+未遂 403 也记+coordinator only 读 | audit()中间件;deny 记 .denied | TestP2(未遂审计)/TestSkill(audit 读 403) |
| §7b me 别名(路径+查询 owner=me) | resolveMe | TestMeAliasInQuery |
| §6 KB 可插拔+fsgrep+ingest 策展单写 | kb.go | TestKBIngestQueryRBAC(穿越 422/member ingest 403) |
| §3 GET /v1/config 列表(project_context) | handleListConfig/ListConfig DISTINCT ON | TestP3ConfigCAS 尾 |
| B 鉴权 token 模型+role 表热载 | withAuth token→{session,role};ReloadRoles(注:tip=token 模型) | TestRoleReloadAndAuthModel(未注册 403/缺... 见注) |
| P9 PG 宕机=503 fail-closed | storeErr default 503;OpenPG 失败不起 | storeErr 覆盖 |
| §5 迁移(走 API/幂等/自报域不迁) | import.py+ledger_adapter | 样本+30 件 dry-run/真导入实证 |

**注(鉴权模型):** 提审 tip=eeb5b2b(=767680b token 模型+测试库铁律注释)。767680b 的鉴权=Tokens map[token]Identity(per-session token 模型=B)。测试 server_test.go/kb_test.go 为 token 模型版(call() token 参数=per-session token,TestRoleReload... 实为 token 轮换双活窗)。B=业主拍的 per-session token,datad 零改即此模型。

## 3. 真机证据(D4c×D5)

- D4c:prodops 真 PG 集成 TestPGIntegration PASS(幂等迁移/乐观锁/CAS/jsonb_set/自报域/audit)+datad 生产部署 systemd active@106.52.174.122:8796+nginx TLS(https://dingwei.wegoab.com/datad/ healthz 200)+audit REVOKE 纵深。
- D4b:MCP 联调 PASS(shence shim 三工具直打生产 datad 过 nginx TLS,heartbeat 202/tasks/sessions/config 200)+helper 代码核@0f92e90(tag t18-b-helper,token=cfg.datad_token per-session)。
- D5:30 件真账导入生产(dry-run→真导入 ok=30→回读活账 27 done/1 proposed/2 in_progress 与 coordinator 账逐项一致);B 鉴权双侧实证(coordinator 写+member 读/自报/RBAC 三负例 403)。
- 16/16 token 发齐;双写窗启动(3 日纯 datad 判据)。

## 4. 复现命令

```
go build ./... && go test ./internal/datad/ ./cmd/dingwei-datad/ -count=1
DATAD_TEST_PG_DSN=postgres://.../dingwei_test go test ./internal/datad/ -run TestPGIntegration  # 真 PG(独立测试库,永不生产)
# 迁移 dry-run:ledger_adapter.py data/task-ledger-imgsearch.json | import.py --dry-run
```

## 5. 披露项(reviewer 先读)

1. **鉴权 B(per-session token)**:业主拍 B(A 复用 SH_KEY_ID 因其账号级全同不可区分会话被否)。datad=token 模型(767680b),token 值=prodops 签的 per-session 随机;helper 改一行取 DATAD_TOKEN(shence@0f92e90)。A 预实现未提交已丢弃零残留。
2. **自报域不迁**(§5 决策):session_state/heartbeat 不批量导入(行级不变量禁代写+易失态无价值),上线各会话 MCP 自报重建;仅管理域(task/dispatch/config)一次性导入。
3. **一期不迁**:hub message/bot_channel SQLite(生产消息链不拴新依赖)+MCS board(调度器域,预留 mcs_* 命名空间)。
4. **后台管理 UI 不在本件**(管理 API 契约 day1 CLI 可管);collector 透传延后。
5. **测试库隔离铁律**(08-04 教训):DATAD_TEST_PG_DSN 必须指 dingwei_test 永不指生产(曾污染留 T-ITEST 残留,已删根+隔离+代码注释锁@eeb5b2b)。
6. **秘密卫生**:token 不入 PG(存 token_id)/不入 memory(env);secret 不进 config 表。

## 6. 建议评审派工

- shence:契约/MCP 面(§3 端点/§7b vsapi/§6 KB)——已 D3 PASS+联调 PASS,复核实现对齐。
- prodops:部署/迁移/PG 面(§5/§8b/pgstore/审计纵深)——已 D3 PASS+真机 PASS,复核。
- coordinator/新 reviewer:写权/鉴权 B/整体。
- 整版一次性给全量 findings,≤3 轮。
