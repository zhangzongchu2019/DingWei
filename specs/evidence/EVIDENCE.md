# 评审证据包（作者产出，供门方复算）

生成时刻：2026-08-27T08:00:29Z
分支头：`6f44052fbda846583dd2808be96f7a6c93191f30`

> 🔴 **本包不含任何门方结论。** 每行给出【命令】与【该次输出】，门方可原样重跑。
> ⚠️ **机器旁证的能力边界**：它们证明的是「形状对不对」，不是「语义对不对」。
> 语义层判断（引对号是否引错义、枚举是否可反推、effect 是否真的单调收紧）**机器判不了**，归人审。

## 评审对象内容指纹（本包分析的就是这些字节）

```
REVIEWED 7f27ecf27a435d34bbdef3fb221c3838d9924e4a29487b3396278d415d5c11ac  docs/dingwei-platform-specdoc.md
REVIEWED 88e3d654f446ae36be2c2efc003d54f60cc15cbe9907e4037fa9f4cae6fc8cf6  docs/dingwei-platform-architecture.md
REVIEWED f2995730ca986ae38993f62391312982e7ed5026dab990c78ff29a6bb00ef9cb  docs/dingwei-prototype.html
REVIEWED dd485e3788f367471bd77d1c4c916b20c9740a737149ed5923038674e1b8c8f1  docs/DELIVERY-STATUS.md
REVIEWED 0d564164092ba326a02de944c3b903531e54763006033819d3b3f1553d7a3061  specs/evidence/collect.sh
REVIEWED 51ae52b28d3d3b0af6f5114bedc246c497179c6035bfd519611d30b86d72e125  scripts/appendix-e-owner-gate.sh
REVIEWED 1e082148049b505073f71888f837aff3d364d378f28d60ab13319abe0745baf3  scripts/arch-refcheck.sh
REVIEWED 253a59f6e85c9a1438beeac339efdc2294b21ae3bba46aaf30e7d1ee9befb521  scripts/check-sensitive.sh
REVIEWED 3cb2373018a75efa47200d682761a2ff965d54af57fdf37539ae8d04bd13de6e  scripts/checklist-executed-gate.sh
REVIEWED f27017844893fd5678daf3e4e50c858215c0048e6f5bef05aa3eb5658ce33e39  scripts/conflict-marker-gate.sh
REVIEWED 687300bc3e8a56f491c533116b6cccc6da256f4e7aaf300607cd32cb6274e78e  scripts/delivery-check.sh
REVIEWED 14158e52ea0e4a0e5d21766e438d31a0a069008e8ec9b3fcc742599ca43aa93a  scripts/dependency-ratchet.sh
REVIEWED 3c6c9168529c0eef64f1cecaa8184a4808ab9b14bc6a010314268e791bf1a1c2  scripts/evidence-fresh-gate.sh
REVIEWED 566208fc33a196c2cc44947b892fda11d3c5feee5c63a55e910df9b071badfad  scripts/gate-witness-gate.sh
REVIEWED 00e2a7106e622c697a9b3c4e718110bdd7d51a6d75fe6bd59429de92201f2d28  scripts/gen-review-state.sh
REVIEWED 189cbe8280dc2a89006e0fd6495ffd7cd79a74d646667c4dcd93868ba1f8df75  scripts/gen-status.sh
REVIEWED 6dd8dd1441776bde1b3f6b79ed9dde7f7fdb353e6b6105a604f1e9b4b40a36e6  scripts/input-anchor-gate.sh
REVIEWED ca7853acbe8a958124bc1200ae89027881ccad3f1cdde9bd8933ad9f6cb41ba9  scripts/legacy-name-gate.sh
REVIEWED e671108564fa7d404c2b6b53340ed202a55beb6351c293196a0668f82108e05f  scripts/na3-pg-evidence.sh
REVIEWED 1b3f7a7bebaddd2315bef2c62c0b60bff4f5b666d0419068f2aa3b4edaef8b01  scripts/path-anchor-gate.sh
REVIEWED 1d2c1ef512417b70760dbe2a4bd314935f319fae23fbe39f8d20ea4202cbd138  scripts/post-columns-gate.sh
REVIEWED 668c60263af6cc277bed585cb29f119c4e25e050176a94fb188f147eca0d7932  scripts/prose-style-gate.sh
REVIEWED 15464d1f637362c0ca022c951c6bfe17b8a84e2f38df139320a2b633da882650  scripts/reliable_delivery_canary.sh
REVIEWED ca0bea89991080cb1c4a512b5f83fb00c20475602d4bfd3c4165d4d04cbebbb4  scripts/repo-boundary-gate.sh
REVIEWED 679e8c6f73f15972895348cab70bf184c4b3763e64efb05ab1de0f716c661a6b  scripts/reviewer-repro-gate.sh
REVIEWED e0aba3236b58915d416809d4e53b90244a0cc4a3f12e19212a3a07d7d1802f36  scripts/rv-integrity-gate.sh
REVIEWED 5e514f9491b33e506691bc559038c39f8611e240b1ab76e59b58f8c7b72b7880  scripts/speclint-gate.sh
REVIEWED 65ca074adfd69fd0315f78991b9bfe21579f27e7d8f3e586fc85b60b857149b6  scripts/specs-index-check.sh
REVIEWED 8ada7fc95e2e60bf1c566723d8ba54326f07b314970f90a1d4ed2066ee2fa879  scripts/status-consistency-check.sh
REVIEWED fe3933da644dce4430efbf359fa4a8bb5ef93708ffab8f63bdecac6c8d110793  scripts/superseded-claims-gate.sh
REVIEWED a5fcd7cfbd7194d44a10fd7bba42c27f0b7258c09861d6ecf39a7337e294f66f  scripts/tenant-isolation-gate.sh
REVIEWED fe0c2678f583e2fd7350442f3a3408816d6b9553f2dc3bdb08b849f6c3429d44  scripts/title-version-gate.sh
REVIEWED 74c9a7fcd8a2b81cfb8150e9e8caf8ae507ba2efd0d5214e097d8c62873cd8c1  scripts/workflow-coverage-gate.sh
REVIEWED fa79c3b52eb11badaed9c86831414b1a5bab8dbdae8ff394164632e67613f5e2  specs/tools/analyze.sh
REVIEWED e1a579471cd08c41bd64ac9798783232dca43488b6f584b5a1b172fa27d64b7a  specs/tools/disposition-audit.py
REVIEWED b615f76834a3e435d18437f6ac450b2647dc6086d9070cadff491b0428769cf8  specs/tools/export-bundle.sh
REVIEWED c9e7863be91d960226690197832bf66434ec20070a0c693fdcc892b576b6d7d3  specs/tools/foreign-version-check.py
REVIEWED c598b3199e0c418af5d6f207fff5a4b2e093edf0e3412562bccd8524612a337b  specs/tools/rv-state-check.py
REVIEWED 49b6196087629e5500f99da9b11a74a7bf035f79e0db24988e9194839d264288  specs/tools/safe-replace.py
REVIEWED 9328cd8abaf6e6ceacf8e0968e8431ae28080ca67044c32457c9283ef467b552  docs/tools/speclint.py
REVIEWED 2ca28e44ad27f466487859548c69cf3f321a2f03200fbe1731fe36ca75651e5a  specs/gates/boundary-baseline.txt
REVIEWED 16e5ba25e2b662a97269723d229d138cd7f0e2ebc7bd5177f9575eb2470fdbae  specs/gates/checklist-execution-exceptions.txt
REVIEWED 659cb7dd7e4a9ae1c57ea5575f52f008595b5fd55eec319f8ed697e71b7d2760  specs/gates/dependency-baseline.txt
REVIEWED fecc8c2f2a3e8ddfd515d492cac5c2d2560f88cc3729c53fad31c0f2a46665b9  specs/gates/disputed.txt
REVIEWED 5f5a725e0536ec20bb07ee5fd0dd0820c8545ad871cfd5e0dece61ce1edb1670  specs/gates/gate-witness-baseline.txt
REVIEWED ba6921e8c0eee7ff9ae0de1a910ac4a5041ecd7c44290e9c6897bad194f9d52c  specs/gates/gate-witness-debt.txt
REVIEWED d1a4012b0a30462467cdb80c39d6c131488700bb4d7acc13fbc8d617220bab10  specs/gates/legacy-name-baseline.txt
REVIEWED d7c98179543bba4c4cba6d32a76866ec5b9f597606896e3d04d84f1181842f27  specs/gates/path-anchor-exceptions.txt
REVIEWED 33c8ff7692409e497bc4f9a8041a284269e293baf96373641e2552704a52582f  specs/gates/prose-style-exceptions.txt
REVIEWED 861f36f0dfeda164e4047bfd75ea78e5977d3ad352c8e1c5cafff6a790945530  specs/gates/repo-packages.txt
REVIEWED a9c3bab0c5dc512ddc901e0ce298a6e17626243c62130b47810cfd49e4425311  specs/gates/reviewer-repro.txt
REVIEWED f586d7e0438528aadc0d61ac18402223a6bd7123d474a2e327fff11011493c7a  specs/gates/reviewers.txt
REVIEWED ab5724fd95b61eb26acc7a5182782fe65b34fb82cd03b948bdd03b55a624e1ac  specs/gates/rv-ledger.txt
REVIEWED 55ad013e6cc145efeb4d991710de331aa1e9e9b88676db50a1011c142468eda0  specs/gates/status-consistency-exceptions.txt
REVIEWED e7a91c5e523a367419df93ea42bb570bbb33a08479ff8e5ec4ff3d01fa7e2789  specs/gates/status-stale-exceptions.txt
REVIEWED 90953e322cb6563fc29462030cf5654106279b889c3aa422fe9e9d0c28905f80  specs/gates/superseded-claims-exceptions.txt
REVIEWED e1d988fcb25de950f740fb3b249ed1b8979c8eaf4cd79d9689a46e4679400ac0  specs/gates/superseded-claims.txt
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

### 目标架构引用核验（数字 heading / § SpecDoc 分别解析）

```
$ scripts/arch-refcheck.sh
  目标架构 heading：89 个 · 重复 0
  SpecDoc 引用 §：72 次 / 50 目标 · 悬空 0
  旧 A§ 引用：0
  --- 引用核验通过
  (rc=0)
