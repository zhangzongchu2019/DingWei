# DingWei SP8 授权与平台治理设计 v1.6

| 项 | 值 |
|---|---|
| **Design ID** | `dingwei-sp8-design` |
| **版本** | v1.6 |
| **状态** | draft，待三方全文评审 |
| **冻结施工锚** | 组合冻结：SpecDoc `4220195`（v3.73）；架构/原型/SP0–SP3/代码基线 `6a98629` |
| **合同锚** | `5241152`（`specs/sp8/spec.md` v1.4 ＋ 证据件） |
| **范围** | SpecDoc §23、§24；owner_sp 含 SP8 的附录 E 行 |
| **主责域** | `GOV`（治理面） |
| **评审** | `zzc-chatgpt56soltchangshark-2642`、`zzc-opus50tchangshark-2642`、`zzc-deepseekv4pro08130ca16c13-2642` |

> 本件定义 GOV 治理面的目标设计与 SP7b 迁移合同，不声称批准链、grant 签发、分发闸或迁移已实现。
> v1.5：把「配额 claim」与「执行许可」分开——grant_use 加 claimed/executing 生命周期、唯一 execution_owner、
> lease 与 fence_token；重放非终态只返 IN_PROGRESS 不发第二张 permit；崩溃走受栅栏接管或 unknown。
> v1.4：高危固定 dual＋second_factor 且按 signer 逐人判；claim 幂等与扣额分两支、revoke 覆盖三态；
> 跨库改为「同步权威 fence，否则只允许同库 UoW」；跨行条件移出行 CHECK 并各配对账。
> v1.3：补回 §23.5「范围」列（动作→档位）并做成全序可求值的 classify 函数，未命中 fail-closed 按高危。
> v1.2：把审批层级从单枚举拆成 `signature_count`＋`factor_requirement` 两个正交分量，三档逐档映射、次因子不再悬空。
> v1.1：按首轮评审补 GOV 目标 schema（D-SP8-12 §12.2/§12.2a）与 grant/challenge 状态机、原子 claim、
> 双签集合、revoke↔consume 竞态、跨 GOV/WRK 事务两形态（D-SP8-11 §11.1–§11.5）。
> 批准**主体**一律取 SP1 的 verified principal；本件不自造批准权，也不改 SP0 定义层语义。

## D-SP8-01 范围、边界与权威事实

### 1.1 GOV 治理面拥有

授权判定的**判据与记录**：审批两轴的判定结果、spec 预授权条目与其失效状态、运维预案承接关系、
渠道授权分级判定、平台域基线批次与签名、紧急放行记录与补审结论、effect grant 的签发/消费/吊销事实，
以及上述事实的受控 transaction、outbox 与审计载体。

### 1.2 不拥有

- spec 的准入、评审、规则版本语义：归 **SP0 定义层**（本件只在其产物上加授权判据）；
- principal、企业边界、渠道 assurance 的**认定**：归 SP1/IDN（本件只消费其结论）；
- 各 effect owner 的业务判据（WRK 的门与发版、CAP 的晋升与装卸、KNO 的条目语义等）：本件只定义其**批准与 grant 侧**约束；
- 紧急动作的**执行入口** `AGT-CAP-EmergencyAction`：归 AGT（SP6 范围），见 §12.4 挂账第 1 条；
- 企业库路由、RLS 与 migration runner：归 PLT/SP7a。

### 1.3 权威事实与 fail-closed

批准方、二次签方、紧急件发起人一律为 **verified principal**；角色字符串、Agent、session、显示名**都不是批准主体**，
Agent 只入 audit provenance。缺 AccessCtx、缺 grant、grant 过期/吊销/范围越界、批准台账不可取时，一律**拒绝**，
不得降级为「调用方自述已批准」。

## D-SP8-02 审批两轴：并存取严

### 2.1 两轴各管一件事

| 轴 | 决定 | 取值来源 |
|---|---|---|
| **预声明** | 审批**路径**（走谁、几步、是否需临时申请） | spec 预授权 / 运维预案中是否事先声明 |
| **敏感度与爆炸半径** | 审批**层级**（单签 / 次因子 / 重两签） | 凭据分级 S0/S1/S2 ＋ 数据分级 |

判定：有预声明 ⇒ 路径为项目管理方逐次判断；无预声明 ⇒ 路径升为业主授权；
**无论有无预声明，层级仍由敏感度与爆炸半径独立决定**；二者冲突取更严。

#### 2.1a 层级三档是两个正交分量，不是一个枚举〔首轮评审阻断的闭法〕

上一版把层级写成单枚举 `{single, second_factor, dual_sign}`，导致「**次因子**」在签数面**悬空**——
它根本不是"签几次"，而是"**认证强度**"。本版拆成两个正交分量，三档由二者组合表达，**无悬空值**：

| 分量 | 取值 | 谁给结论 | 语义 |
|---|---|---|---|
| `signature_count` | `single` / `dual` | 本件（批准链） | **签几个独立 principal** |
| `factor_requirement` | `single_factor` / `second_factor` | **SP1/IDN 的 assurance 面**（本件只消费结论，不定义认证方式） | **认证强度** |

| §23.1 的层级档 | ＝ `signature_count` | ＋ `factor_requirement` | 渠道可终结？ |
|---|---|---|---|
| 单签 | `single` | `single_factor` | 低危：可终结（＋一次性 challenge）|
| **次因子** | `single` | **`second_factor`** | 中危：**渠道可发起，须次因子确认后方可终结** |
| 重两签 | **`dual`**（两个独立合格 principal）| 🔴 **`second_factor`，且两名 signer 各自满足** | 高危：**渠道只能发起、不能终结**，须回强身份面 |

🔴 **两个分量各自取严，不得互相抵扣**：
**次因子不是重两签的替代品**——「已做次因子确认」**不得**用来把 `dual` 降回 `single`。
恒零：以次因子为由跳过重两签而生效的次数。
🔴 **高危固定 `dual` ＋ `second_factor`，且 assurance 按 signer 逐人判**——「回强身份面」是对**每个签字人**的要求，
不是对批次整体的一句话。恒零：**以 `dual` ＋ `single_factor` 终结高危的次数**；
负测：两名单因子 principal 联合签高危批次 → 必拒；一名达 `second_factor`、另一名未达 → 同样必拒。

### 2.2 反例（恒零）

🔴 **预声明不得被用于降低层级**——一句预声明把 S0/S1 访问降成低危，是本节要恒零的那件事。
`authorization_decision` 必须分别持久化 `path_axis`（预声明结论）与层级的**两个分量** `signature_count` / `factor_requirement`（见 §2.1a），
禁止合成一个 status；层级两列的输入只能是敏感度/爆炸半径与 assurance 结论，**schema 上不接受预声明作为其输入**。
负测：以「已预声明」为由把 S0 访问判为低危 → 必须拒绝并落账；人审面另判「预授权是否企图降低固有风险等级」。

## D-SP8-03 spec 预授权

### 3.1 六条规则的落法

预授权条目 `spec_preauth(spec_ref, spec_version_anchor, scope, purpose, deadline, review_ref, state)`：
须经评审（`review_ref` 非空）；**仅在产品定义后 ~ 上线前有效**；有效期内仍须项目管理方**逐次判断**（必要不充分）；
无预授权则须业主授权。

### 3.2 具体性判据（机器可判 ＋ 人审两层）

`scope`/`purpose`/`deadline` **三字段非空**，且 `scope`/`purpose` **不得含不可判定表述**
（`可能` / `视情况` / `相关` 等禁用词表随规则版本维护）；**笼统兜底视同无预授权**，按无预授权路径处理。
机器判：禁用词命中数与三字段空缺数恒零。人审判：范围划得够不够窄、用途陈述是否成立——两者不互相冒充。

### 3.3 与 SP0 的分工

