# 评审证据包（作者产出，供门方复算）

生成时刻：2026-08-25T12:12:22Z
分支头：`53f4033dac84c797f72ac6f01363ed6fc3a6bca1`

> 🔴 **本包不含任何门方结论。** 每行给出【命令】与【该次输出】，门方可原样重跑。
> ⚠️ **机器旁证的能力边界**：它们证明的是「形状对不对」，不是「语义对不对」。
> 语义层判断（引对号是否引错义、枚举是否可反推、effect 是否真的单调收紧）**机器判不了**，归人审。

## 评审对象内容指纹（本包分析的就是这些字节）

```
REVIEWED e866bcbc2face920198cb5da9c447e3ee9ba3857ba8f5bfb82c1a6bffa7ad8ea  docs/dingwei-platform-specdoc.md
REVIEWED e64dca22a7263ecf17cf0977cc14297d89e8ba196d913b5dea0c33ce82c47b7c  docs/dingwei-architecture-design.md
REVIEWED 222eccb1b650ef3132fd97bc0b7fb334037cf76948dc63bdb35be31478a1249e  docs/dingwei-prototype.html
REVIEWED 3fa86dc4e01433393245d1b00cef569ff5c11d9b0a793f4de60de1d75d5b5700  docs/DELIVERY-STATUS.md
REVIEWED a1fe9843b31755f89f241d80a39b57a15e72f3384c5af0219e284ca1ae9135eb  specs/evidence/collect.sh
REVIEWED 51ae52b28d3d3b0af6f5114bedc246c497179c6035bfd519611d30b86d72e125  scripts/appendix-e-owner-gate.sh
REVIEWED 2c1688e8431b0819e9ac6fc58d254c983284d788d86250562195345dffcc9a9b  scripts/arch-refcheck.sh
REVIEWED 253a59f6e85c9a1438beeac339efdc2294b21ae3bba46aaf30e7d1ee9befb521  scripts/check-sensitive.sh
REVIEWED 451fa8171c5460ef6ff723d68f902d756469b90d06dbcd4f924886ce6b885f0f  scripts/checklist-executed-gate.sh
REVIEWED 69b60fda965d49479f0390a583810b082c1f049b30b4d04af2127f3eb13d43c3  scripts/conflict-marker-gate.sh
REVIEWED 687300bc3e8a56f491c533116b6cccc6da256f4e7aaf300607cd32cb6274e78e  scripts/delivery-check.sh
REVIEWED 33d372ff3b1dcda466476c113af888b55e02dee8479a894c8b0c48d8e9a45013  scripts/evidence-fresh-gate.sh
REVIEWED 156c626fe5e069606e07f642d5f7fd3477b5ce42f1173d28b40e425f4556b883  scripts/gen-status.sh
REVIEWED e671108564fa7d404c2b6b53340ed202a55beb6351c293196a0668f82108e05f  scripts/na3-pg-evidence.sh
REVIEWED 19c187d91bff0d18a734582e6c278cca4f87596a59a87b824ea0acbabdc0d5be  scripts/path-anchor-gate.sh
REVIEWED 5bbb2d1fc390a00e7a2d12c13dd0c591081d1210dfaacaa732e91cae2795f62e  scripts/prose-style-gate.sh
REVIEWED 15464d1f637362c0ca022c951c6bfe17b8a84e2f38df139320a2b633da882650  scripts/reliable_delivery_canary.sh
REVIEWED 29fefc4850804f2f9250241b8c9584d19dfc183e7987c90c25819f2e58a10ed9  scripts/repo-boundary-gate.sh
REVIEWED 8e8795b9c2b21d7c2b0de09a69f8b1b7049f0f0b7c71cccf119e9156a4c8e323  scripts/rv-integrity-gate.sh
REVIEWED 729093843d0d058b1f3aef88081e27e5daa6f527feb5bb60fbcb8be2dd865a40  scripts/speclint-gate.sh
REVIEWED 65ca074adfd69fd0315f78991b9bfe21579f27e7d8f3e586fc85b60b857149b6  scripts/specs-index-check.sh
REVIEWED 04a61a19321b7a35a78a5a886b1bc915dd5012fcf1dbd8636627e058c966eb3e  scripts/status-consistency-check.sh
REVIEWED 4c41cb0ccd5452a7eadd8f5eddad986660d9cf51cdc461d51235eb0894100023  scripts/superseded-claims-gate.sh
REVIEWED a5fcd7cfbd7194d44a10fd7bba42c27f0b7258c09861d6ecf39a7337e294f66f  scripts/tenant-isolation-gate.sh
REVIEWED 91102ae852827ee0b7ecfe122d29f13c95ff73d49752be29f3991f2ca93b2832  specs/tools/analyze.sh
REVIEWED fa2e42ef5aabe1418512e2e3b4cf4a103c978a4bcfd80e0c06db4c536ee092d6  specs/tools/disposition-audit.py
REVIEWED 49a9eab11f34bdef4c65e1c4c6f0ac969cfa28faa834aa52d7f84d5d453bc8e7  specs/tools/export-bundle.sh
REVIEWED 41e915d3132b580332d7fa984979c6740ad6a892733f57d14d1192ddce83c778  specs/tools/foreign-version-check.py
REVIEWED c598b3199e0c418af5d6f207fff5a4b2e093edf0e3412562bccd8524612a337b  specs/tools/rv-state-check.py
REVIEWED 49b6196087629e5500f99da9b11a74a7bf035f79e0db24988e9194839d264288  specs/tools/safe-replace.py
REVIEWED 9328cd8abaf6e6ceacf8e0968e8431ae28080ca67044c32457c9283ef467b552  docs/tools/speclint.py
REVIEWED 2ca28e44ad27f466487859548c69cf3f321a2f03200fbe1731fe36ca75651e5a  specs/gates/boundary-baseline.txt
REVIEWED 16e5ba25e2b662a97269723d229d138cd7f0e2ebc7bd5177f9575eb2470fdbae  specs/gates/checklist-execution-exceptions.txt
REVIEWED 861f36f0dfeda164e4047bfd75ea78e5977d3ad352c8e1c5cafff6a790945530  specs/gates/repo-packages.txt
REVIEWED 905f6b5d146b7976cae5b84a9e8d321170a4355f945c1cc40fd53b923134c84c  specs/gates/rv-ledger.txt
REVIEWED e7a91c5e523a367419df93ea42bb570bbb33a08479ff8e5ec4ff3d01fa7e2789  specs/gates/status-stale-exceptions.txt
REVIEWED 77103d4b6275652f23eaaea5f075e159073432eee5c2253f0cf42ea8592cc011  specs/gates/superseded-claims.txt
```

