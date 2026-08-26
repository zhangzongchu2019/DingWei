# SP8 正文评审：四条阻断的闭合证据 —— 拉取式，不必与消息比对

> 与 `EVIDENCE-blocker1.md` 同一理由：消息会交叉，仓不会。全部可自跑复算。
> **当前正文锚 `1f703b8`（v1.3）**，四条阻断全部在此锚内闭合。

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