`GOV-CMD-SubmitSpec` 与其评审产物由 **SP0** 定义，本件**只读消费** spec 版本与其锚；
spec 一改锚即变 ⇒ 预授权自动失去其绑定对象（见 D-SP8-04）。本件不改 SP0 的准入与评审语义。

## D-SP8-04 失效由机制触发

### 4.1 三类触发

| 触发源 | 事件 | 结果 |
|---|---|---|
| **收口** | 发版收口（WRK 第 ⑧ 格）同一事务 | 关联预授权条目置 `expired` |
| **版本变更** | `spec_version_anchor` 变化 | 绑定失效，须重新评审授权 |
| **时限** | `deadline` 到期 | 到期即 `expired`，不靠人清理 |

🔴 **粒度**：失效按「**预授权条目 × 其声明的作用域**」判定，**不得按 `task_id` 一刀切**——
子 spec 收口不得连带失效父 spec 下其他仍在研发期的子 spec。

### 4.2 消费侧

上线后仍援引已 `expired` 预授权者，**消费侧必须拒绝**（恒零判据）；拒绝理由与被援引条目一并落账，历史记录不改写。

## D-SP8-05 上线后由运维预案承接

### 5.1 阶段与授权文档

产品定义前 → 业主授权；定义后 ~ 上线前 → spec 预授权；**上线后 → 运维预案 / 生产访问规范**（知识库第 8 类）。
两者形式对称：**都须经评审、都不自动放行、都须具体到范围·用途·时限**；**交接点就是上线**。

### 5.2 交付面的授权侧判据（规则归 SP2，授权面归本件）

- **合入权**：`preprod`/`prod` 的合入权唯一属 ops；绕过该唯一合入权的次数恒零。**分支模型本身是 SP2 §8.1a 的规则，本件不改**，只定义"谁有权合入"的授权判定与落账；
- **发版记录**：七项记录缺项（含"文档有而评审/校验记录无"）的次数恒零；**七项清单归 SP2 §8.1b**，本件只判其**授权侧**：记录不齐即不得放行合入与发版授权。

## D-SP8-06 渠道授权分级与一次性挑战

### 6.1 四档：范围（动作→档位）与可达步骤，两列都要

上一版只保留了"档位→可达步骤"，**丢了"动作→档位"这一列**，导致来一个「租户级 skill 晋升」无从判级。本版补回并做成可求值的分类函数。

| 档位 | **范围（哪些动作属本档）** | 渠道可做到哪一步 |
|---|---|---|
| **查询** | 项目进展**只读**（须经受控仓储 API 过滤后返回） | 可信渠道即可 |
| **低危** | spec 预授权**范围内的一次读取**、**复盘定稿**、**L1 级 node 的门 PASS** | **可终结**（＋一次性 challenge）|
| **中危** | **单会话 provision**、**租户级 skill 晋升**、**跨项目读** | **可发起**，须**次因子确认**后方可终结 |
| **高危** | **全网批量下发**、**平台级 skill/MCP 晋升**、**跨人**、**S0/S1 凭据**、**平台域基线变更**、**L3 级 node 的门 PASS** | **只能发起，不能终结**；须回强身份面 ＋ **重两签** |

**分类函数 `classify(action) → level` 的判定顺序**（先命中先定，保证**全序、可求值**）：
1. 该动作是**门 PASS** ⇒ 层级**继承其 node 分级**（L1→低危 / L2→中危 / L3→高危，见 §6.2），不再往下匹配；
2. 命中**高危**范围枚举 ⇒ 高危；
3. 命中**中危**范围枚举 ⇒ 中危；
4. 命中**低危**范围枚举 ⇒ 低危；
5. 是**只读**且经受控仓储 API 过滤 ⇒ 查询；
6. 🔴 **兜底：以上都不命中 ⇒ 按【高危】处理（fail-closed），并登记为"待归档动作"进入下一批次补档**。
   —— 未知动作不得落进低危，也不得挂空；**"判不出来"本身不是放行理由**。

`authorization_decision` 持久化 `action_ref`、`level`（本函数输出）与 `classify_reason`（命中的是哪一条），
使**任一次授权都能复算其档位**；档位变更须新版本 ＋ `supersedes`，不原地改。

**⇒「渠道终结高危授权数恒零」因此对全部四档可求值**：对每一条"渠道终结"事实取 `classify(action)`，
`level='高危'` 即计数（恒零）；中危若在**无次因子确认**的情况下被渠道终结，计入同族恒零计数器
（"中危未经次因子确认即被渠道终结数"），不与高危混计。

恒零（本节）：**渠道终结高危授权数**、**§23.5 高危级操作未经重两签而生效数**、
**中危未经次因子确认即被渠道终结数**、**授权未绑一次性 challenge 数**。

### 6.2 门 PASS 的授权层级继承

🔴 **门 PASS 的授权层级 ＝ 该 node 的分级层级（L1→低危 / L2→中危 / L3→高危）**，
不因「只是打个勾」而变低。判定输入取 WRK 的 node 分级（只读消费，不改其语义）；
**L3 node 的门 PASS 不得由渠道终结**。恒零：门 PASS 授权层级与该 node 分级不符数。

### 6.3 一次性挑战（challenge）

授权动作必须绑定一次性凭据：平台生成 `challenge(digest_of_pending_item, expires_at)`，**单次性由 D-SP8-11 §11.1 的原子 CAS 保证**（不是一个布尔字段），
业主**回复该 challenge 而非自由文本**。三类必拒：**重放**（已消费的 challenge 再次出现）、
**摘要不符**（回复所绑摘要与当前待批事项不一致）、**过期**。
🔴 **「同意」两个字不构成授权**——可被重放、可被群内他人冒名、也可能是 Agent 把历史文本当成新指令。
恒零：授权未绑一次性 challenge 数。

## D-SP8-07 平台域基线变更的范围

### 7.1 §24.1 范围判定

范围含：平台级 skill / MCP / spec 模板的**晋升与变更**、**immutable 名单变更**、
**D0 平台级条目及其可见性策略变更**、**C1 原语面语义变更**。
落在范围内者一律走 §24 的批次审批形态；范围外不得借道本通道取得批准。

### 7.2 与 SP3 的接界

`D0 降 public` 或平台域基线可见性策略变更**必须走 §24**；
visibility 的语义与降级审批规则归 **SP3 §11.4b**，本件不改，只判"该变更是否落入平台域基线范围、是否取得对应批准"。
恒零：降级未经该归属域 owner/ops 批准数；**D0 降 public 未走 §24 数**。

### 7.3 企业注册的配置完整性

企业注册的五项配置**缺任一项即不得视为注册完成**（恒零：缺项即视为注册完成的次数）；
未完成注册的企业不得取得任何 grant，其治理面操作一律 fail-closed。

## D-SP8-08 审批形态、签数与批准决策屏

### 8.1 形态

| 环节 | 谁 | 产物 |
|---|---|---|
| 整理汇总 | ops（平台级） | 批次清单：每项含**谁提的 / 为什么 / 影响面**（影响面用 SYNC IMPACT REPORT 格式） |
| 集中审批 | **平台管理者**（角色，区别于 ops） | 对整批签一次，签数按 8.2 定 |

**粒度：批为审批单位，项为追溯单位**；批次 `manifest` 不可变、逐项可追溯；**签名覆盖 manifest 摘要**，不覆盖"一堆事"。

### 8.2 签数由批内最高危项决定

| 批次内容 | 签数 |
|---|---|
| 全部为普通项（未列入 immutable 名单且不改变语义的文案修订） | 平台管理者**单签** |
| **含任一高危项**（平台级 skill/MCP 晋升任何形式 / immutable 名单变更 / S0·S1 凭据 / 跨人 capability / 全网批量下发 / C1 原语面语义变更） | **整批升重两签**（两个**独立** principal） |