```

### 引用核验的反测四项（含 SpecDoc heading 重复 —— 曾漏接失败条件）

```
$ scripts/arch-refcheck.sh --selftest
    ✓ 拦住：SpecDoc 悬空引用
    ✓ 拦住：旧 A§ 命名空间回流
    ✓ 拦住：目标架构 heading 重复
    ✓ 正常件放行
  --- 反测 4 项全过（闸有牙）
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
    原型引用 119 个节号；悬空 0 ✓
  ④a 三件的【自述】版本是否与 DELIVERY-STATUS 一致
    SpecDoc: 件内 v3.75 · 状态文件 v3.75 ✓
    架构设计: 件内 v1.4 · 状态文件 v1.4 ✓
    原型: 件内 v1.48 · 状态文件 v1.48 ✓
  ④b 正文是否出现【他件】的旧版本号
    ✓ dingwei-platform-specdoc.md: 正文无他件旧版本号
    ✓ dingwei-platform-architecture.md: 正文无他件旧版本号
    ✓ dingwei-prototype.html: 正文无他件旧版本号
    ✓ 架构输入表：SpecDoc/原型版本与 sha256 逐项等于当前件
  ⑤ 生效前置状态（§0.4）
    ✓ 三域 checklist 齐备（结构 / 安全 / 跨制品）—— 但【生效与否取决于门方执行结果，非文件存在】
  ---
  跨制品一致性检查通过（不含语义层，见脚本头注）
  (rc=0)
