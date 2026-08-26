# T18 D5 迁移方案(双写窗,设计冻结版 §5)

前提:D4 三线全汇合(datad@1ee0c73c/PG 真机 PASS/nginx TLS 通/MCP 联调 PASS)。
工具:scripts/t18-migrate/import.py(走 VS API,不直连 PG=迁移也守单入口铁律)。

## 1. 迁什么 / 不迁什么

**迁(管理域,一次性导入,coordinator token)**:
- task/dispatch ← coordinator 手工台账(导出 tasks.json)
- config ← 规约/契约条文(导出 config.json,如 rules.review/rules.dispatch)

**不迁(自报域,上线自然重建)**:
- session_state/heartbeat ← **各会话上线后 MCP 自报天然写入**,不批量导入。
  理由:①行级不变量(§4)禁 coordinator 代写他人 state,批量导入会被自己的安全网 403 挡②在线清单/心跳是**易失实时态**,历史无迁移价值,重建=各会话首次自报即得。
  (import.py 保留 --sessions 仅为特殊场景兜底,默认不用。)

**不迁(域外)**:hub message/bot_channel(一期不迁,§1①)、MCS board(调度器域)、send.py 信道(数据面≠消息面)。

## 2. 双写窗步骤

1. **T0 导入**:coordinator 导出台账→`import.py --tasks tasks.json --config config.json`(先 --dry-run 核条数)→PG 落管理域快照。幂等:重跑靠乐观锁/CAS 挡重复(安全)。
2. **双写窗(1-2 工作段)**:
   - 旧账**照常跑**(coordinator 手工账不停)=回滚零成本基线;
   - 新链并行:管理域变更 coordinator 经 datad API 写(PUT task/config);自报域各会话 MCP 自报;
   - 窗内两账并存,datad 为观察对象。
3. **切换判据**(§5):coordinator **连续 3 日**以 datad 查询(GET /v1/tasks、/v1/sessions)完成日常协调,无需翻旧账。
4. **切换**:旧账冻结只读留档,datad 为唯一活账。
5. **回滚**:停新链写,旧账解冻(窗内旧账从未停=回滚零成本)。回滚触发=datad 查询不可用>阈值/数据不一致抽检失败。

## 3. 验收(P 组迁移相关项)

- P6 迁移可逆:双写窗内切回旧账,零丢(旧账全程未停即满足)。
- 导入正确性:import.py 后 GET /v1/tasks 条数=台账条数;抽 3 件核 anchor/status 一致。
- 单入口:迁移全程无直连 PG(import.py 走 API)。
- 自报域重建:各会话上线首次 MCP 自报→GET /v1/sessions 见全员活性(替代在线清单)。

## 4. 与 token 签发的依赖

管理域导入需 coordinator token;正式全员 token(各会话 MCP 自报用)=coordinator 签发流程产物(候拍)。双写窗启动前置=正式 token 到位(否则各会话无法自报重建自报域)。故 token 签发是 D5 硬前置。

## 5. 提审

D5 执行完(导入+双写窗切换判据达成)→整版提审包(datad 实现@汇合 tip + 迁移证据 + P 组全绿),≤3 轮整版。
