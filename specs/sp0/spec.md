# DingWei SP0 总则与定义层设计合同

## 0. 头块

| 项 | 值 |
|---|---|
| **Spec ID** | `dingwei-sp0-design` |
| **版本** | v1.0 |
| **状态** | `draft-for-review` |
| **范围** | SpecDoc §1–§4、附录 A |
| **分级** | L2 |
| **冻结施工锚** | `46b79ab0022b0686a23455f328c9316d7f87281f` |
| **目标件** | `docs/dingwei-sp0-design.md` |

## 1. 六个必备面

### ① 系统对接

设计 spec/version/input anchor/attribute/rule set/checklist/review/pending/tuning 模型，细化既有
`GOV-CMD-SubmitSpec`、`GOV-QRY-ResolveGateSet`，向 WRK 只提供不可变 spec/gate-set ref。

### ② 运行约束

- `attributes + rules_version` 可复算；缺输入 fail-closed。
- 硬门失败不进人审；机器全过不替代人审。
- author/reviewer/implementer 权限分离；正文变更使已认领件退审。
- 重复、并发、迟到评审与代际冲突均不覆盖历史。

### ③ 预计数据

只定义 spec/version、rule set、checklist、review、pending、tuning 的计量出口；规模未知写 `unknown`，
每个预计值须有依据和复核点，不用未实测数字作容量结论。

### ④ 环境要求

只读冻结仓；不接生产、不改数据库/部署；不新增依赖。DMN 仅用已裁决策表，不引入执行引擎。

### ⑤ 设计约束

- 单一权威：SpecDoc 管产品规则，设计件不复制第二份规则表。
- 复用现有 Spec Kit、lint/gate 与目标架构 GOV 接口，不另造平行工具链。
- 空集、解析失败、依赖闭包不完整均不得报 PASS。
- 生命周期、review verdict、checklist、pending 分开建模。
- 附录 A 仅允许调数值；改变结构/权限/语义须走语义评审。

### ⑥ 输入来源

冻结锚及三件指纹见 `specs/sp0-1/spec.md` 第 2 节；代码和现有工具只证复用点，不决定产品语义。

## 2. 交付物

| ID | 必须内容 |
|---|---|
| `D-SP0-01` | 范围、术语、与 SP8/WRK/PLT 边界 |
| `D-SP0-02` | spec/version/input/attribute/rule/checklist/review/pending/tuning 模型与唯一键 |
| `D-SP0-03` | 各自生命周期、代际与不可变历史 |
| `D-SP0-04` | SubmitSpec、ResolveGateSet 细化及内部 ports/events |
| `D-SP0-05` | 数据设计与迁移责任，不写生产 DDL |
| `D-SP0-06` | 准入、人审、非语义修订、依赖闭包、认领退审、调参流程 |
| `D-SP0-07` | author/reviewer/implementer/owner 权限与审计矩阵 |
| `D-SP0-08` | 幂等、并发、迟到、UNKNOWN/FAIL 处理 |
| `D-SP0-09` | 现有 specs/scripts 的 retain/adapt/new/retire 映射 |
| `D-SP0-10` | §1–§4、附录 A、owner_sp=SP0 的附录 E 行与原型入口验收矩阵 |
| `D-SP0-11` | 未决、运行样本和跨 SP 转交合同 |

同时交付 `specs/sp0/check.sh`，机器派生预期/实检/未知集合；未知非空不得宣称覆盖完成。

## 3. 验收

| ID | 判据 |
|---|---|
| `AC-SP0-01` | `D-SP0-01..11` 唯一齐全；§1–§4、附录 A 与 owner_sp=SP0 行未知 0 |
| `AC-SP0-02` | 原型中的 spec 查阅/准入/评审/待决/调参入口全部分类 |
| `AC-SP0-03` | 新增外部依赖 0、目标架构新增接口 ID 0 |
| `AC-SP0-04` | 权限不代签，逐项反例有拒绝点 |
| `AC-SP0-05` | 规则复算同输入同输出，缺项 fail-closed |
| `AC-SP0-06` | 非语义修订按依赖闭包判，闭包不全转语义修订 |
| `AC-SP0-07` | 恒零/全称断言声明范围，空集/UNKNOWN 不报 PASS |
| `AC-SP0-08` | 附录 A 越界变更退回语义评审 |

## 4. 不交付

SP1–SP8 详细设计、代码、DDL、migration、部署、CI、生产证据、新产品规则或新外部依赖。