> 🔴 **指纹对不上即本包非当期产出** —— 与「分支头」是两个不同的保证：
> 分支头说的是【在哪个提交上跑的】，指纹说的是【跑的是哪些字节】。
> 提交距离只是前者的代理，改动落在那一个提交里时它判不出来（乙域 RV-乙08）。

## 一、单件闸
### SpecDoc speclint（受控符号 · 字段声明 · 表完整性 · E 计数 · 权威定义唯一 等 14 条规则）

```
$ python3 docs/tools/speclint.py docs/dingwei-platform-specdoc.md
  --- 全部通过
  (rc=0)
```

### speclint 语料自检（正反例须拦在预期规则上）

```
$ python3 docs/tools/speclint.py --selftest docs/tools/speclint-corpus.md
  --- 语料 45 例 / 覆盖规则 16 条,全部通过
  (rc=0)
```

### speclint 变异体（改坏规则，其反例须随之失守 —— 证明语料有牙）

```
$ python3 docs/tools/speclint.py --mutate docs/tools/speclint-corpus.md
  --- 变异体 16 个,全部如期失守(语料有牙)
  (rc=0)
```

### 架构设计引用核验（A§ 本件 / § SpecDoc 分别解析）

```
$ scripts/arch-refcheck.sh
  本件内引用 A§ : 65 次 / 27 目标 · 悬空 0
  SpecDoc 引用 § : 169 次 / 68 目标 · 悬空 0
  heading 真实重复：本件 0 · SpecDoc 0
  --- 引用核验通过
  (rc=0)
```

### 引用核验的反测四项（含 SpecDoc heading 重复 —— 曾漏接失败条件）

```
$ scripts/arch-refcheck.sh --selftest
    ✓ 拦住：SpecDoc 悬空 §99.9
    ✓ 拦住：本件悬空 A§77.7
    ✓ 拦住：本件 heading 重复
    ✓ 拦住：SpecDoc heading 重复
  --- 反测四项全过（闸有牙）
  (rc=0)
```

## 二、跨制品
### analyze 跨制品一致性（件与件之间；单件自洽由上节负责）

```
$ specs/tools/analyze.sh
  ① 各件单件闸
    ✓ SpecDoc speclint
    ✓ 架构引用核验
  ② 架构设计引用的 SpecDoc 节号是否都存在（跨制品悬空）
    ✓ 无悬空
  ③ 原型引用的 SpecDoc 节号是否都存在
    原型引用 114 个节号；悬空 0 ✓
  ④a 三件的【自述】版本是否与 DELIVERY-STATUS 一致
    SpecDoc: 件内 v3.30 · 状态文件 v3.30 ✓
    架构设计: 件内 v0.33 · 状态文件 v0.33 ✓
    原型: 件内 v1.37 · 状态文件 v1.37 ✓
  ④b 正文是否出现【他件】的旧版本号
    ✓ dingwei-platform-specdoc.md: 正文无他件旧版本号
    ✓ dingwei-architecture-design.md: 正文无他件旧版本号
    ✓ dingwei-prototype.html: 正文无他件旧版本号
  ⑤ 生效前置状态（§0.4）
    ✓ 三域 checklist 齐备（结构 / 安全 / 跨制品）—— 但【生效与否取决于门方执行结果，非文件存在】
  ---
  跨制品一致性检查通过（不含语义层，见脚本头注）
  (rc=0)
```

