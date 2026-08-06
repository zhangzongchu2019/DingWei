# T18 DingWei 平台基础升级 总体设计(D2)· D3 冻结版

**冻结(08-04)**:D3 三方审全过——shence 契约面 PASS+prodops 部署面 PASS+coordinator 写权面 PASS(其审锚 cfcec9e 与本版差=§8b 两行 prodops 定案+本收口行,审围字节同,归一于本版)。coordinator 唯一提醒已入档:**自报域 token 轮转运维程序**(轮换周期/触发条件/操作步骤/回滚)=D4c 运维手册必备章,prodops 执笔 devo48 核。
范围基线:t18-platform-upgrade-scope-v1.md@fb5ef8e(D1)。
契约模板沿用 T17 先例:versioned API/错误码全表/token=审计主体/CAS/掩码(§K/§F 同哲学)。

## 1. 架构总览

```
MCP工具(各会话) ─┐
coordinator ─────┼──> dingwei-datad(VS统一API,唯一入口) ──> PostgreSQL
后台管理UI(后期) ┘         │铁律:除datad外任何组件不连PG
                            └─> 知识库provider(可插拔接口)
```

- **dingwei-datad**=新独立 Go 服务(决策点①建议案:不扩展 View Service——其职责=终端视图,数据面混入=两器互拖;同仓 cmd/dingwei-datad,复用 T17 管理面模式)。
- PG=唯一数据中心;datad=唯一入口;MCP=会话侧唯一姿势。三层各自单一。

## 2. Schema v1(PG DDL,七表)

```sql
CREATE TABLE task (            -- 任务台账
  id TEXT PRIMARY KEY,                     -- 件号:T17/T18/…
  title TEXT NOT NULL,
  owner_session TEXT NOT NULL,             -- zzc-devo48-2642
  status TEXT NOT NULL CHECK (status IN
    ('proposed','dispatched','in_progress','review','blocked','waiting_gate','done','archived')),
  anchor TEXT,                             -- 当前 git SHA/tag 锚(B类锚,来自git输出禁手敲)
  gate TEXT,                               -- waiting_gate 时:所候事件(如"board上产")
  eta TEXT, detail JSONB,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now());

CREATE TABLE dispatch (        -- 派工与轮换(append-only)
  id BIGSERIAL PRIMARY KEY,
  task_id TEXT NOT NULL REFERENCES task(id),
  assignee TEXT NOT NULL, role TEXT NOT NULL CHECK (role IN ('executor','reviewer','advisor')),
  round INT NOT NULL DEFAULT 1, note TEXT,
  created_at timestamptz NOT NULL DEFAULT now());

CREATE TABLE config (          -- 平台配置(版本化,append-only,读取最新代次)
  key TEXT NOT NULL, generation BIGINT NOT NULL,
  value JSONB NOT NULL,
  updated_by TEXT NOT NULL, updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (key, generation));

CREATE TABLE session_state (   -- 会话状态(行级自报域)
  session TEXT PRIMARY KEY,
  state TEXT NOT NULL CHECK (state IN ('active','idle','silent_notice','restarting','dead')),
  watermark_pct REAL, current_task TEXT, note TEXT,
  updated_at timestamptz NOT NULL DEFAULT now());

CREATE TABLE heartbeat (       -- 心跳打点(行级自报域,保留30d滚动清理)
  session TEXT NOT NULL, at timestamptz NOT NULL, payload JSONB,
  PRIMARY KEY (session, at));

CREATE TABLE skill (           -- Skill库(提案全员,批准=coordinator)
  name TEXT NOT NULL, version TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('proposed','approved','deprecated')),
  content TEXT NOT NULL, sha256 TEXT NOT NULL,
  proposed_by TEXT NOT NULL, approved_by TEXT,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (name, version));

CREATE TABLE audit (           -- 全写操作审计(系统自动,任何token不可写/删)
  id BIGSERIAL PRIMARY KEY,
  actor TEXT NOT NULL,                     -- token_id
  op TEXT NOT NULL, target TEXT, detail JSONB,
  at timestamptz NOT NULL DEFAULT now());
```

命名空间预留:`mcs_*`(调度器域,一期空)。知识库**无表**=provider 接口后置(§6)。

## 3. VS 统一 API 契约 v1(datad,REST,全 JSON)

鉴权:`Authorization: Bearer <session token>`;token 注册表=datad 配置文件(掩码/轮换双活窗,同 T17 §F);每 token 绑定 `{session, role∈{coordinator,member}}`。**每次写=audit 一条(actor=token_id)**。