```

## 三、代码现状实测（目标架构第 12 节现状映射的证据来源）
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
      internal/mcs/board/board.go:62:	db, err := sql.Open("sqlite", path)
      internal/store/sqlite.go:53:	db, err := sql.Open("sqlite", dsn)
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

### task 表跨存储重名（须在 SP7a 前定名，见 docs/v0.1/task-table-collision-analysis.md）

```
$ grep -rlP 'CREATE TABLE (IF NOT EXISTS )?task' internal/mcs/board/schema.sql internal/datad/pgstore.go
  internal/mcs/board/schema.sql
  internal/datad/pgstore.go
  (rc=0)
```

### RLS 字面标记出现次数（仅覆盖下列三个字面量，**不足以代表 RLS 合同的全部实现形态**）

```
$ grep -rn 'row_level_security\|SET LOCAL\|app\.tenant' --include=*.go --include=*.sql . | wc -l
  8
  (rc=0)
```

> 🔴 **本项零命中不得外推为「RLS 零实现」** —— 它只说明这三个字面量没出现。
> 现行裁定为 C.3a「PG ＋ RLS（人层）✅ 采纳」；RLS 合同的验收判据见目标架构第 8.3 节与 SpecDoc §22.2c，
> **其覆盖面远大于本项**。扩 detector 或改判据前，本项只作字面子集旁证（甲域 RV-甲41）。

## 四、本包未覆盖的（明说，不留白）
| 项 | 为什么本包给不出 |
|---|---|
| 三类越权负测（伪造 tenant / 跨租户读 / 缺 context） | 🔴 **受控仓储 API 尚未实现**，负测无被测对象。目标架构第 8.2、18.3 节保留为交付验证项 |
| 紧急面平台不可用负测 | 需实际部署环境，本包为静态仓库分析 |
| 敏感内容判定器覆盖面 | 判定器未实现（§22.5 为目标态） |
| 语义层的一切 | 机器判不了，见本文件头注 |
