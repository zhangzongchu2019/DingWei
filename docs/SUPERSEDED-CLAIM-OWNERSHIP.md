# 6d8ae86「35 处引用点」的 **claim_id 清单 ＋ 归属**

> 🔴 **本件是分派底座，不是执笔方的自证。**
> 乙域 terra 裁定（coordinator 2026-08-26 转）：
> 「`6d8ae86` 的 35 处混合三域撤销断言，**不能由执笔方或任一域单签**；
> 应以稳定 `claim_id` 列表**按归属分派、门方独立复核**。」
> ⇒ 本件只做两件事：**给出稳定 ID**、**给出机器可复算的定位与归属规则**。
> **每一处是否真的改对了，仍归门方逐处判。**

## 一、🔴 先说清「35」这个数：**它复算不出来**

本件的行是 `git show 6d8ae86` 的实取结果，口径与计数如下（**取 6d8ae86 当时的清单**
`git show 6d8ae86:specs/gates/superseded-claims.txt`，12 条短语，非今天的 15 条）：

| 口径 | 数 |
|---|---|
| 被改动且**含清单短语**的行数 | **36** |
| 同上，按**短语命中**计（一行含两个短语算两处）| **38** |
| 若改用**今天**的清单（短语 `晋升至 D0` 已收窄为 `可晋升至 D0`）| 34 行 |

⇒ **提交信息里的「35 处」在上述任一口径下都复算不出**（差 1 行 / 3 命中）。
🔴 **故请门方以本表的 claim_id 为准，不要以「35」这个数为准。**
⚠️ 这正是本轮反复出现的那一族：**一个没写口径的计数，事后没人能复算**
（见 `specs/gates/GATE-CONTRACT.md` 三之二）。

## 二、口径（可复跑）

```sh
git show 6d8ae86:specs/gates/superseded-claims.txt | sed 's/#.*//' | awk -F'\t' 'NF>=3 && $1!=""{print $1}'
git show 6d8ae86 --unified=0 -- docs/dingwei-platform-specdoc.md docs/dingwei-prototype.html \
    docs/dingwei-architecture-design.md docs/DELIVERY-STATUS.md
```
· 取每个 hunk 的 `-` 行，判其是否含上述短语；含则计一处（**多短语分别计**）；
· 「当前行」＝ 用该 hunk 的 `+` 行原文在**当前工作树**中定位；原文被后续提交再改过的，
  按前 40/30/22/16 字唯一前缀模糊定位，仍不唯一者人工定位并在**定位方式**列注明；
· 「行指纹」＝ 当前行去空白后 `sha256` 前 16 位（与三闸受控例外同口径，可直接用于登记例外）。

## 三、归属规则（**先说规则，再列表** —— 规则可被门方推翻，逐条争论不可复算）

| 主归属 | 判据（按短语的语义面，不按件）| 门方 |
|---|---|---|
| **安全** | 库/表边界、RLS、跨企业读、typed capability、晋升越界 | 乙域 terra |
| **结构** | D 轴层级重映射、租户双层结构、隔离域定义 | 甲域 sol |
| **跨制品会签** | 同一短语出现在 **≥2 件**（SpecDoc / 原型 / 架构）⇒ 另需一致性会签 | 丙域 kimi |

🔴 **主归属按短语语义定，不按件定** —— 否则原型里的每一处都会被划成「跨制品」，
而它们的实质分歧仍在安全或结构域。**件维度另立「跨制品会签」一列，不吃掉主归属。**

| 主归属 | 条数 |
|---|---|
| 安全（乙域 terra）| 15 |
| 结构（甲域 sol）| 23 |
| 其中需**跨制品会签**（丙域 kimi）| 33 |
| **合计** | **38** |

## 四、清单

