# Cross-artifact reviewer rerun · design/v0.2

- 被检树：`ccccd4f540d26313eda3098d115d74de746b1a89`
- 被检分支：`design/v0.2`
- 复跑者：`zzc-chatgpt56soltchangshark-2642`
- 复跑时间：2026-08-27（UTC）
- 清单：`specs/platform/checklist.crossartifact-reviewer.md` 的 CAC-1～CAC-12
- 口径：这是对新统一树的重新执行记录，不改写 `ae2582d` 上的历史判断；命令均从被检树仓根运行。

## 取件坐标

| 件 | sha256 |
|---|---|
| `docs/dingwei-platform-specdoc.md` | `772220830b47a1dbbab69b2740efac1992f6681f73f92d0821476fa516c6a655` |
| `docs/dingwei-platform-architecture.md` | `0a2288c7b9e245eded06944cd063a6d0d458c6b1f45a3a9534d29817b6c58fdc` |
| `docs/dingwei-prototype.html` | `eeb71a5a77a53990569e08f9745165903bcf9e34cf6ce0a5047824167e1df4b8` |
| `docs/DELIVERY-STATUS.md` | `8a8c55eb8483a0d9ede4cf27b2253dec7dcf9fd2a71c900a8516141dda6eb23a` |

## 逐项结果

| 项 | 结果 | 当前树证据 |
|---|---|---|
| CAC-1 引用语义 | PASS | `scripts/arch-refcheck.sh`：目标架构 heading 89、重复 0；SpecDoc 引用 72 次/50 目标、悬空 0、旧 `A§` 0。`specs/tools/analyze.sh`：原型引用 119 个唯一节号、悬空 0。逐条核对架构引用点上下文与目标标题，未发现误指。 |
| CAC-2 计数与枚举 | PASS | `python3 docs/tools/speclint.py docs/dingwei-platform-specdoc.md` rc=0；`scripts/arch-refcheck.sh --selftest` 4/4；v0.2 派生集合由 `specs/v0.2/check.sh` 重算，实检与预期集合相等。 |
| CAC-3 多处枚举 | PASS | SpecDoc 的权威定义/指针仍由 speclint 放行；v0.2 的 manifest、域基线、义务、RV ledger 均做预期⇄实检精确对撞，`specs/v0.2/check.sh` rc=0。未见同一现行集合两处各自定义而值不同。 |
| CAC-4 术语唯一义 | **FAIL** | 旧单层模型名尚未归零：`TenantCtx` 出现数 SpecDoc=13、架构=1、原型=6；`PendingTenantCtx` 为 SpecDoc=3。SpecDoc §21.1c 明记迁移归 SP1，故这是 RV-甲06 的已知未闭阻断，不得把“有迁移说明”误作“旧称零残留”。 |
| CAC-5 规则依赖同步 | **FAIL** | SpecDoc 已规定双层 `AccessCtx`，但架构仍记“现有 `TenantCtx` 只有单层 ID”，原型仍用 `TenantCtx` 作为仓储入口与负测上下文。三件对目标语义与现状描述并不矛盾，但规则变更尚未同步到实现/展示依赖点；与 RV-甲06 同根，待 SP1 收口后复判。 |
| CAC-6 版本/状态/锚 | **FAIL** | `specs/tools/analyze.sh` rc=0 只证明三件头块版本与状态源一致；架构 §0 输入表仍冻结 SpecDoc **v3.69** / `8ae715…` 与原型 **v1.46** / `0308a2…`，而 v0.2 状态源是 SpecDoc **v3.73** / `772220…`、原型 **v1.47** / `eeb71a…`。这是当前 `foreign-version-check.py` 未覆盖的假绿。 |
| CAC-7 示例数据 | PASS | 复核原型既有跨屏实体链：N-1204 仍为唯一封存主例；N-1163 状态无相反值；图搜 3.2 为 expired、图搜 3.1 v0.4 为待认领；未发现同实体的状态/归属/时间线新增冲突。 |
| CAC-8 实测与锚 | PASS | `specs/evidence/collect.sh` 所列命令仍存在；`specs/v0.2/check.sh` 对 12 个 baseline path 逐项 sha256、六历史锚 ancestry、七义务和 RV ledger 实算，rc=0。本文取件指纹由 `sha256sum` 实取。 |
| CAC-9 依赖闭包 | PASS | `scripts/arch-refcheck.sh` rc=0；`scripts/arch-refcheck.sh --selftest` 4/4；`specs/tools/analyze.sh` rc=0；未出现不可解析节号或旧架构命名空间回流。 |
| CAC-10 “所有”枚举层 | PASS | 复核 §22.5 的入站、出站与基础设施层约束仍在；本次统一树未把作用域缩回应用层。相关 § 引用由上述引用闸证明可达。 |
| CAC-11 冲突裁断 | PASS | F 附录的现行冲突均有“以哪边为准/已消解/须删改”终态；v0.2 域错配 3 条全部登记为 `DM-SP2-01..03`，未登记错配 0。未发现合并时形成的两套现行权威件。 |
| CAC-12 搜索范围 | PASS | `status-consistency-check.sh` 明示受检范围、预期集合、实检集合和未知集合 0；v0.2 gate 同样报告五类预期/实检集合及未知集合 0。本文对旧名的断言显式覆盖三件全文，没有以子集声称全集。 |

## Findings

| ID | 件/位置 | 问题 | 可复跑依据 | 严重度 |
|---|---|---|---|---|
| CA-v02-01 | 三件；SpecDoc §21.1c | 双层访问上下文已定，但 `TenantCtx`/`PendingTenantCtx` 旧单层名尚未跨件归零。 | `for f in docs/dingwei-platform-specdoc.md docs/dingwei-platform-architecture.md docs/dingwei-prototype.html; do rg -o 'TenantCtx|PendingTenantCtx' "$f" | sort | uniq -c; done`；期望 SP1 收口后目标语义位置零旧名（沿革/迁移证据须显式豁免）。 | 阻断 |
| CA-v02-02 | 架构 §0 输入锚 | 架构输入表冻结 v3.69/v1.46，与 v0.2 权威三件 v3.73/v1.47 不一致；现有 analyze 假绿。 | `sed -n '16,29p' docs/dingwei-platform-architecture.md`; `rg -n 'dingwei-platform-(specdoc|architecture)|dingwei-prototype' docs/DELIVERY-STATUS.md`; 期望版本和 sha256 逐项相同。 | 阻断 |

## Verdict

**NO：9 PASS / 3 FAIL（归并为 2 条阻断）。**

- CAC-4 与 CAC-5 同根，归入 SP1 的 RV-甲06 收口面；不能在本次复跑中假定已闭。
- CAC-6 是统一树新暴露的跨件输入锚漂移，须刷新架构输入表，并给 `foreign-version-check.py` 增加该表的精确对撞，避免只看头块的假绿。
- 其余 9 项在 `ccccd4f` 上复跑通过；本结论不把 `ae2582d` 的旧证据冒充当前证据。