🔴 **取批内最高危项的要求，不取多数、不取平均**；判据是「**是否在 immutable 名单内 ＋ 是否属高危枚举**」，**不举具体件为例**。

🔴 **为什么批次面只有两档、而层级有三档**（与 §2.1a 的对应，避免读成"少了一档"）：
`signature_count` 只有 `single`/`dual` 两值，批次签数表因此只有两行；**「次因子」是 `factor_requirement`，不是签数**，
它作用在**授权动作的认证强度**上（§23.5 中危：渠道可发起、须次因子确认），**不改变批次要签几个人**。
§24.1 范围内的项非"普通"即"高危"：**中危项若进入批次，按普通项计签数**（除非命中高危枚举，则整批升 `dual`），
而其**次因子要求随该动作本身走，不因进了批次而消失**。⇒ 三档全部有落点，无悬空。
拆批不得成为降低审批强度的手段：拆分后的每一批各自按本规则定签数，**拆批本身不降级**。
恒零：批次含高危项而仅单签通过数。人审面另判：高危项认定是否遗漏、批次是否把互不相关或不同风险项捆绑以稀释逐项审查。

### 8.3 批准决策屏（原型 `s1` 的治理面）

`s1` typed 待决队列是**批准决策实际发生的屏**。本件在其上定义：
批准方 principal（须与执行者分离）、**challenge 绑定**（D-SP8-06）、**二次签**（高危批次）、
拒绝语义（`FORBIDDEN` 与 `NOT_FOUND_OR_HIDDEN` 不混用）、以及每次批准/拒绝的**落账**（批次 ref、manifest 摘要、签名、时刻）。
⚠️ `spec_fix` / `spec_claim` 两类条目归 **SP0**，本件**只读消费、不改其语义**。
`s8` 平台治理屏承载批次列表与签数状态，其读模型只反映本节判据，不另算。

## D-SP8-09 紧急件与单调收紧

### 9.1 事后 flag 制

安全召回、凭据撤销、下架出问题的平台级件等不了批次周期 ⇒ **单独放行、事后补入批次**。
🔴 **不新造紧急通道**——新造一条紧急通道，就是新造一条绕过点。

### 9.2 判据：capability effect，不看动作名

**允许**（须逐项通过判据）：`disable` / `quarantine` / `revoke`，且**须有 fail-closed fallback**；
吊销凭据、缩短有效期；收紧或删除白名单条目；把件**加入** immutable 名单；提高门的强度。
🔴 **禁止**：安装或升级任何制品；签发凭据、延长有效期；新增或放宽白名单条目；把件**移出** immutable 名单；降低门的强度或跳过门。
🔴 **`uninstall` 不在允许列**——它可能移除安全控制。
**唯一判据**：该动作**在任何情形下都不可能扩大任何主体的权限或可达范围**才允许走紧急通道；**存疑即按非紧急处理（fail-closed）**。

### 9.3 落账、补审与恒零

紧急放行**逐次落账**（发起人、事实依据、时刻）；**必须进入下一批次补审**，补审否决时须给出回退动作。
恒零：紧急通道中出现放宽类动作的次数、`uninstall` 走了紧急路径的次数、紧急放行未进入下一批次补审数。
建成核验：紧急通道枚举只含 `disable`/`quarantine`/`revoke`，且执行前校验 fail-closed fallback 存在。
人审面：某次处置的 capability effect 判断是否成立、放行是否真的只减不增。

## D-SP8-10 分发面校验批准与职责分离

### 10.1 分发前校验台账

分发前必须校验：`target` ＋ `version` 在对应表中且状态为「**已批准**」，否则**拒发**。
🔴 **批准与执行分处两个服务、两套写权**——同一 principal 不得既批准又执行。恒零：未经批准即下发的制品数。

### 10.2 §24.5 的其余硬性要求

1. 保留五道闸：来源鉴权 / sha256 完整性 / host 白名单 / 动作白名单 / **不执行制品内脚本**；
2. **只下发数据，不下发指令**——更新逻辑固化在接收端，下发方无法改变它；
3. 版本号**只在收到接收端 `ack(ok=true)` 后推进**（下发成功 ≠ apply 成功）；
4. 🔴 **管理面必须有角色模型**：`provision` / `api-keys` / `config` 三类高危端点与只读端点**分权**；
5. 🔴 **范围闸**：单会话下发 ＝ 平台 ops 单签；**全网批量 ＝ 重两签 ＋ 业主知会**；
6. 装卸对称（规则归 CAP/SP4，本件只判其**批准侧**对称：装与卸同级同期审批）。

原型 `s8r` 管理面角色模型屏承载第 4 条：角色与端点的授权矩阵、以及**分权是否真实**的负测入口。
建成核验：管理面存在角色模型；**只读角色调用 `provision`/`api-keys`/`config` 必须被拒**（负测）；分发面不执行制品内脚本。

### 10.3 职责分离与审计不可篡改

同一分离原则向下延伸到运行介质，均属本件的**授权面**判据（对应恒零计数器）：
- 审计记录必须带 `dw.actor`，能回答"哪个 Agent 干的"；
- `dw_owner` **属主须单点**，企业 ops 之外不得持有；
- 企业 ops 执行 `NO FORCE` / `DROP POLICY` / `ALTER POLICY` / `OWNER TO` 须留痕并计数；
- 企业 ops 以 `dw_owner` 身份执行 DML 未留痕的次数恒零（属主读写＝访问数据，非运维）；
- **审计落点不得位于企业 ops 有写权的介质上**（否则可连审计一起删）；
- 平台 ops 持有的备份 / 只读副本 / 逻辑复制槽**未经介质加密或访问审批**的数恒零；
- CI workflow 的**触发面必须覆盖闸的载体**（`scripts/`、`specs/gates/`），配置漏覆盖数恒零。

## D-SP8-11 GOV 接口、grant 状态机与并发合同

### 11.1 三个状态机（先定状态与原子转换，再谈接口）

**challenge**：`issued → consumed | expired | superseded`
- 唯一键 `(pending_ref, challenge_id)`；`digest` 绑定待批事项摘要；`expires_at` 必填；
- **消费是一次原子 CAS**：`UPDATE challenge SET state='consumed', consumed_by=:p, consumed_at=now() WHERE challenge_id=:c AND state='issued' AND expires_at>now() AND digest=:d` —— **受影响行数为 0 即拒绝**（重放／过期／摘要不符三类在同一条谓词里判掉，不靠先查后改）；
- 同一 `pending_ref` 重新签发 challenge 时，旧条目 CAS 置 `superseded`，不留两个可用凭据。

**effect_grant**：`issued → (claim…) → exhausted | revoked | expired`
- **单次与限次统一表达**：`use_limit INT NOT NULL DEFAULT 1 CHECK(use_limit>=1)`、`used_count INT NOT NULL DEFAULT 0 CHECK(used_count<=use_limit)`；
  `remaining = use_limit - used_count`；**不再有 `single_use` 布尔**（`single_use` ≡ `use_limit=1`），消除上一版"宣称限次、形状只有布尔"的自相矛盾；
- **绑定面**（缺一不可）：`enterprise_id`、`audience`（消费主体：principal 或受控 service identity）、`effect_target_digest`（待执行 effect 的规范化摘要）、`scope`、`expires_at`、`generation`；
- 🔴 **配额 claim ≠ 执行许可**（上一版把两者混为一谈：做到了"不重复扣额"，没做到"不重复执行 effect"）。
  `grant_use` 因此有**自己的生命周期**与**唯一执行权**：

  `claimed → executing → succeeded | failed | unknown`，另有 `claimed → aborted`（被吊销且未发起，终态、无 effect）
  列：`state`、`execution_owner`（唯一执行权持有者）、`fence_token BIGINT`（单调，接管即 +1）、
  `lease_expires_at`、`payload_digest`、`effect_result`、`outcome_ref`。

