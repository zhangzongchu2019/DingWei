# 评审证据包（作者产出，供门方复算）

生成时刻：2026-08-26T08:26:22Z
分支头：`7a5eb75e7c62667ac69ef7d4d95ea4eb4d7755c4`

> 🔴 **本包不含任何门方结论。** 每行给出【命令】与【该次输出】，门方可原样重跑。
> ⚠️ **机器旁证的能力边界**：它们证明的是「形状对不对」，不是「语义对不对」。
> 语义层判断（引对号是否引错义、枚举是否可反推、effect 是否真的单调收紧）**机器判不了**，归人审。

## 评审对象内容指纹（本包分析的就是这些字节）

```
REVIEWED 4fb89aa958c784b963e59f9f1ad49a8c9ee5693a90e662a9dfecf1627148e950  docs/dingwei-platform-specdoc.md
REVIEWED be8345feb9b471e69c8387e5970ffc37635f9cb5dc3ffe1a2f2e1e2ef75b6d79  docs/dingwei-architecture-design.md
REVIEWED 6003718242079f24862c3cc635f6b2f35d709130c2c8c176048e7df56e73149e  docs/dingwei-prototype.html
REVIEWED 01c962eabf6df51a880a7104b928383f914e3f0966e8cf8bc7b5a7dbe855bc64  docs/DELIVERY-STATUS.md
REVIEWED 8996a7df3ae0e99db2aaef56d2d13126ded09c01787167ecebcf47c7f3372798  specs/evidence/collect.sh
REVIEWED 51ae52b28d3d3b0af6f5114bedc246c497179c6035bfd519611d30b86d72e125  scripts/appendix-e-owner-gate.sh
REVIEWED 2c1688e8431b0819e9ac6fc58d254c983284d788d86250562195345dffcc9a9b  scripts/arch-refcheck.sh
REVIEWED 253a59f6e85c9a1438beeac339efdc2294b21ae3bba46aaf30e7d1ee9befb521  scripts/check-sensitive.sh
REVIEWED 3cb2373018a75efa47200d682761a2ff965d54af57fdf37539ae8d04bd13de6e  scripts/checklist-executed-gate.sh
REVIEWED 69b60fda965d49479f0390a583810b082c1f049b30b4d04af2127f3eb13d43c3  scripts/conflict-marker-gate.sh
REVIEWED 687300bc3e8a56f491c533116b6cccc6da256f4e7aaf300607cd32cb6274e78e  scripts/delivery-check.sh
REVIEWED 14158e52ea0e4a0e5d21766e438d31a0a069008e8ec9b3fcc742599ca43aa93a  scripts/dependency-ratchet.sh
REVIEWED 33d372ff3b1dcda466476c113af888b55e02dee8479a894c8b0c48d8e9a45013  scripts/evidence-fresh-gate.sh
REVIEWED 97ad3840fa91f6f739fea89dc507f88f1a4071fdf7856f208126f39e2cc1883f  scripts/gate-witness-gate.sh
REVIEWED 8be0b821c0ad2574722f3e4032f8d91f599fc623bc42f1334dd491aed20e868e  scripts/gen-review-state.sh
REVIEWED 156c626fe5e069606e07f642d5f7fd3477b5ce42f1173d28b40e425f4556b883  scripts/gen-status.sh
REVIEWED 6dd8dd1441776bde1b3f6b79ed9dde7f7fdb353e6b6105a604f1e9b4b40a36e6  scripts/input-anchor-gate.sh
REVIEWED 510ae09c71ca9ac2f4d44ef74d1530cbe03d5a3560179fc4ad03c280c9f30d3f  scripts/legacy-name-gate.sh
REVIEWED e671108564fa7d404c2b6b53340ed202a55beb6351c293196a0668f82108e05f  scripts/na3-pg-evidence.sh
REVIEWED 3ad200b42177d6035dd7010f6d3ac93453888637977386b3614772c91758de9d  scripts/path-anchor-gate.sh
REVIEWED 1d2c1ef512417b70760dbe2a4bd314935f319fae23fbe39f8d20ea4202cbd138  scripts/post-columns-gate.sh
REVIEWED fb7fb00cfa2014aab8c2c9e252a50ee9425995eec3f1c937765a519e1b398ec8  scripts/prose-style-gate.sh
REVIEWED 15464d1f637362c0ca022c951c6bfe17b8a84e2f38df139320a2b633da882650  scripts/reliable_delivery_canary.sh
REVIEWED 29fefc4850804f2f9250241b8c9584d19dfc183e7987c90c25819f2e58a10ed9  scripts/repo-boundary-gate.sh
REVIEWED 679e8c6f73f15972895348cab70bf184c4b3763e64efb05ab1de0f716c661a6b  scripts/reviewer-repro-gate.sh
REVIEWED bf1e2ba70efd82d87bd7b39c9cd75ddbc20ed636374aed3c7488c71f01eab451  scripts/rv-integrity-gate.sh
REVIEWED 5c341b4b26763a3948cd12980a4f8ca2b90f4b9e9903a8028a59aec0210b7eaa  scripts/speclint-gate.sh
REVIEWED 65ca074adfd69fd0315f78991b9bfe21579f27e7d8f3e586fc85b60b857149b6  scripts/specs-index-check.sh
REVIEWED 520edd4fa3e440f205e38931749429c79962bb0a27324162a2c362a586594714  scripts/status-consistency-check.sh
REVIEWED e70f8ffe8fa0099cc7d83dcbf63006531149c4ca685605b0ae7ea5dd4e800e18  scripts/superseded-claims-gate.sh
REVIEWED a5fcd7cfbd7194d44a10fd7bba42c27f0b7258c09861d6ecf39a7337e294f66f  scripts/tenant-isolation-gate.sh
REVIEWED 71f0a4d5a35746101e540521df36975adf57803faa233fb0d27bcd0c73fa86f8  scripts/title-version-gate.sh
REVIEWED 74c9a7fcd8a2b81cfb8150e9e8caf8ae507ba2efd0d5214e097d8c62873cd8c1  scripts/workflow-coverage-gate.sh
REVIEWED 91102ae852827ee0b7ecfe122d29f13c95ff73d49752be29f3991f2ca93b2832  specs/tools/analyze.sh
REVIEWED fa2e42ef5aabe1418512e2e3b4cf4a103c978a4bcfd80e0c06db4c536ee092d6  specs/tools/disposition-audit.py
REVIEWED 4846536afab7911f005f9dd35c657b03f083da3ec3c3a377d6d0c1b392b03b68  specs/tools/export-bundle.sh
REVIEWED 41e915d3132b580332d7fa984979c6740ad6a892733f57d14d1192ddce83c778  specs/tools/foreign-version-check.py
REVIEWED c598b3199e0c418af5d6f207fff5a4b2e093edf0e3412562bccd8524612a337b  specs/tools/rv-state-check.py
REVIEWED 49b6196087629e5500f99da9b11a74a7bf035f79e0db24988e9194839d264288  specs/tools/safe-replace.py
REVIEWED 9328cd8abaf6e6ceacf8e0968e8431ae28080ca67044c32457c9283ef467b552  docs/tools/speclint.py
REVIEWED 2ca28e44ad27f466487859548c69cf3f321a2f03200fbe1731fe36ca75651e5a  specs/gates/boundary-baseline.txt
REVIEWED 16e5ba25e2b662a97269723d229d138cd7f0e2ebc7bd5177f9575eb2470fdbae  specs/gates/checklist-execution-exceptions.txt
REVIEWED 659cb7dd7e4a9ae1c57ea5575f52f008595b5fd55eec319f8ed697e71b7d2760  specs/gates/dependency-baseline.txt
REVIEWED fecc8c2f2a3e8ddfd515d492cac5c2d2560f88cc3729c53fad31c0f2a46665b9  specs/gates/disputed.txt
REVIEWED 5f5a725e0536ec20bb07ee5fd0dd0820c8545ad871cfd5e0dece61ce1edb1670  specs/gates/gate-witness-baseline.txt
REVIEWED ba6921e8c0eee7ff9ae0de1a910ac4a5041ecd7c44290e9c6897bad194f9d52c  specs/gates/gate-witness-debt.txt
REVIEWED 377197b6f96de93701b7200290f574ab485015bfcd6cef1bb98e1432ec830461  specs/gates/legacy-name-baseline.txt
REVIEWED c018da4f493698bbae2b8a0a122845867de79328d59304f3df202083ec68d34c  specs/gates/path-anchor-exceptions.txt
REVIEWED ec07bbba5e69f6794072de96ed04fd17d15aea1a9664c4d0a20da283dd24a322  specs/gates/path-anchor-suppression-baseline.txt
REVIEWED 8b9462995408ffbd9c4e5cbf3c3000e485ddd854f08db046445018d004cdcec5  specs/gates/prose-style-exceptions.txt
REVIEWED 94ffbd1b3558270275844d09762ce9fa176d367eadafa7ca9dc182d001a8ad3c  specs/gates/prose-style-suppression-baseline.txt
REVIEWED 861f36f0dfeda164e4047bfd75ea78e5977d3ad352c8e1c5cafff6a790945530  specs/gates/repo-packages.txt
REVIEWED a9c3bab0c5dc512ddc901e0ce298a6e17626243c62130b47810cfd49e4425311  specs/gates/reviewer-repro.txt
REVIEWED f586d7e0438528aadc0d61ac18402223a6bd7123d474a2e327fff11011493c7a  specs/gates/reviewers.txt
REVIEWED 706c97c396a087fcdedd8f4a85d12403426ebd8fdce31f1b7f952012f2260d40  specs/gates/rv-ledger.txt
REVIEWED bf0f4bc8b748be02979d9ec124ec39511a42752e4a9476cbf3aae9881054b410  specs/gates/status-consistency-exceptions.txt
REVIEWED e7a91c5e523a367419df93ea42bb570bbb33a08479ff8e5ec4ff3d01fa7e2789  specs/gates/status-stale-exceptions.txt
REVIEWED 480c24cb5e1ee6a6499543f99261df205b8a22ffcc6e846b473b6a583332a739  specs/gates/status-suppression-baseline.txt
REVIEWED 90953e322cb6563fc29462030cf5654106279b889c3aa422fe9e9d0c28905f80  specs/gates/superseded-claims-exceptions.txt
REVIEWED e1d988fcb25de950f740fb3b249ed1b8979c8eaf4cd79d9689a46e4679400ac0  specs/gates/superseded-claims.txt
REVIEWED c7f67c9f43a5c5e149bf7282dd1b65bfb840b353e2950940d952d8b1326b4689  specs/gates/superseded-suppression-baseline.txt
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
    原型引用 119 个节号；悬空 0 ✓
  ④a 三件的【自述】版本是否与 DELIVERY-STATUS 一致
    SpecDoc: 件内 v3.69 · 状态文件 v3.69 ✓
    架构设计: 件内 v0.34 · 状态文件 v0.34 ✓
    原型: 件内 v1.43 · 状态文件 v1.43 ✓
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
> 现行裁定为 C.3a「PG ＋ RLS（人层）✅ 采纳」；RLS 合同的验收判据见架构 A§12 与 SpecDoc §22.2c，
> **其覆盖面远大于本项**。扩 detector 或改判据前，本项只作字面子集旁证（甲域 RV-甲41）。

## 四、本包未覆盖的（明说，不留白）
| 项 | 为什么本包给不出 |
|---|---|
| 三类越权负测（伪造 tenant / 跨租户读 / 缺 context） | 🔴 **受控仓储 API 尚未实现**，负测无被测对象。架构设计 A§12 已标「未建」 |
| 紧急面平台不可用负测 | 需实际部署环境，本包为静态仓库分析 |
| 敏感内容判定器覆盖面 | 判定器未实现（§22.5 为目标态） |
| 语义层的一切 | 机器判不了，见本文件头注 |
