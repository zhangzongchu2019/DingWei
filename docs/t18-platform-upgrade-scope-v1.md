# T18 DingWei 平台基础升级 范围文档 v1

状态:D1 交付件(coordinator 立项 08-04);候三方确认范围后进 D2 总体设计。
分工(coordinator 拍):总体设计=devo48;MCP 侧=shencedeepseek;PG 部署=prodops。

## 0. 背景与目标

现状痛点:平台状态散置——任务台账在 coordinator 手工账/board 文件、配置在各机 env、心跳在各会话 state.json、会话上下文靠 memory 文件+send.py 文本互报;无统一数据面=每次协调都是文本采集,恢复/审计/看板全靠拼。
目标:**PG 单一数据中心(任务台账/配置/状态/心跳)+VS 统一 API(唯一读写入口)+MCP 工具面(会话侧原生读写)+管理写权单点(coordinator 单写)**。

## 1. 范围(三子件)

### ① PG 唯一数据中心 + VS 统一 API(数据面,devo48 设计/prodops 部署)

**In scope**:
- PG schema:tasks(任务台账:件/锚/owner/状态/ETA)、dispatch(派工与轮换)、config(平台配置,版本化)、session_state(会话状态/水位)、heartbeat(心跳打点)、audit(全写操作审计)。
- VS 统一 API:REST,**唯一**读写入口(任何组件不直连 PG——单入口铁律,同 T17"真源在代理"哲学);读面全员,写面见子件③。
- 迁移策略:现散置账(coordinator 台账/heartbeat state.json/在线清单)→PG,双写窗口→切换→回滚点。
- 数据 owner 矩阵:每表一 owner(写权归属),见 §3。

**Out of scope(一期不做,列决策点)**:
- hub(workpulse)内部 message/bot_channel SQLite 迁 PG——hub 保持自治,避免把生产消息链拴上新依赖;二期评估。
- MCS board/spool 存储迁移——调度器域(claudek3),仅预留 schema 命名空间。

### ② MCP 扩展(shencedeepseek 主执,devo48 冻契约)

工具清单(七件):
1. **团队制度查询**:规约/契约/纪律 read(源=PG config 表或知识库,见 6);
2. **项目上下文**:任务/锚/状态/在线名册 read(走 VS API);
3. **智能 send**:寻址+抬头纪律+格式内建(修 send.py 三坑:收件人笔误/参数序/3KB 分片);
4. **任务读写**:领件/报进度/交付登记(写走 VS API 自报域,见 §3);
5. **外接知识库可插拔**:接口冻结(query/ingest 两面+provider 抽象),实现后批;
6. **Skill 库**:可复用技能包的登记/分发/版本(存 PG,MCP 拉取);
7. (基线)心跳自报:替代 hb.py 文本心跳。

**边界铁律**:MCP 工具一律走 VS API,不直连 PG;每会话一 token=身份即审计主体。

### ③ 后台管理写权:coordinator 单写

- 写权模型:**coordinator=管理域唯一写者**(任务台账/派工/配置);其余会话=只读+**自报域白名单**(自己的心跳/进度/水位——这些天然必须自报,不算破单写);
- 实施面:VS API 按 token RBAC(同 T17 管理 API 哲学:token_id 入审计/掩码/loopback 或 mTLS);
- 后台管理 UI 对接 VS API(与 T17 管理后台同期,API 先行)。

## 2. 与在途件的关系

- T17 fan-out 代理:**无交集**(消息面 vs 数据面);T17 管理 API 的 RBAC/审计/真源模式为 T18 ③ 的先例模板。
- reply-origin:无交集(信封语义层)。
- MCS 应急切:无交集(事件触发另件)。
- T18 设计期不阻塞上述任何候令件;若 T17 canary/真机令到,T17 优先(业主"必须成"件)。

## 3. 数据 owner 矩阵(草案,D2 细化)

| 表 | 写者 | 读者 |
|---|---|---|
| tasks/dispatch/config | coordinator(单写) | 全员 |
| session_state/heartbeat | 各会话自报(仅本行) | coordinator+全员 |
| skill 库 | coordinator 批准入库,提案全员 | 全员 |
| audit | 系统自动(不可写) | coordinator |

## 4. 关键决策点(D2 要拍)

1. VS API=现 View Service 扩展 or 新独立服务(倾向:新独立服务 dingwei-datad,View Service 职责不混);
2. hub 存储一期不迁(见 §1① out-of-scope)——请确认;
3. 心跳/自报域的写白名单粒度(行级:token 只能写自己 session 行);
4. 知识库 provider 首选(文件目录 / SQLite FTS / 外接向量库)——接口冻结先行,provider 可插拔后批;
5. 迁移窗口形态:双写 or 冻结切换(倾向双写 1-2 段观察);
6. PG 高可用等级(单实例+WAL 归档 or 主备)——prodops 定标。

## 5. 阶段与 ETA

| 阶段 | 内容 | ETA | 归属 |
|---|---|---|---|
| D1 | 范围文档(本件) | 即刻✓ | devo48 |
| D2 | 总体设计 v1(schema+API 契约+安全模型+迁移方案) | 2 工作段 | devo48 |
| D3 | 三方评审(shence MCP 契约面/prodops 部署面/coordinator 写权面,≤3 轮整版) | 1 段 | 三方 |
| D4a | VS API+schema 实现 | 2-3 段 | devo48 |
| D4b | MCP 七工具(契约冻结后并行) | 与 D4a 并行 | shencedeepseek |
| D4c | PG 部署(实例/备份/监控/权限) | 与 D4a 并行 | prodops |
| D5 | 迁移双写+灰度切换 | 1-2 段 | 三方 |

**端到端 ETA≈6-8 工作段**(关键路径=设计 2+审 1+实现 3+迁移 2;MCP/部署并行不占关键路径)。

## 6. 验收面(D2 细化为用例)

- 单入口:grep 全仓无 PG 直连(VS API 外);
- 单写:非 coordinator token 写管理域=403+审计;自报域越行写=403;
- 心跳链:会话 MCP 自报→PG→coordinator 查询一条 SQL 出全员活性;
- 恢复演练:会话重启→MCP 项目上下文一拉即得(替代现 memory+文本采集);
- 迁移可逆:双写窗内切回旧账零丢。