- **消费路径分两支：先判是否新 claim，再决定扣额与是否发执行许可**：

```sql
-- ① 尝试登记 claim（payload_digest 为消费侧规范化载荷的摘要）
INSERT INTO grant_use(grant_id, claim_key, payload_digest, claimed_by,
                      state, execution_owner, fence_token, lease_expires_at)
VALUES (:g, :k, :d, :p, 'claimed', :owner, 1, now() + :lease)
ON CONFLICT (grant_id, claim_key) DO NOTHING
RETURNING use_id, fence_token;
```

- **① 返回行 ⇒ 新 claim**：同事务内原子扣额，**达上限在同一语句内置 `exhausted`**：
```sql
UPDATE effect_grant
   SET used_count = used_count + 1,
       state = CASE WHEN used_count + 1 >= use_limit THEN 'exhausted' ELSE state END
 WHERE grant_id = :g AND state = 'issued'
   AND used_count < use_limit AND expires_at > now() AND generation = :gen
RETURNING used_count, state;
```
  影响行数为 0 ⇒ **整笔回滚**（含 ①），返回 `GRANT_UNAVAILABLE`。
  提交成功后**仅向该 `execution_owner` 返回 execute permit**（含 `use_id` ＋ `fence_token`）——
  🔴 **permit 是唯一执行凭据**，且**permit 不是一张长期通行证**：发起 effect 前必须过下面这道 CAS，
  它同时校验**持证人**与**授权在发起时刻仍然有效**：

```sql
UPDATE grant_use gu
   SET state = 'executing', lease_expires_at = now() + :lease
 WHERE gu.use_id = :u
   AND gu.execution_owner = :owner          -- 持证人
   AND gu.fence_token   = :t                -- 未被接管/未被吊销 bump
   AND gu.state         = 'claimed'
   AND gu.lease_expires_at > now()          -- 租约未过期
   AND EXISTS (SELECT 1 FROM effect_grant g  -- 🔴 发起时刻授权仍有效
                WHERE g.grant_id = gu.grant_id
                  AND g.state IN ('issued','exhausted')   -- 已扣额者仍可执行本次
                  AND g.revoked_at IS NULL
                  AND g.expires_at > now()
                  AND g.generation = :gen)
RETURNING gu.use_id;
```
  **影响 0 行 ⇒ 一律不得发起 effect**（返回 `PERMIT_INVALID`）。
  ⇒ 「claim 时有效、execute 时已被吊销/过期/代际变更」这一格由 `EXISTS` 子句堵死，**旧 permit 不再是通行证**。

- **① 无返回（冲突）⇒ 重放**：读既有行，先比 `payload_digest`：
  - **载荷不同** ⇒ `IDEMPOTENCY_CONFLICT`（不扣额、不覆盖、不发 permit）；
  - **载荷相同且 `state` 为终态** ⇒ 返回**既有 outcome**，不扣额、**不发 permit**；
  - 🔴 **载荷相同且 `state` 非终态**（`claimed`/`executing`）⇒ 返回 **`IN_PROGRESS`（附 `retry_after`）**，
    **既不扣额，也绝不授权第二次 effect**——这正是上一版缺的那一格：两个重放者据同一 claim 各执行一次副作用。

- **崩溃与接管（lease ＋ fencing）**：`lease_expires_at` 过期后，该 claim **不会自动作废**（配额已扣、effect 可能已发生）：
  - 若该 effect 在 SP2/SP4 侧被**声明为可对账**（有 reconcile 句柄）⇒ CAS 接管：
    `UPDATE grant_use SET execution_owner=:new, fence_token=fence_token+1, lease_expires_at=now()+:lease
     WHERE use_id=:u AND state IN ('claimed','executing') AND lease_expires_at < now()`，
    接管者拿到**新 fence_token**；**旧 owner 的任何回填一律按 fence 校验拒绝**（`FENCED_OWNER`）；
    接管者**先对账再决定是否执行**，🔴 **不得盲目重执行**；
  - 若该 effect **不可对账** ⇒ 直接置 `unknown` 并进 reconcile；**永不自动重发**。
  ⇒ 「claim 后崩溃永久占配额且无恢复状态」因此不再存在：要么被受栅栏接管，要么落 `unknown` 由 reconcile 收敛。

- **revoke 与非终态 claim 交叉**：吊销后 **`claimed`/`executing` 的 claim 一律不得取得新的 permit**；
  该 claim 转入 **rollback/reconcile 义务**（与 §11.3 的 `revoked_after_use` 同一路径），
  已在途的执行由其 owner 按 fence 回填结果，回填后仍计回退义务。

⇒ 三个不变量：**同一 `claim_key` 至多一次扣额** · **同一时刻至多一个有效 `execution_owner`** ·
**任何时刻至多一条执行路径持有 permit**（fence_token 单调保证旧路径被隔离）。

**authorization_decision**：`draft → decided(approved|rejected)`，`path_axis` 与层级两分量 `signature_count`／`factor_requirement` 分列（D-SP8-02 §2.1a），决定后不可改，更正走新版本 ＋ `supersedes`。

### 11.2 重两签：签名是集合，不是一个字段

高危批次的批准由 **`batch_signature` 集合**承载，不是 `signature_ref` 单值（上一版的形状错误）：
- 唯一键 `(batch_id, signer_principal)` ⇒ **同一 principal 不能凑两签**；
- 有效性判据：`count(distinct signer_principal) >= required_signatures`，且每个 signer 均为**合格批准主体**
  （verified principal、具该责任域批准权、**不是该批次的提出者或执行者**）；
- `required_signatures` 由批内最高危项决定（D-SP8-08），写入 `batch` 行并随 manifest 摘要一起被签名覆盖；
- 未达签数前 `batch.state` 不得进入 `approved`；grant 只能由已 `approved` 的批次派生。

### 11.3 claim → effect → result 的幂等与未知路径

1. 消费域先 **claim**（11.1 原子块）→ 仅 `execution_owner` 拿到 **execute permit**（`use_id` ＋ `fence_token`）；
2. 发起前 CAS `claimed→executing`；执行后**按 fence 回填** `effect_result`（`succeeded|failed|unknown`）与 `outcome_ref`——
   `UPDATE … WHERE use_id=:u AND fence_token=:t AND state='executing'`，fence 不符即 `FENCED_OWNER` 拒绝；
3. **`unknown` 不猜成功**：进入 reconcile，由消费域按其既有 reconcile 语义收敛（WRK/CAP 各自已有），GOV 侧只保留 claim 事实与最终回填；
4. **revoke 必须覆盖三态**（上一版只写 `WHERE state='issued'`，把"部分已用/已耗尽"漏在外面）：

| 吊销时的实际态 | 判别 | CAS 与记录 | 后续义务 |
|---|---|---|---|
| **未用** | `state='issued' AND used_count=0` | `UPDATE … SET state='revoked' WHERE grant_id=:g AND state='issued' AND used_count=0 AND generation=:gen` | 无 effect 发生，仅落账 |
| **部分已用** | `state='issued' AND used_count>0` | 同上但条件 `used_count>0`；`grant_revocation` 记 `prior_state`、`used_count_at_revoke`、`revoked_after_use=true` | 🔴 **消费域必须给出回退动作**（与紧急通道补审否决同一路径），不得静默丢弃 |
| **已耗尽** | `state='exhausted'` | `WHERE state='exhausted' AND generation=:gen` ⇒ `revoked` | 同上；**"已耗尽"不等于"不用管"** |

   - revoke 先成功 ⇒ 后续新 claim 的条件更新影响 0 行 ⇒ 拒绝（重放分支仍返回既有 outcome，不复活配额）；
   - claim 先成功、revoke 后到 ⇒ **不回滚已发生的 effect**，按上表落 `revoked_after_use` 并挂回退义务。