## 三、代码现状实测（架构设计 A§6.2 所载数字的来源）
### internal 包总数

```
$ ls -d internal/*/ | wc -l
  28
  (rc=0)
```

### workpulse 装载的包（架构设计称 19）

```
$ grep -rhoE 'zhangzongchu2019/dingwei/internal/[a-z0-9]+' cmd/workpulse --include=*.go | grep -v _test | sed 's|.*/||' | sort -u | paste -sd' '
  admin bus clock config coordination datad evidence fcs feishu llm m8 model portal reminder schedule scheduler secretbox store worker
  (rc=0)
```

### internal/view 是否存在（架构设计称不存在）

```
$ ls -d internal/view 2>&1 || true
  ls: cannot access 'internal/view': No such file or directory
  (rc=0)
```

### 仓储包外的库句柄处数（判据：恒为 0；口径与实数均出自 scripts/repo-boundary-gate.sh）

```
$ scripts/repo-boundary-gate.sh
  ⑦ 仓储包边界（SP7a 甲组·收口 / §22.2c #1 两半）
    仓储包允许清单（本次生效的全部条目）：
      - internal/repo
    判据一·仓储包外建库句柄：4 处（基线 4，终值 0）
      internal/store/sqlite.go:53:	db, err := sql.Open("sqlite", dsn)
      internal/mcs/board/board.go:62:	db, err := sql.Open("sqlite", path)
      internal/datad/assignment_pg_integration_test.go:58:	w.appDB, err = sql.Open("pgx", dsn)
      internal/datad/assignment_pg_integration_test.go:64:		w.setupDB, err = sql.Open("pgx", sup)
    债·尚未租户化的调用点：55 处（基线 55，终值 0，随 SP7b 归零）
      ↳ 按效果判（specs/tools/pending-debt），按领域各记各的棘轮：
         · datad：25 处（基线 25，SP7b 收口时本领域须归零）
         · fanout：30 处（基线 30，SP7b 收口时本领域须归零）
    债·显式 SystemCtx 调用点：12 处（基线 12；每处都是评审面，不许悄悄增长）
    判据二·裸库句柄逃出包外：1 处（基线 1，终值 0；允许清单对本条不豁免）
      internal/mcs/board/board.go:84:func-returns-handle DB
    ⑦ 持平（建库 4 · 逃逸 1）✓ 未回升；终值均为 0，尚未达标
  (rc=0)
```

### 四处存储的建表点（36 / 11 / 5 / 9）

```
$ for p in internal/store/migrations internal/mcs/board/schema.sql internal/fanout internal/datad; do printf '%-34s ' $p; grep -rhoP 'CREATE TABLE (IF NOT EXISTS )?\K\w+' $p | sort -u | wc -l; done
  internal/store/migrations          36
  internal/mcs/board/schema.sql      11
  internal/fanout                    5
  internal/datad                     9
  (rc=0)
```

### task 表跨存储重名（须在 SP7a 前定名，见 docs/task-table-collision-analysis.md）

```
$ grep -rlP 'CREATE TABLE (IF NOT EXISTS )?task' internal/mcs/board/schema.sql internal/datad/pgstore.go
  internal/mcs/board/schema.sql
  internal/datad/pgstore.go
  (rc=0)
```

### RLS 字面标记出现次数（仅覆盖下列三个字面量，**不足以代表 RLS 合同的全部实现形态**）

```
$ grep -rn 'row_level_security\|SET LOCAL\|app\.tenant' --include=*.go --include=*.sql . | wc -l
  0
  (rc=0)
```

> 🔴 **本项零命中不得外推为「RLS 零实现」** —— 它只说明这三个字面量没出现。
> 现行裁定为 C.3a「PG ＋ RLS（人层）✅ 采纳」；RLS 合同的验收判据见架构 A§12 与 SpecDoc §22.2c，
> **其覆盖面远大于本项**。扩 detector 或改判据前，本项只作字面子集旁证（甲域 RV-甲41）。

## 四、本包未覆盖的（明说，不留白）
| 项 | 为什么本包给不出 |
|---|---|
| 三类越权负测（伪造 tenant / 跨租户读 / 缺 context） | 🔴 **受控仓储 API 尚未实现**，负测无被测对象。架构设计 A§12 已标「未建」 |
| 紧急面平台不可用负测 | 需实际部署环境，本包为静态仓库分析 |
| 敏感内容判定器覆盖面 | 判定器未实现（§22.5 为目标态） |
| 语义层的一切 | 机器判不了，见本文件头注 |
