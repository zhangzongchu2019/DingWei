# SP4 设计合同 v1.3 评审登记（coordinator 归口）

> 本件是方案 §10 所要求的「coordinator 正式登记 SP4 3/3 PASS 且可取」的载体。
> coordinator 不入仓（行为红线：不直接改任何仓库文件）。**需由执笔方或整合方将本件提交进仓**，
> 建议路径 `specs/sp4/REVIEW-REGISTRATION-v1.3.md` 或 v0.2 整合方指定位置。
> 登记人：`zzc-coordinator-2642` · 登记时间 UTC 2026-08-27。

## 一、标的

| 项 | 值 |
|---|---|
| 件 | SP4 skill/MCP 体系设计**合同** v1.3 |
| 锚 | `4006cbeeda3edd986d6a9da53268e68aa01c9c9e` |
| 分支 | `design/sp4-v1-opus50tczc2000`（登记时该锚即分支头） |
| 父提交 | `9c732b2`（v1.2），无 rebase |
| 改动面 | `specs/sp4/check.sh` ＋ `contract-check.sh` ＋ `spec.md`，共 +24/−13 |
| 执笔方 | `zzc-opus50tczc2000-2642` |
| 三件 sha256 | `spec.md` `198a8ed118ff8816a23e9ab3a3dc32a2590de6fabe549cc2a356ed1123c9447c`<br>`check.sh` `ef9bc33b4e446f8f2d0dbcf893175f43dc02ac0bccfde8b7cc788c6603676bce`<br>`contract-check.sh` `fb13bfd11b74601ee0aaf61fc694a5cff4b85480bb555087b2b7e2ea1afda4ba` |
| SP4 正文 | `docs/dingwei-sp4-design.md` **尚未产生**；`check.sh` 普通检查预期 rc=2，不得记 PASS |

## 二、三票（全部对同一锚 `4006cbe`）

| 门方 | v1.2 `9c732b2` | v1.3 `4006cbe` | 非阻断 |
|---|---|---|---|
| `zzc-chatgpt56soltchangshark-2642` | NO（B1＋B2 两组） | **PASS** 阻断 0 | 0 |
| `zzc-deepseekv4pro08130ca16c13-2642` | PASS（未覆盖 B1/B2） | **PASS** 阻断 0 | 0 |
| `zzc-opus50tchangshark-2642` | NO（B1） | **PASS** 阻断 0 | 4 |

⇒ **3/3 PASS，阻断 0，门已关。** v1.2 的两个 NO 均已翻转。
轮次：v1.1→v1.2→v1.3 复评为第三轮，符合业主「三轮出结果」纪律；
第三票明确表态「**不必再开第四版**」。

### 两组原阻断的闭合方式
- **B1**（SP8 件未冻结未校验）：改为**三锚组合冻结**＝ SpecDoc `4220195` ＋ SP8 件 `970effe` ＋ 基线 `6a98629`；
  `docs/dingwei-sp8-design.md@970effe`（sha256 `4f5e4e06d29d4a9fffcdb46f413fbc37bcfd615e9af9b04215e0a6c20205a98a`）
  入 ⑥ 输入表，`check.sh` 的 PATHS 与 `contract-check.sh` 双侧校验，取不到或漂移 rc=2。
- **B2**（依赖点未同步、验收仍允许旧弱语义）：`D-SP4-08` / `AC-SP4-07` / `D-SP4-11` / §5 / §6 五处同改，
  新增六条断言并逐条负测。

## 三、coordinator 独立复算（不引用任何一方自报）

- `4006cbe` 为 `origin/design/sp4-v1-opus50tczc2000` 当时分支头，父 `9c732b2`，改动面三件与自报一致。
- 三锚组合与各自上游对撞：SpecDoc `4220195` 即 `design/addendum-decisions` 头；
  基线 `6a98629` 即 `design/sp3-v1-sol` 头；
  SP8 件 `970effe` 与当时 SP8 分支头 `c7ac0c3` 上的 `docs/dingwei-sp8-design.md`
  **为同一 blob `0460d914ef9fcba6a5045286cff31942ad722817`** ⇒ 锚动件未动。
- committed 内容核验：`spec.md` L151 SP8 输入行、`check.sh` L6/L20/L29、
  `contract-check.sh` L5/L19-20 与 L41/L42 反向断言，均在提交内容中实取到。

## 四、随票登记的 4 条非阻断（`zzc-opus50tchangshark-2642` 提出，coordinator 已复算前两条）

1. 🔴 **「五项」vs「六项」**：`spec.md` 全文 4 处称 SP8 §11.5a 为「五项独立复算」，
   而所锚 `970effe` 上该节编号条目实为 **6 项**（`1f703b8` 才是 5 项）。
   缺的第 6 项含「接管须持新 `fence_token` 且先对账再决定是否执行」，合同无落点。
   **业主 2026-08-26 已裁：正文开工前 bump 合同并重开受影响评审。**
2. ⑥ 表所写 sha256 与 checker 常量之间无对撞：表里数值写错机器不会响。
3. `D-SP4-11` 外部 effect 接口举例未含 `RequestPromotion`。
4. 🔴 **`970effe` 可达性**：无 tag、非 `4006cbe` 祖先、仅 `design/sp8-v1-opus50tczc2000` 可达。
   已并入 v0.2 整合方案，升格为**整合门**（要求全部跨分支锚成为受保护 v0.2 冻结锚的 ancestor）。

## 五、有效性声明

本登记只证明：**上述三票对 `4006cbe` 成立、阻断为 0、合同门已关。**
不证明：SP4 正文正确、SP8 实现正确、或本合同在 SpecDoc 后续版本下仍然充分。
第 1 条非阻断已由业主裁定须 bump，**故本登记所述 v1.3 为「已过门但已知须继任」的状态**，
引用本登记时不得据此宣称 SP4 合同为终版。

—— `zzc-coordinator-2642` · UTC 2026-08-27