🔴 **revoke 必须在同一事务内处理"已发出但未执行"的 permit**，否则旧 permit 仍能启动 effect：

```sql
-- 同一事务，顺序执行
UPDATE effect_grant SET state='revoked', revoked_at=now() WHERE grant_id=:g AND generation=:gen;
UPDATE grant_use SET state='aborted', fence_token=fence_token+1, lease_expires_at=now()
 WHERE grant_id=:g AND state='claimed';        -- 尚未发起：直接作废并 bump，旧 permit 立即失效
UPDATE grant_use SET revoke_pending=true
 WHERE grant_id=:g AND state='executing';      -- 已在途：不 bump（否则其合法回填会被 FENCED），改挂回退义务
```
- `claimed` ⇒ `aborted`（**终态，无 effect 发生**）：其旧 permit 因 `fence_token` 变化与 `state≠'claimed'` **双重失效**，
  上面的发起 CAS **必然影响 0 行**；
- `executing` ⇒ 保留其 fence 以接受**真实结果回填**（事实不能被抹掉），回填后按 `revoked_after_use` 挂**回退义务**。
🔴 恒零：**「claim 后、execute 前发生 revoke，而旧 permit 仍启动了 effect」的次数**。

### 11.4 跨 GOV/WRK 的「同一事务」：outbox 不能当判定依据

SpecDoc 要求发版收口与预授权失效「同一事务」。上一版给的形态 B（transactional outbox ＋ 幂等消费）
**证明不了「不存在收口后预授权仍可用的窗口」**——outbox 只保证**最终**投递；
WRK 已提交 closeout 而 GOV 尚未消费的那段时间里，GOV 既看不到 closeout 事实、也还没有 `preauth_expiry` 行，
读侧的「fail-closed」**根本没有触发条件**。这是上一版的实质错误，本版改掉。

**形态 A（默认，且是唯一无条件允许的形态）**：收口与 `spec_preauth` 置 `expired` 在**同一 DB 事务**内提交。

**形态 B（跨库，仅在同时满足下列条件时允许）**：
1. 🔴 **判定依据不是 outbox，而是同步权威 fence**：授权读取路径在**判定时刻**对 WRK 侧做**可线性化**查询
   （`closeout_exists(spec_ref, version_anchor)` 或读取权威 `closeout_high_water`），**以该结果为准**；
2. 🔴 **fence 查询失败、超时或结果不确定 ⇒ fail-closed 拒绝授权**，不得回退到「本地没看到 expiry 就放行」；
3. outbox 仅用于**补偿落账**（补齐 `preauth_expiry` 行，供审计与对账），**不作为判定依据**；
4. 对账恒零：存在 closeout 事实但缺 `preauth_expiry` 行且已超过补偿 SLA 的条目数。

> **等价的第三条路**（若两库确实无法同步查询）：**closeout 侧延后可见**——
> WRK 的 closeout 在收到 GOV 失效 ack 之前对外不可见。该路把窗口挪到 WRK 侧并由其承担，
> 需 SP2 同意并登记，**本件不单方指定**。

负测：**注入 outbox 延迟窗口**（closeout 已提交、消费未落地）时发起授权 ⇒ **必须被拒**；
fence 查询超时 ⇒ 必须被拒；补偿落账后重放同一授权 ⇒ 仍按已失效处理。

### 11.5 四接口逐项合同

| ID | schema | effect / tx | auth | idempotency / generation | error | audit |
|---|---|---|---|---|---|---|
| `GOV-CMD-IssueEffectGrant` | in: `approval_ref`(batch ＋ 签名集合)、`audience`、`effect_target_digest`、`scope`、`use_limit`、`expires_at`；out: `grant_id`＋`generation` | 单事务写 `effect_grant`＋审计＋outbox；**仅当批次 `approved` 且签数达标**才允许 | 批准主体 principal；签发者 ≠ 执行者 | `request_id` 唯一；grant `generation` 单调 | `FORBIDDEN` / `SCOPE_EXCEEDED` / `BATCH_NOT_APPROVED` / `IDEMPOTENCY_CONFLICT` | 签发、每次 claim、吊销各自落账 |
| `GOV-CMD-DecidePending` | in: `pending_ref`、`challenge_id`＋回复、`decision`、`signer`；out: 决定记录 ＋ 批次签名行 | 同事务：challenge CAS ＋ 写 `batch_signature` ＋ 决定记录；达签数则批次转 `approved` | 批准方 ≠ 执行者；同一 principal 只计一票 | `request_id` 唯一；`(batch_id, signer_principal)` 唯一 | `CHALLENGE_INVALID`（重放/过期/摘要不符）/ `FORBIDDEN` / `DUPLICATE_SIGNER` | 每票与拒绝理由落账 |
| `GOV-CMD-SubmitSpec` | **seam（语义归 SP0）**：只读消费 spec 版本与锚 | 不在本件事务内 | 不改其 auth | 本件只按 `spec_version_anchor` 判绑定是否仍有效 | 规则缺失 ⇒ 本件 fail-closed 拒绝授权 | 引用关系落账 |
| `GOV-QRY-ResolveGateSet` | **seam（语义归 SP0）**：只读消费规则版本 | 只读 | 不改其解析语义 | 规则版本进入层级两分量的判定输入 | 版本缺失/撤销 ⇒ 拒绝授权 | 判定输入落账 |

### 11.5a 消费侧合同（对各 effect owner，含挂起中的 SP4）

`approval_ref` 交付形状：
`{grant_id, generation, enterprise_id, audience, scope, effect_target_digest, use_limit, remaining, expires_at, batch_ref, signature_set_ref}`
（**无 `single_use` 布尔**；单次 ≡ `use_limit=1`；`signature_set_ref` 指向签名**集合**，不是单个签名）。

消费侧**必须独立复算**，任一不过即拒绝并落账：
1. 签名集合可验、且含 **≥ `required_signatures` 个互不相同的合格 principal**；
2. `audience` 与 `enterprise_id` 与本次执行主体一致；
3. `scope` 包含本次 effect，且 `effect_target_digest` 与待执行 effect 的规范化摘要**逐位相同**；
4. 未过期、未吊销；
5. **通过原子 claim 取得配额并拿到 execute permit**（§11.1），不是"读一下 remaining 再执行"；
6. 🔴 **无 permit 不得发起外部 effect**：重放拿到 `IN_PROGRESS` 时**只能等待或按 `retry_after` 重试查询**，
   **不得据同一 claim 再执行一次**；lease 过期后的接管须持**新 `fence_token`**，且**先对账再决定是否执行**。

🔴 **不接受"上游说批了"**；🔴 **新增消费域自动适用本合同，无需改本件**。
消费失败或效果 `unknown` 时按 §11.3 走，GOV 侧不替消费域猜结果。

### 11.6 事件面

冻结架构 §4.2 中 owner=GOV 的事件实算为 **0**，本件**不产出** `EV-GOV-*`；批准结论经 grant 与各域既有事件面传播。
未来若需 GOV 作为 producer，须先改目标架构 §4.2 并重走评审。

## D-SP8-12 复用、GOV 数据与追踪验收

### 12.1 现状复用映射

| 现状单元 | 状态 | 目标处置 | conformance |
|---|---|---|---|
| 现有审批消息流（群内自由文本） | **retire** | 一律走 challenge ＋ typed 待决队列 | 自由文本"同意"必须被拒 |
| 现有分发脚本的五道闸 | **retain** | 原样保留，纳入本件校验链 | 五闸各一正一反例 |
| 台账/批准记录载体 | **rewrite** | 企业库/平台库受控表 ＋ 不可变 manifest | 越权写、迟到批准、代际丢弃 |
| 角色模型（管理面） | **new** | 端点×角色授权矩阵 ＋ 负测 | 只读角色调高危端点必拒 |
| grant 签发与消费 | **new** | 本件 §11.3 合同 | 范围/时限/单次/吊销四类负测 |
| CI 闸触发面配置 | **adapt** | 覆盖 `scripts/` 与 `specs/gates/` | 漏覆盖即红 |