| claim_id | 旧断言短语 | 件 | 6d8ae86 旧行 | 当前行 | 行指纹 | 主归属 | 跨制品会签 | 定位方式 |
|---|---|---|---|---|---|---|---|---|
| `SC-P01-PROTO-01` | `不做 RLS` | `dingwei-prototype.html` | 680 | 685 | `566e7e8df3fdc8c7` | 安全 | ✔ | 原文直接命中 |
| `SC-P01-PROTO-02` | `不做 RLS` | `dingwei-prototype.html` | 1481 | 1534 | `f3adda96342e0b2b` | 安全 | ✔ | 模糊定位(前40字唯一) |
| `SC-P01-SPEC-01` | `不做 RLS` | `dingwei-platform-specdoc.md` | 1138 | 1241 | `d88dd22e65da2560` | 安全 | ✔ | 模糊定位(前22字唯一) |
| `SC-P01-SPEC-02` | `不做 RLS` | `dingwei-platform-specdoc.md` | 1982 | 2212 | `7e51d53cd7d149f0` | 安全 | ✔ | 原文直接命中 |
| `SC-P01-SPEC-03` | `不做 RLS` | `dingwei-platform-specdoc.md` | 2774 | 3107 | `63335ba0647a1cb5` | 安全 | ✔ | 原文直接命中 |
| `SC-P02-SPEC-01` | `不叠 RLS` | `dingwei-platform-specdoc.md` | 1999 | 2229 | `4f1021fe19af4df8` | 安全 | — | 原文直接命中 |
| `SC-P03-PROTO-01` | `不做每租户独立库` | `dingwei-prototype.html` | 680 | 685 | `566e7e8df3fdc8c7` | 安全 | ✔ | 原文直接命中 |
| `SC-P03-SPEC-01` | `不做每租户独立库` | `dingwei-platform-specdoc.md` | 1982 | 2212 | `7e51d53cd7d149f0` | 安全 | ✔ | 原文直接命中 |
| `SC-P05-PROTO-01` | `三级映射` | `dingwei-prototype.html` | 852 | 909 | `23ada5e9a31e23e3` | 结构 | ✔ | 模糊定位(前40字唯一) |
| `SC-P05-PROTO-02` | `三级映射` | `dingwei-prototype.html` | 1478 | 1531 | `f54d8653315f89f4` | 结构 | ✔ | 原文直接命中 |
| `SC-P05-PROTO-03` | `三级映射` | `dingwei-prototype.html` | 1639 | 1693 | `cf8d03a42a713d58` | 结构 | ✔ | 原文直接命中 |
| `SC-P05-PROTO-04` | `三级映射` | `dingwei-prototype.html` | 1643 | 1697 | `db545093011f18e5` | 结构 | ✔ | 原文直接命中 |
| `SC-P05-PROTO-05` | `三级映射` | `dingwei-prototype.html` | 1736 | 1790 | `72174b66715d984f` | 结构 | ✔ | 原文直接命中 |
| `SC-P05-PROTO-06` | `三级映射` | `dingwei-prototype.html` | 1780 | 1834 | `2a25b04f44bf4f04` | 结构 | ✔ | 原文直接命中 |
| `SC-P05-PROTO-07` | `三级映射` | `dingwei-prototype.html` | 1784 | 1838 | `f226d004779216c8` | 结构 | ✔ | 人工定位(该行后续被改写) |
| `SC-P05-PROTO-08` | `三级映射` | `dingwei-prototype.html` | 1884 | 1938 | `a9b640952f26457d` | 结构 | ✔ | 原文直接命中 |
| `SC-P05-PROTO-09` | `三级映射` | `dingwei-prototype.html` | 1889 | 1943 | `2facc83b14522b96` | 结构 | ✔ | 原文直接命中 |
| `SC-P05-PROTO-10` | `三级映射` | `dingwei-prototype.html` | 1897 | 1951 | `11d6e7a935b39829` | 结构 | ✔ | 原文直接命中 |
| `SC-P05-PROTO-11` | `三级映射` | `dingwei-prototype.html` | 1936 | 1990 | `06b054940017982e` | 结构 | ✔ | 原文直接命中 |
| `SC-P05-SPEC-01` | `三级映射` | `dingwei-platform-specdoc.md` | 1842 | 2063 | `11b8ff11225d0251` | 结构 | ✔ | 原文直接命中 |
| `SC-P05-SPEC-02` | `三级映射` | `dingwei-platform-specdoc.md` | 2297 | 2535 | `6f3b8b9ef8bd0e6b` | 结构 | ✔ | 原文直接命中 |
| `SC-P05-SPEC-03` | `三级映射` | `dingwei-platform-specdoc.md` | 2303 | 2541 | `8782e22a484bba73` | 结构 | ✔ | 原文直接命中 |
| `SC-P05-SPEC-04` | `三级映射` | `dingwei-platform-specdoc.md` | 2315 | 2553 | `550b2e6a305fa4f7` | 结构 | ✔ | 原文直接命中 |
| `SC-P05-SPEC-05` | `三级映射` | `dingwei-platform-specdoc.md` | 2378 | 2623 | `f8e9555403f10bd6` | 结构 | ✔ | 原文直接命中 |
| `SC-P06-SPEC-01` | `业主级隔离域` | `dingwei-platform-specdoc.md` | 251 | 269 | `fba9e815a41489be` | 结构 | — | 原文直接命中 |
| `SC-P07-PROTO-01` | `D2 租户级` | `dingwei-prototype.html` | 2336 | 2389 | `64a1e518c088f991` | 结构 | ✔ | 原文直接命中 |
| `SC-P07-SPEC-01` | `D2 租户级` | `dingwei-platform-specdoc.md` | 1197 | 1300 | `28a74902dbf615fc` | 结构 | ✔ | 原文直接命中 |
| `SC-P07-SPEC-02` | `D2 租户级` | `dingwei-platform-specdoc.md` | 1284 | 1387 | `a80c6055bcc09931` | 结构 | ✔ | 原文直接命中 |
| `SC-P07-SPEC-03` | `D2 租户级` | `dingwei-platform-specdoc.md` | 1299 | 1402 | `827660c5ca70c45b` | 结构 | ✔ | 原文直接命中 |
| `SC-P07-SPEC-04` | `D2 租户级` | `dingwei-platform-specdoc.md` | 1763 | 1953 | `a663916e3eba84a4` | 结构 | ✔ | 原文直接命中 |
| `SC-P07-SPEC-05` | `D2 租户级` | `dingwei-platform-specdoc.md` | 2509 | 2776 | `dc9592a9af92f4f8` | 结构 | ✔ | 人工定位(该行后续被改写) |
| `SC-P09-SPEC-01` | `晋升至 D0` | `dingwei-platform-specdoc.md` | 1019 | 1122 | `7cbb10d321392998` | 安全 | — | 原文直接命中 |
| `SC-P09-SPEC-02` | `晋升至 D0` | `dingwei-platform-specdoc.md` | 1398 | 1500 | `7997633aff5edd08` | 安全 | — | 原文直接命中 |
| `SC-P11-SPEC-01` | `跨企业视图` | `dingwei-platform-specdoc.md` | 1758 | 1949 | `6f24d61deb24233c` | 安全 | — | 原文直接命中 |
| `SC-P12-ARCH-01` | `cross_tenant_person_rollup` | `dingwei-architecture-design.md` | 788 | 792 | `1cccbb2551b7fcb9` | 安全 | ✔ | 原文直接命中 |
| `SC-P12-PROTO-01` | `cross_tenant_person_rollup` | `dingwei-prototype.html` | 2359 | 2411 | `f350954d21ad1404` | 安全 | ✔ | 原文直接命中 |
| `SC-P12-SPEC-01` | `cross_tenant_person_rollup` | `dingwei-platform-specdoc.md` | 1747 | 1924 | `bb9f96ab99882a93` | 安全 | ✔ | 原文直接命中 |
| `SC-P12-SPEC-02` | `cross_tenant_person_rollup` | `dingwei-platform-specdoc.md` | 3044 | 3537 | `2bccb860815759c1` | 安全 | ✔ | 模糊定位(前40字唯一) |

## 五、本件**判不了**什么（写明不假装）

| 判得了 | 🔴 判不了 |
|---|---|
| 这 38 处**确实是** 6d8ae86 改动的、且旧行含清单短语 | **改后的新表述对不对** |
| 每处的当前位置与行指纹（可复算、可用于登记例外）| **是否有【漏改】的引用点**（本件只覆盖 6d8ae86 改过的行，改漏的行不在其中）|
| 归属规则是什么 | **归属划得对不对** —— 规则在第三节，门方可整条推翻 |

🔴 **「6d8ae86 清零了 35 处」这句话，本件不为它背书**，只提供分派用的稳定 ID。