| 端点 | 方法 | 写权 | 语义 |
|---|---|---|---|
| /v1/tasks[?status=] | GET | — | 台账列表/单件(全员读) |
| /v1/tasks/{id} | PUT | coordinator | 建/改件(单写域);If-Match 乐观锁(updated_at)否则 409 |
| /v1/tasks/{id}/progress | POST | **owner_session 本人** | 自报域:进度/锚推进(只可改 anchor/detail.progress/eta,不可改 status/owner=防越权),写 audit |
| /v1/dispatch | POST | coordinator | 派工(append-only;advisory xact 锁保 id 序=提交序) |
| /v1/dispatch[?task_id=&since_id=&limit=] | GET | coordinator | 权威只读侧(读侧 DispatchRow 含 id/created_at;since_id 增量,limit≤1000,截断出 X-Has-More/X-Next-Since-Id 头);audit 重建不权威故设 |
| /v1/heartbeats/latest | GET | coordinator | 每会话最新心跳(活性核;无 payload) |
| /v1/config/{key} | GET | — | 最新代次(?generation= 取历史) |
| /v1/config/{key} | PUT | coordinator | If-Match: <generation> CAS,不符=412(T17 同款) |
| /v1/sessions/{name}/state | PUT | **本人**(token.session==name 否则 403) | 行级自报 |
| /v1/sessions/{name}/heartbeat | POST | **本人** | 心跳打点(替 hb.py) |
| /v1/sessions | GET | — | 全员状态一览(活性判定=coordinator 一次 GET) |
| /v1/skills | GET/POST | POST=全员(status 恒 proposed) | 提案 |
| /v1/skills/{name}/{ver}/approve | POST | coordinator | 批准/弃用 |
| /v1/audit?since= | GET | coordinator | 审计查询 |
| /v1/kb/query,/v1/kb/ingest | POST | 见 §6 | 知识库可插拔面(接口冻,provider 后批) |
| /healthz,/metrics | GET | — | 存活/计数 |

错误码全表:400 schema/401 token/403 越权(单写域或越行)/404/409 乐观锁/412 CAS/413 size(1MB)/422 语义/503 PG 不可达(fail-closed 不降级为内存态)。
监听面:内网端口+bearer;跨机=TLS(nginx 反代);loopback 免 TLS(T17 §E 同规)。

## 4. 安全模型

- **单写不变量**:管理域(task 状态/owner/dispatch/config/skill 批准)写=role:coordinator 唯一;403+audit 记录未遂。
- **自报域行级不变量**:member 只能写 `token.session==目标行 session` 的 state/heartbeat/本人 owner 件的 progress;越行=403+audit。
- audit 表无任何写端点(仅 datad 内部 insert);PG 层再加一道:datad 连接用户对 audit 仅 INSERT 无 UPDATE/DELETE 权(纵深)。
- token 轮换:双活窗≤24h;token 明文只在 datad 配置(600)与各会话 env,PG 不存明文(存 token_id)。
- 备份内含台账非秘密;config 若存敏感值须先加密再入(v1 约定:secret 不进 config 表,留 env——决策点⑦新增)。

## 5. 迁移方案(双写窗,决策点⑤建议案)

- **迁入物三类**:coordinator 手工台账→task/dispatch(一次性导入脚本+人工核对);心跳(hb.py/各会话)→heartbeat API 双写;在线清单→session_state(sessionHelper 侧加一个 POST,或过渡期由 coordinator 定时导)。
- **双写窗 1-2 工作段**:旧账照跑,新链并行写;判据=coordinator 连续 3 日以 PG 查询完成日常协调无需旧账。
- **切换**:旧账冻结只读留档;**回滚**=停新链写,旧账解冻(窗内旧账从未停=回滚零成本)。
- hub/MCS 存储不迁(范围 §1①);send.py 消息信道不迁(数据面≠消息面)。

## 6. 知识库可插拔接口(冻结候选,provider 后批)