### 12.2 GOV 治理面目标 schema（最小集，逐表可判）

所有表驻**平台库**（治理面是平台自身事实）；含企业维度的行带 `enterprise_id`，
人层表带 `owner_principal`，**应用显式谓词为主守、RLS 为漏谓词兜底**；事务由 SP7a `RunInAccessTx` 提供。
时间列一律 DB 生成 UTC；`*_ref` 一律锚（id ＋ sha256），不是路径。

| 表 | PK | FK | unique | check / 枚举 | index | 不可变性 |
|---|---|---|---|---|---|---|
| `spec_preauth` | `preauth_id` | `spec_ref`→SP0 只读锚 | `(spec_ref, spec_version_anchor, scope_digest)` | `state ∈ {active,expired,revoked}`；`scope/purpose/deadline` NOT NULL 且非空串 | `(state, deadline)`、`(spec_ref)` | 正文不可改；更正新版本 ＋ `supersedes` |
| `preauth_expiry` | `expiry_id` | `preauth_id` | `(preauth_id, trigger_kind, trigger_ref)` | `trigger_kind ∈ {closeout,version_change,deadline}` | `(preauth_id)` | append-only |
| `authorization_decision` | `decision_id` | `subject_ref`、`preauth_id?` | `(subject_ref, request_id)` | `path_axis ∈ {preclaimed,owner_auth}`；**`signature_count ∈ {single,dual}`**；**`factor_requirement ∈ {single_factor,second_factor}`**（两列，非单枚举）；`outcome ∈ {approved,rejected}` | `(subject_ref)`、`(decided_at)` | decided 后不可改 |
| `challenge` | `challenge_id` | `pending_ref` | `(pending_ref, state) WHERE state='issued'`（同一待批项同时只允许一个可用凭据） | `state ∈ {issued,consumed,expired,superseded}`；`expires_at` NOT NULL | `(state, expires_at)` | 状态只前进，不回退 |
| `batch` | `batch_id` | —— | `manifest_digest` | `state ∈ {draft,pending,approved,rejected}`；`required_signatures ∈ {1,2}` | `(state)` | `manifest` 冻结后不可改 |
| `batch_item` | `(batch_id, item_seq)` | `batch_id` | `(batch_id, item_ref)` | `risk ∈ {normal,high}`；`impact_report_ref` NOT NULL | `(batch_id)` | append-only |
| `batch_signature` | `signature_id` | `batch_id` | **`(batch_id, signer_principal)`** | 行级 CHECK 只约束本行（签名格式、`manifest_digest` 非空、`assurance_ref` 非空）；**`signer ≠ proposer` 与「签数达标」是跨行/跨表条件，归 §12.2b，不由行 CHECK 承担** | `(batch_id)` | append-only |
| `effect_grant` | `grant_id` | `batch_id`、`enterprise_id` | `(batch_id, audience, effect_target_digest)` | `state ∈ {issued,exhausted,revoked,expired}`；`use_limit>=1`；`used_count<=use_limit`；`expires_at` NOT NULL | `(state, expires_at)`、`(audience)` | 签发后 scope/audience/target 不可改 |
| `grant_use` | `use_id` | `grant_id` | **`(grant_id, claim_key)`** | `state ∈ {claimed,executing,succeeded,failed,unknown,aborted}`；`fence_token>=1`；`lease_expires_at` NOT NULL；**一致性**：`state ∈ {claimed,executing,aborted} ⇒ effect_result IS NULL AND outcome_ref IS NULL`；`state ∈ {succeeded,failed,unknown} ⇒ effect_result = state` | `(grant_id)`、`(state, lease_expires_at)`、`(effect_result)` | 🔴 **不是 append-only，是受控单调状态更新**：普通角色无 `UPDATE` 权，只有受控写路径可改；状态只能按 §11.1 的转换图前进（触发器或写路径断言），每次变更 append 一行 `grant_use_history` |
| `grant_use_history` | `history_id` | `use_id` | `(use_id, seq)` | `from_state`/`to_state` 属状态枚举且为合法转换；`actor`、`fence_token`、`reason` NOT NULL | `(use_id, seq)` | **append-only（真正的 append-only）** |
| `grant_revocation` | `revocation_id` | `grant_id` | `(grant_id)` | `reason` NOT NULL；`revoked_after_use` bool | `(grant_id)` | append-only |
| `emergency_release` | `release_id` | `grant_id?` | `(action_ref)` | `action ∈ {disable,quarantine,revoke}`；`fallback_ref` NOT NULL | `(reviewed)` | append-only |
| `emergency_review` | `review_id` | `release_id`、`batch_id` | `(release_id)` | `outcome ∈ {upheld,overturned}`；`overturned ⇒ rollback_action_ref` NOT NULL | `(batch_id)` | append-only |
| `ops_role_binding` | `binding_id` | `principal` | `(principal, endpoint_class)` | `endpoint_class ∈ {provision,api_keys,config,readonly}` | `(endpoint_class)` | 变更走批次审批 |
| `gov_audit` | `audit_id` | 各事实 ref | `(source_table, source_id, event_seq)` | `dw_actor` NOT NULL（回答"哪个 Agent 干的"） | `(recorded_at)` | **append-only，且落点不得位于企业 ops 有写权的介质** |
| `gov_outbox` | `outbox_id` | 各事实 ref | `(aggregate_ref, event_seq)` | `state ∈ {pending,delivered}` | `(state)` | append-only |

**跨表不变量**（实现须由约束或受控写路径保证，不靠调用方自觉）：
1. `effect_grant` 只能由 `batch.state='approved'` 且 `count(batch_signature) >= required_signatures` 的批次派生；
2. `grant_use` 行数 ＝ `effect_grant.used_count`（对账判据）；
2a. 🔴 **任一 `grant_use` 在任一时刻至多一个有效 `execution_owner`**；非终态且 lease 过期者必须要么被受栅栏接管、要么置 `unknown`——恒零：**「非终态且 lease 过期超过 SLA 仍无接管也无 `unknown`」的 claim 数**；
3. `emergency_release` 必有对应 `emergency_review`，否则计入"未进入下一批次补审"恒零计数器；
4. 任一 `challenge` 消费必对应一条 `authorization_decision`；
5. `preauth_expiry` 存在 ⇒ 该 `preauth` 的读侧一律按 `expired` 处理（D-SP8-11 §11.4 的读侧 fail-closed）。

### 12.2c 「append-only」与「状态机」不可并存，本件的分工

上一版把 `grant_use` 标成 append-only，同时又给它 `claimed→executing→终态` 的状态机、lease 接管改 owner/token/expiry、
结果回填——**这三件事都要 UPDATE，与 append-only 直接矛盾**（sol 指出，成立）。本件的分工：

| 对象 | 可变性 | 手段 |
|---|---|---|
| `grant_use` | **受控单调状态更新** | 普通角色**无 `UPDATE` 权**；只有受控写路径/函数可改；转换合法性由触发器或写路径断言（非终态→终态、终态不可再改）；`fence_token` 单调不减 |
| `grant_use_history` | **append-only** | 每次状态变更写一行（`from_state`/`to_state`/`actor`/`fence_token`/`reason`/时刻），**它才是不可篡改的那一份** |
| `gov_audit` | **append-only** | 同上，且落点不得在企业 ops 可写介质 |

