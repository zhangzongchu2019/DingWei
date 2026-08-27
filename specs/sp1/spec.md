# DingWei SP1 身份与租户地基设计合同

## 0. 头块

| 项 | 值 |
|---|---|
| **Spec ID** | `dingwei-sp1-design` |
| **版本** | v1.0 |
| **状态** | `draft-for-review` |
| **范围** | 乙04 隔离合同、甲10 一企业一库、甲06 改名、`agent → principal` 绑定 |
| **权威章节** | SpecDoc §21、§22.1、§22.3、§22.4、§7.1 P1 |
| **分级** | L3（安全面＝是） |
| **冻结施工锚** | `46b79ab0022b0686a23455f328c9316d7f87281f` |
| **目标件** | `docs/dingwei-sp1-design.md` |

## 1. 六个必备面

### ① 系统对接

定义 credential/connection → `SessionAuthRef` → envelope → Receiver 的 P1 传播，enterprise/principal/channel/session/
agent provenance 模型，`AccessCtx`，ops 注册的 principal→企业库映射，一企业一 PG 库与人层 RLS 合同。
只细化既有 IDN/MSG/PLT seam，不新增架构接口 ID。

### ② 运行约束

- enterprise 由可信 principal 经 ops 注册表派生；0/多映射、请求自报、域名猜测均拒绝。
- SQL/selector 前完成企业库选择；取连接后第一条事务语句 `SET LOCAL` principal context。
- `dw_owner`/`dw_app`/`dw_migrate` 分离，runtime 非属主/非 superuser/无 BYPASSRLS，表 FORCE RLS。
- 绑定未闭前 agent 不作审计交叉证据；任何时期 Agent 都不进授权或门判定。

### ③ 预计数据

定义 enterprise/principal/channel/session/agent binding、0/1/多映射、错库拒绝、连接池复用、旧名 prod/test/comment
计数出口；具体规模未知写 `unknown`，不得编造性能结论。

### ④ 环境要求

设计期不接生产、不建库、不跑真实 PG；secret/DSN/真实企业数据不入正文。后续 dev/test/preprod/prod 的
注册表与数据库映射均由平台 ops 管理，不由请求或 Agent 自报。

### ⑤ 设计约束

- 企业决定数据库，principal 决定 owner/actor；不可用单一 tenant 混装两轴。
- `agent_id` 仅 provenance，不能提升 principal/assurance/capability。
- `TenantCtx → AccessCtx` 归零只是必要条件；仍须 `Org()`/`Principal()` 双轴负测。
- RLS 只覆盖企业库在线访问；备份/副本/复制槽/平台特权面另列加密、审批与审计。
- SP1 定身份与隔离合同，SP7a 实现仓储/RLS/DDL/migration；不得把实现 PASS 倒签给设计。
- 不新增外部依赖，不扩到 SP2–SP8 产品能力。

### ⑥ 输入来源

冻结锚及三件指纹见 `specs/sp0-1/spec.md` 第 2 节；现有代码只用于定位迁移断点和复用 seam。

## 2. 交付物

| ID | 必须内容 |
|---|---|
| `D-SP1-01` | P1 主体传播与伪造/缺失/吊销失败点 |
| `D-SP1-02` | enterprise/principal/channel/session/agent binding 模型与唯一键 |
| `D-SP1-03` | AccessCtx 双轴、签发/引用/吊销/过期与最小字段 |
| `D-SP1-04` | ops 注册表、opaque database key、0/1/多映射、一企业一库/平台库分离 |
| `D-SP1-05` | 三 role、FORCE RLS、SET LOCAL、policy/WITH CHECK、负测与残余风险 |
| `D-SP1-06` | bind/rebind/generation/audit 与 Agent 两条长期禁令 |
| `D-SP1-07` | TenantCtx/PendingTenantCtx/NewTenantCtx 迁移阶段、独立棘轮与完成判据 |
| `D-SP1-08` | SP7a 转交五列：被测对象/输入/期望/证据形态/验收门 |

同时交付 `specs/sp1/check.sh`，机器派生 SP1 章节、owner_sp=SP1 行、身份接口与旧名现状集合。

## 3. 验收

| ID | 判据 |
|---|---|
| `AC-SP1-01` | `D-SP1-01..08` 唯一齐全；权威章节、owner_sp=SP1 行与相关原型入口未知 0 |
| `AC-SP1-02` | connection/envelope/Receiver principal 同源；伪造/缺失/吊销 fail-closed |
| `AC-SP1-03` | 企业仅由服务端注册映射得出；0/多结果、请求自带 org/tenant 均拒绝 |
| `AC-SP1-04` | 一企业一库、平台库独立；错库不得伪装成功空集 |
| `AC-SP1-05` | role/owner/BYPASSRLS/FORCE/SET LOCAL/WITH CHECK 逐项有正反合同 |
| `AC-SP1-06` | 池复用残留、缺 context、谓词错、跨人越权均有负测 |
| `AC-SP1-07` | Agent 不提升权限；绑定前审计交叉证据拒绝，授权/门输入永久拒绝 |
| `AC-SP1-08` | 三类旧名独立棘轮只降；归零后双轴语义负测仍须 PASS |
| `AC-SP1-09` | RLS 外残余面有 owner、审批、时限、加密/审计转交 |
| `AC-SP1-10` | SP7a 转交五列齐，不以设计替实现 |

## 4. 不交付

SP0 或 SP2–SP8 详细设计、仓储实现、DDL/migration、真实 PG 验证、部署、生产配置、新外部依赖。