```
type KBProvider interface {
  Query(ctx, q string, topK int) ([]KBHit, error)   // KBHit{Source,Chunk,Score}
  Ingest(ctx, doc KBDoc) error                       // KBDoc{Source,Content,Meta}
}
```
- v1 内置 provider=`fsgrep`(文件目录+全文检索,零新依赖);向量库 provider=接口不变后批插入。
- API 面 /v1/kb/* 转调当前 provider;provider 选择=config 表一键(热载)。

## 7. MCP 工具契约面(shence 实现输入,七工具→API 映射)

| MCP 工具 | 调用 | 备注 |
|---|---|---|
| 团队制度查询 | GET /v1/config/rules.* + /v1/kb/query | 规约条文入 config,长文档走 kb |
| 项目上下文 | GET /v1/tasks?owner=me + /v1/sessions | 恢复/接件一拉即得 |
| 智能 send | (信道仍 send.py/hub)工具内建三坑修复(shence 建议具体化):**短名→全名匹配**(寻址表 GET /v1/sessions)+**在线清单刷新**(过期>2min 自动重拉)+**冠字头自动格式**(抬头=send 收件人强一致,消笔误)+3KB 自动分片 | 修 send.py 三坑 |
| 任务读写 | GET /v1/tasks;POST /v1/tasks/{id}/progress | 写仅自报域 |
| 知识库 | POST /v1/kb/query | provider 无感 |
| Skill 库 | GET /v1/skills;POST 提案 | 拉取校验 sha256 |
| 心跳自报 | POST /v1/sessions/{me}/heartbeat + PUT state | 替 hb.py;静默预告=state=silent_notice |

## 7b. MCP 调用链路与 IPC 通用化(shence 输入,08-04 定契约)

链路(与现 mcp_send 同构,shence 确认可行):**MCP shim→IPC(Unix socket)→helper→datad(VS API)**;shim 不持凭据,helper 以本会话 token(SH_KEY_ID 系)调 datad——**每会话一 token 可信模型天然成立**。

**IPC 扩展契约(通用化,非逐工具专有 type)**:现 payload={type,to,body,...};新增**单一通用 type**:
```json
{"type":"vsapi_v1","request_id":"...","method":"GET|POST|PUT","path":"/v1/...","body":{...}}
```
- helper 职责=纯管道:附 `Authorization: Bearer <本会话token>` 转发 datad,响应原样回 IPC(不做任何权限判断——**单一执法点=datad**,helper 不长第二个 RBAC);
- **`me` 别名**:datad 支持路径中 `me`(如 /v1/sessions/me/heartbeat、/v1/tasks?owner=me)按 token 解析会话身份——shim/工具层无需知道全名,helper 无需改写路径=真纯管道;
- 七工具全部落在这一个 type 上(工具语义在 MCP 层,线上只有 vsapi_v1)——IPC type 枚举零膨胀,向后兼容(旧 helper 遇未知 type 拒收即可)。
- 智能 send 例外:信道走现有 mcp_send(消息面),仅寻址表查询走 vsapi_v1。

## 8. 决策点建议案(呈业主,coordinator 已转)

①VS=**新独立服务 dingwei-datad**(View Service 不混职责)②hub 一期**不迁**③自报域=**行级**(已入 §4 不变量)④知识库首 provider=**fsgrep**(接口可插拔,向量库后批)⑤迁移=**双写窗 1-2 段**⑥PG HA=**已定标(prodops 08-04)**:单实例+WAL 归档(/data/pg-wal/),主备延后——平台数据非延迟关键,WAL 覆 crash 恢复,与 hub 单容器风险平齐⑦**secret 不进 config 表**(留 env,备份不含密)。

## 8b. 部署基线(prodops 输入,08-04)

- 部署位=**106.52.174.122**(与 hub 同机隔离;114.132.71.70 间歇不可达弃选);/data 持久卷;datad 同机低延迟;独立 systemd unit+端口。
- 备份=pg_dump cron→/data/backup/+WAL 归档;**异机 dump 已定(prodops AGREE 08-04)**:定期拷贝至 43.135.178.132(或对象存储,prodops 择一落地);**D4c 前置件**:该机 scp key/SSH 凭证(授权归口 coordinator/张宗楚,prodops 申请)。
- 监控=pg_exporter 或 datad /healthz 透传 PG 探活;权限=**pg_hba 仅 localhost(prodops AGREE 定案)**;未来 datad 分机再开单 IP。

## 9. 测试计划(P 组)

P1 单入口(grep 全仓:PG 连接串仅 datad)/P2 权限矩阵(coordinator/member×管理域/自报域/越行=403 全格)/P3 config CAS 412+并发/P4 心跳链 e2e(MCP→datad→PG→coordinator 查询)/P5 恢复演练(会话重启→项目上下文一拉即得)/P6 迁移可逆(双写窗切回零丢)/P7 审计完整性(每写一条+未遂 403 也记+audit 表不可改)/P8 token 轮换双活窗/P9 PG 宕机=datad 503 fail-closed(不静默内存态)/P10 skill sha256 校验拒篡改。

## 10. 实现分解(D4,对应 ETA)

- D4a(devo48,2-3 段):datad 骨架+schema 迁移件+tasks/config/sessions/audit 端点+RBAC/CAS/audit 中间件→skills/kb(fsgrep)→P 组测试;
- D4b(shence,并行):MCP 七工具(本契约 §7 冻结后开工);
- D4c(prodops,并行):PG 实例+备份+监控+datad systemd+nginx;
- D5(三方):导入脚本+双写窗+切换判据。