⇒ 「不可篡改」由**历史表 ＋ 审计**承担，「当前态」由**受控单调更新**承担；
🔴 **两者分工不得互换**——把当前态写成 append-only 会逼实现方要么违约、要么把状态塞进别处。
一致性对账（恒零）：`grant_use` 的当前态与其 `grant_use_history` 末行 `to_state` 不一致的行数；
非终态却有 `effect_result` 的行数；终态却与 `effect_result` 不匹配的行数。

### 12.2b 跨行/跨表条件由受控写路径保证，不冒充行级 CHECK

PG 的行级 `CHECK` **看不到别的行、也看不到别的表**，故下列条件**不得**写成 CHECK
（上一版把 `signer ≠ proposer` 放进 CHECK 列，是形状错误）：

| 条件 | 强制手段（写路径 / 触发器，二选一但须明写选哪个） | 兜底对账（恒零） |
|---|---|---|
| `signer_principal ≠ batch.proposer` | 同事务 `SELECT proposer FROM batch WHERE batch_id=:b FOR SHARE` 后断言；或 `AFTER INSERT` 触发器校验并 `RAISE` | 存在 `signer = proposer` 的签名行数 |
| 同一 principal 不得凑两签 | **唯一键 `(batch_id, signer_principal)`**——这一条确实是可靠约束 | —— |
| `count(distinct signer) >= required_signatures` 才能进 `approved` | 条件更新：`UPDATE batch SET state='approved' WHERE batch_id=:b AND state='pending' AND (SELECT count(*) FROM batch_signature WHERE batch_id=:b) >= required_signatures`，**影响 0 行即拒**；或触发器推进 | 处于 `approved` 但签名数不足的批次数 |
| 高危批次每名 signer 各自达 `second_factor` | 写路径校验 SP1 assurance 结论并落 `assurance_ref` | 高危批次中 `assurance` 未达标的签名行数 |
| `effect_grant` 只能由已 `approved` 且签数达标的批次派生 | 签发路径同事务复算上述条件 | 来源批次非 `approved` 的 grant 数 |

🔴 **约束、触发器、对账三者分工不得互相冒充**：能用唯一键的用唯一键；跨行的写路径或触发器兜；
无论选哪种，**对账查询都要有**——它是"实现是否真按写路径走"的唯一事后证据。

### 12.2a SP7b 五列：逐对象对应上表

| 被测对象 | 输入 | 期望 | 证据形态 | 验收门 |
|---|---|---|---|---|
| GOV 治理面 DDL（上表全部） | 空平台库 ＋ schema version | 表/PK/FK/unique/check/index/RLS policy 齐；append-only 表无 UPDATE 权 | schema dump ＋ 越权写负测 | SP7a conformance ＋ SP8 delivery |
| `spec_preauth` backfill | 现有 spec 与其锚 | 三字段齐、禁用词 0、state 正确 | before/after keyed diff | semantic migration gate |
| `batch`/`batch_item`/`batch_signature` 迁移 | 现有审批记录 | manifest 不可变；签名集合唯一键成立；单人凑双签被拒 | manifest ＋ 验签报告 ＋ 负测 | 平台治理 review |
| `effect_grant`/`grant_use` cutover | generation fence ＋ 并发 claim 用例 | 新写只入受控表；**并发 claim 只有一个成功**；超限/过期/已吊销必拒 | mutation ＋ 并发负测 | release approval |
| `gov_audit` 介质与属主 | 现有审计落点 | 落点不在企业 ops 可写介质；`dw_actor` 非空；属主单点 | 介质权限清单 ＋ 负测 | ops ＋ SP8 gate |
| rollback | 备份 ref ＋ cutover point | 可恢复旧读或前滚，不丢批准、签名与审计 | rehearsal report | ops ＋ SP8 gate |

### 12.3 证据出口

- coverage checker：来源集合 / 实检集合 / 未知集合；
- 授权两轴负测：预声明降层级必拒；**以「已做次因子确认」为由把 `dual` 降为 `single` 必拒**；
  三档映射用例：低危(single+single_factor) / 中危(single+second_factor，渠道须次因子后方可终结) / 高危(dual，渠道不得终结)；
- 预授权：禁用词与三字段空缺扫描、失效三触发用例；
- challenge：重放 / 摘要不符 / 过期三类负测；
- 签数：含高危项批次单签必拒、**同一 principal 凑双签必拒**、拆批不降级用例；
- 并发：**两个消费者同时新 claim，只有一个扣到最后一份配额**；**同 `claim_key` ＋ 同载荷重放 ⇒ 返既有 outcome 且 `used_count` 不变**；
  **同 `claim_key` ＋ 异载荷 ⇒ `IDEMPOTENCY_CONFLICT` 且不扣额**；用尽后再 claim 必拒且 `state='exhausted'`；
  **claim↔revoke 两个顺序各自期望**（revoke 先 ⇒ 新 claim 拒；claim 先 ⇒ 记 `revoked_after_use` 并挂回退义务）；
- **执行许可三类并发负测**（本轮新增）：
  ① **首个 claim 未回填 outcome 时两个重放者同时到** ⇒ 二者均得 `IN_PROGRESS`，**外部 effect 只发生一次**；
  ② **claim 后崩溃**（owner 死、lease 过期）⇒ 要么受栅栏接管（新 `fence_token`、旧 owner 回填被 `FENCED_OWNER` 拒、接管者先对账）、
     要么置 `unknown` 进 reconcile；**恒零：非终态且 lease 超 SLA 仍无接管无 `unknown` 的 claim 数**；
  ③ **revoke 与 pending claim 交叉** ⇒ 该 claim 不得取得新 permit，转 rollback/reconcile；在途执行按 fence 回填后仍计回退义务；
  challenge 重放/过期/摘要不符三类在同一 CAS 谓词内被拒；revoke 与 claim 交叉的两个顺序各有期望；
- 跨域事务：形态 A（同库 UoW）为默认；若选形态 B，须实证**同步权威 fence 生效**——
  **注入 outbox 延迟窗口后授权必须被拒**、**fence 超时必须被拒**，并给对账（closeout 有而 expiry 缺超 SLA 的条目恒零）；
- 高危认证强度：**`dual` ＋ `single_factor` 终结高危必拒**；一名达 `second_factor`、另一名未达 ⇒ 同样必拒；
- 批签跨行条件：`signer = proposer` 必拒；签名数不足而进入 `approved` 必拒（两者另有恒零对账）；
- 紧急通道：允许/禁止两张表逐项用例、`uninstall` 必拒、fallback 缺失必拒；
- 分发面：五闸各一正一反、只读角色调高危端点必拒、未批准即下发必拒；
- 职责分离：审计 `dw.actor`、属主单点、审计介质不可写、备份/副本加密与审批；
- **表格完整性校验入 CI**（每表块须有分隔行 · 列数一致 · 无孤立表行）——此判据 owner 为 `SP4·SP8`，
  本件承接 CI 侧实现，**SP4 侧不另造一套**；
- 原型 `s8h` 平台健康屏承载上述恒零计数器与自检结果的读出口。

每次交付回执须带：可取件路径或 40 位 commit、最终 sha256、行数与 `wc -c` 字节、自测命令 ＋ 摘要、门结果 ＋ 失败最小复现。
机器全绿不代签语义 PASS；**设计 PASS 不升级为实现 PASS**。

### 12.4 挂账与冲突

1. **§24.3a 的执行入口不在本件**：唯一入口 `AGT-CAP-EmergencyAction` 属 AGT（SP6 范围）。本件只交判据与拒绝语义；
   ⚠️ **SP4 阻断①的 SP6 侧不因本件收口而闭**——"谁在入口处强制执行"仍悬在 SP6。
