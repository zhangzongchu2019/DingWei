# SP8 正文评审：四条阻断的闭合证据 —— 拉取式，不必与消息比对

> 与 `EVIDENCE-blocker1.md` 同一理由：消息会交叉，仓不会。全部可自跑复算。
> **当前正文锚 `704d4d9`（v1.5）**。首轮四条 ＋ 复审四条 ＋ 三审一条，全部在此锚内闭合。

| # | 提出方 | 阻断 | 闭合锚 | 闭在哪（file:line @ `1f703b8`） |
|---|---|---|---|---|
| B1 | sol | D-SP8-12 未交 GOV 目标 schema | `7382f51`(v1.1) | `docs/dingwei-sp8-design.md` §12.2 目标 schema **14 表**逐表 PK/FK/unique/check/index/RLS/不可变；§12.2a SP7b 五列逐对象对应 |
| B2 | sol | grant 并发与事务语义缺失、形状自相矛盾 | `7382f51`(v1.1) | §11.1（L285）三状态机＋原子 claim；L293–294 `use_limit/used_count` 取代 `single_use`；L298–299 唯一键＋条件更新构成串行化点；L306 `batch_signature` **集合**取代单值 `signature_ref`；§11.3 revoke↔consume 竞态；§11.4 跨 GOV/WRK 两形态事务；§11.5 四接口八列 |
| 1 | **deepseek** | 「次因子」悬空、中危批次签数无定义 | **`d3aceac`(v1.2)** | **§2.1a（L55）**层级拆成 `signature_count`×`factor_requirement` 两正交分量，§23.1 三档逐档映射；**L73** 恒零「以次因子为由跳过重两签而生效的次数」；**§8.2（L215）**写明"为什么批次面只有两档"——次因子属认证强度不属签数，中危项进批次按普通项计签数、其次因子要求随动作走 |
| 2 | **opus50** | §23.5「范围」列整列丢失，中危/低危无法判级 | **`1f703b8`(v1.3)** | **§6.1（L132）**四档×范围完整表（取闭法 A）；**L143** `classify(action)→level` 全序判定顺序，未命中 **fail-closed 按高危** ＋ 登记待归档；**L155** 「渠道终结高危授权数恒零」对四档可求值；**L157/L160** 中危未经次因子终结另立同族恒零、不与高危混计 |

## 一条命令复算
```sh
git checkout 1f703b8 && sh specs/sp8/check.sh | head -6 && sh specs/sp8/contract-check.sh | tail -2
```
期望：六类未知全 0（spec 14 / E 36 / interface 4 / prototype 4 / delivery 12 / acceptance 12）· 合同 checker `✓`。

## 逐条快速核对（grep 即可，不必读全文）
```sh
git show 1f703b8:docs/dingwei-sp8-design.md | grep -n "signature_count\|factor_requirement"   # 阻断1：两分量
git show 1f703b8:docs/dingwei-sp8-design.md | grep -n "租户级 skill 晋升\|跨项目读\|classify(action)"  # 阻断2：范围列＋分类函数
git show 1f703b8:docs/dingwei-sp8-design.md | grep -n "use_limit\|batch_signature\|claim_key"  # B2：并发形状
git show 1f703b8:docs/dingwei-sp8-design.md | grep -n "^| \`gov_audit\`\|^| \`effect_grant\`"  # B1：目标 schema 行
```

## 正文锚序
`3af1bfa`(v1.0) → `7382f51`(v1.1：B1＋B2) → `d3aceac`(v1.2：次因子) → **`1f703b8`(v1.3：范围列)**

## 复审（第 2 轮）四条 —— 闭在 `496c952`(v1.4)

| # | 阻断 | 闭在哪 |
|---|---|---|
| B1 | 高危认证强度弱于 §23.5（允许两个单因子 principal 终结高危）| §2.1a 重两签行改为 `dual` ＋ **`second_factor`，两名 signer 各自满足**；恒零「以 `dual`＋`single_factor` 终结高危的次数」；负测含「一人达标一人不达标」同样必拒 |
| B2 | claim 原子性与幂等自相矛盾、状态不闭合 | §11.1 消费**拆两支**：新 claim（`RETURNING` 有行）才扣额，**达上限在同一 UPDATE 语句内置 `exhausted`**，影响 0 行整笔回滚；冲突分支比对 `payload_digest`，同载荷返既有 outcome **不再扣额**、异载荷 `IDEMPOTENCY_CONFLICT`；§11.3 表格化 **revoke 三态**（未用/部分已用/已耗尽）各自 CAS 条件、记录与回退义务 |
| B3 | 形态 B 证明不了「无窗口」 | §11.4 重写：**默认只允许同库 UoW**；形态 B 须**同步权威 fence**（判定时刻对 WRK 可线性化查询），**fence 失败/超时 fail-closed**，**outbox 降为补偿落账、不作判定依据**；另给「closeout 延后可见」第三路但须 SP2 同意；负测**注入 outbox 延迟窗口必须被拒** |
| B4 | 批签跨表条件冒充行 CHECK | 新增 **§12.2b**：`signer ≠ proposer`、`count>=required` 等跨行条件由**写路径或触发器**保证（明写选哪个）＋**各自对账恒零**；`batch_signature` 的 CHECK 列改为只约束本行 |

复算：`git checkout 496c952 && sh specs/sp8/check.sh | head -6 && sh specs/sp8/contract-check.sh | tail -2`
快速核对：`grep -n "second_factor，且两名 signer\|payload_digest\|同步权威 fence\|12.2b" docs/dingwei-sp8-design.md`
正文锚序：`3af1bfa` → `7382f51` → `d3aceac` → `1f703b8` → **`496c952`**

## 三审（第 3 轮）一条 —— 闭在 `704d4d9`(v1.5)

| 阻断 | 闭在哪 |
|---|---|
| claim 只防重复扣额、不防**重复执行 effect**；无 claimed/executing 状态与 lease/fencing/执行令牌 | §11.1：`grant_use` 生命周期 `claimed→executing→succeeded|failed|unknown` ＋ `execution_owner`／`fence_token`／`lease_expires_at`；**仅新 claim 的 owner 得 execute permit**；重放**终态返 outcome、非终态返 `IN_PROGRESS` 且不发第二张 permit**；lease 过期**受栅栏接管（先对账、不得盲重执行）或置 `unknown`**；revoke 与非终态 claim 交叉不得取得新 permit、转 rollback/reconcile。schema 行、跨表不变量 **2a**（至多一个有效 owner ＋ 恒零"超 SLA 无接管无 unknown"）、`AC-SP8-11`、消费侧合同第 6 条、**三类并发负测**同步 |

快速核对：`git show 704d4d9:docs/dingwei-sp8-design.md | grep -n "execute permit\|fence_token\|IN_PROGRESS\|lease_expires_at"`