2. **SP0/SP1 冻结件头仍为 `draft`**：本件按待确认 seam 编写；其最终 verdict 若改接口，相关 D 块重审。
3. **SP4 挂起**：其平台级晋升与 immutable 名单变更按本件 §11.3 消费侧合同接线，本件不替 SP4 定义 CAP 侧行为。
4. **SP7a 未交付**：受控 transaction 与 migration runner 只写合同，不宣称已具备。
5. **表格完整性 CI 校验为 `SP4·SP8` 双主**：本件承接实现，SP4 解冻后引用本件，不重复实现。

### 12.5 结构化覆盖

<!-- COVERAGE-SP8-BEGIN -->
| 来源 | 设计落点 |
|---|---|
| `SRC:§23` | `D-SP8-01` |
| `SRC:§23.1` | `D-SP8-02` |
| `SRC:§23.2` | `D-SP8-03` |
| `SRC:§23.3` | `D-SP8-04` |
| `SRC:§23.4` | `D-SP8-05` |
| `SRC:§23.5` | `D-SP8-06` |
| `SRC:§24` | `D-SP8-07` |
| `SRC:§24.1` | `D-SP8-07` |
| `SRC:§24.2` | `D-SP8-08` |
| `SRC:§24.2a` | `D-SP8-08` |
| `SRC:§24.3` | `D-SP8-09` |
| `SRC:§24.3a` | `D-SP8-09` |
| `SRC:§24.4` | `D-SP8-10` |
| `SRC:§24.5` | `D-SP8-10` |
| `E:E.1#14` | `D-SP8-10` |
| `E:E.1#32` | `D-SP8-06` |
| `E:E.1#33` | `D-SP8-06` |
| `E:E.1#34` | `D-SP8-03` |
| `E:E.1#35` | `D-SP8-04` |
| `E:E.1#37a` | `D-SP8-09` |
| `E:E.1#46` | `D-SP8-09` |
| `E:E.1#47` | `D-SP8-09` |
| `E:E.1#48` | `D-SP8-08` |
| `E:E.1#49` | `D-SP8-06` |
| `E:E.1#52` | `D-SP8-02` |
| `E:E.1#60` | `D-SP8-06`、`D-SP8-08` |
| `E:E.1#64` | `D-SP8-03` |
| `E:E.1#137` | `D-SP8-07` |
| `E:E.1#160` | `D-SP8-07` |
| `E:E.1#163` | `D-SP8-05` |
| `E:E.1#164` | `D-SP8-05` |
| `E:E.1#169` | `D-SP8-10` |
| `E:E.1#170` | `D-SP8-10` |
| `E:E.1#171` | `D-SP8-10` |
| `E:E.1#172` | `D-SP8-10` |
| `E:E.1#173` | `D-SP8-10` |
| `E:E.1#174` | `D-SP8-10` |
| `E:E.1#179` | `D-SP8-10` |
| `E:E.1#181` | `D-SP8-10`、`D-SP8-12` |
| `E:E.3#9` | `D-SP8-10` |
| `E:E.3#9a` | `D-SP8-10` |
| `E:E.3#12` | `D-SP8-10` |
| `E:E.3#19` | `D-SP8-09` |
| `E:E.3#37` | `D-SP8-12` |
| `E:E.4#8` | `D-SP8-03` |
| `E:E.4#13` | `D-SP8-09` |
| `E:E.4#15` | `D-SP8-08` |
| `E:E.4#16` | `D-SP8-09` |
| `E:E.4#36` | `D-SP8-02` |
| `E:E.4#37` | `D-SP8-08` |
| `IF:GOV-CMD-IssueEffectGrant` | `D-SP8-11` |
| `IF:GOV-CMD-DecidePending` | `D-SP8-08`、`D-SP8-11` |
| `IF:GOV-CMD-SubmitSpec` | `D-SP8-03`、`D-SP8-11` |
| `IF:GOV-QRY-ResolveGateSet` | `D-SP8-11` |
| `UI:#s1` | `D-SP8-08` |
| `UI:#s8` | `D-SP8-08` |
| `UI:#s8r` | `D-SP8-10` |
| `UI:#s8h` | `D-SP8-12` |
<!-- COVERAGE-SP8-END -->

### 12.6 验收判据

<!-- ACCEPTANCE-SP8-BEGIN -->
| ID | 受检范围 | 判据 | 判不了什么 |
|---|---|---|---|
| `AC-SP8-01` | checker 派生的 §23–§24 heading | COVERAGE 逐项 D 落点，未知 0 | 落点语义正确性 |
| `AC-SP8-02` | owner_sp 含 SP8 的附录 E 行 | 逐项语义落点，未知 0 | 人工判据是否专业正确 |
| `AC-SP8-03` | 架构 §6 GOV 接口 | 4 接口逐项落点；本件只对授权/批准/grant 面给实体判据，SubmitSpec/ResolveGateSet 只写 seam | 定义层语义是否恰当（归 SP0） |
| `AC-SP8-04` | §13 GOV 入口减 SP0 完整认领（含 `s1`） | 差集逐项落点且原型中存在；`s1` 落点在批准面 | UI 可用性与实现状态 |
| `AC-SP8-05` | 事件面 | `EV-GOV-*` 实算 0 且正文不产出事件 ID | 未来是否该有事件 |
| `AC-SP8-06` | 授权两轴、层级两分量与预授权 | 两轴分别持久化；**层级落成 `signature_count` ＋ `factor_requirement` 两列**，§23.1 三档逐档有映射、无悬空值；**次因子不得抵扣重两签**（以次因子为由跳过 `dual` 必拒）；层级两列不接受预声明为输入；预授权三字段齐且无禁用词 | 某次授权决定是否恰当 |
| `AC-SP8-07` | 失效触发 ＋ 预案承接 | 收口/版本/时限三类机制触发且按条目×作用域判；上线后依据为预案条目 | 运维是否照做 |
| `AC-SP8-08` | 渠道分级（范围×可达步骤）、challenge 与门 PASS 层级 | **四档均给「动作→档位」范围枚举**，`classify(action)` 全序可求值且未命中者 fail-closed 按高危；「渠道终结高危授权数恒零」对四档均可求值；分级与可达步骤对应；challenge 三要素齐且重放/摘要不符/过期必拒；门 PASS 层级继承 node 分级、L3 不得由渠道终结 | 危险度评估是否准确 |
| `AC-SP8-09` | 基线范围与签数 | §24.1 范围可判；签数取批内最高危项、拆批不降级 | 高危认定是否遗漏（人审） |
| `AC-SP8-10` | 紧急通道 | 按 capability effect 判而非动作名；uninstall 不在允许列；无 fallback 即拒；补审闭环 | 兜底机制实际有效性 |
| `AC-SP8-11` | 分发面、职责分离与 grant/challenge 并发合同 | 未批准即拒发；批准与执行分权；五闸与角色模型负测齐；**新 claim 才扣额且仅其 owner 得 execute permit；重放：终态返既有 outcome、非终态返 `IN_PROGRESS` 且不发第二张 permit；异载荷报 `IDEMPOTENCY_CONFLICT`**；**lease 过期须受栅栏接管或置 `unknown`，接管者先对账不得盲重执行**；达上限在同一语句置 `exhausted`；**revoke 覆盖未用/部分已用/已耗尽三态**并挂回退义务；双签为签名集合、两名 signer 各自达 `second_factor`；**跨行条件由写路径或触发器保证并各有对账，不冒充行 CHECK** | 真实并发下的实现正确性 |
| `AC-SP8-12` | GOV 目标 schema ＋ SP7b 五列与证据出口 | 目标表逐表给 PK/FK/unique/check/index/RLS 与不可变性；**跨行/跨表条件另列强制手段与对账（§12.2b），不写成行 CHECK**；五条跨表不变量成立；五列逐对象对应目标表；设计 PASS 不升级实现 PASS | 实际迁移与运行结果 |
<!-- ACCEPTANCE-SP8-END -->
