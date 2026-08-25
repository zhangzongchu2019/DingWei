# 评审证据包（作者产出，供门方复算）

生成时刻：2026-08-25T03:18:38Z
分支头：`d0322c6663772f0098802a050a8b28cc08d053b5`

> 🔴 **本包不含任何门方结论。** 每行给出【命令】与【该次输出】，门方可原样重跑。
> ⚠️ **机器旁证的能力边界**：它们证明的是「形状对不对」，不是「语义对不对」。
> 语义层判断（引对号是否引错义、枚举是否可反推、effect 是否真的单调收紧）**机器判不了**，归人审。

## 一、单件闸
### SpecDoc speclint（受控符号 · 字段声明 · 表完整性 · E 计数 · 权威定义唯一 等 14 条规则）

```
$ python3 docs/tools/speclint.py docs/dingwei-platform-specdoc.md
  --- 全部通过
```

### speclint 语料自检（正反例须拦在预期规则上）

```
$ python3 docs/tools/speclint.py --selftest docs/tools/speclint-corpus.md
  --- 语料 45 例 / 覆盖规则 16 条,全部通过
```

### speclint 变异体（改坏规则，其反例须随之失守 —— 证明语料有牙）

```
$ python3 docs/tools/speclint.py --mutate docs/tools/speclint-corpus.md
  --- 变异体 16 个,全部如期失守(语料有牙)
```

### 架构设计引用核验（A§ 本件 / § SpecDoc 分别解析）

```
$ scripts/arch-refcheck.sh
  本件内引用 A§ : 56 次 / 27 目标 · 悬空 0
  SpecDoc 引用 § : 163 次 / 67 目标 · 悬空 0
  heading 真实重复：本件 0 · SpecDoc 0
  --- 引用核验通过
```

### 引用核验的反测四项（含 SpecDoc heading 重复 —— 曾漏接失败条件）

```
$ scripts/arch-refcheck.sh --selftest
    ✓ 拦住：SpecDoc 悬空 §99.9
    ✓ 拦住：本件悬空 A§77.7
    ✓ 拦住：本件 heading 重复
    ✓ 拦住：SpecDoc heading 重复
  --- 反测四项全过（闸有牙）
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
    原型引用 115 个节号；悬空 0 ✓
  ④ 三件的版本自述是否与 DELIVERY-STATUS 一致
    SpecDoc: 件内 v3.23 · 状态文件 v3.23 ✓
    架构设计: 件内 v0.20 · 状态文件 v0.20 ✓
    原型: 件内 v1.19 · 状态文件 v1.19 ✓
  ⑤ 生效前置状态（§0.4）
    ✓ 三域 checklist 齐备（结构 / 安全 / 跨制品）—— 但【生效与否取决于门方执行结果，非文件存在】
  ---
  跨制品一致性检查通过（不含语义层，见脚本头注）
```

## 三、代码现状实测（架构设计 A§6.2 所载数字的来源）
### internal 包总数

```
$ ls -d internal/*/ | wc -l
  28
```

### workpulse 装载的包（架构设计称 19）

```
$ grep -rhoE 'zhangzongchu2019/dingwei/internal/[a-z0-9]+' cmd/workpulse --include=*.go | grep -v _test | sed 's|.*/||' | sort -u | paste -sd' '
  admin bus clock config coordination datad evidence fcs feishu llm m8 model portal reminder schedule scheduler secretbox store worker
```

### internal/view 是否存在（架构设计称不存在）

```
$ ls -d internal/view 2>&1 || true
  ls: cannot access 'internal/view': No such file or directory
```

### 仓储包外的库句柄处数（判据：恒为 0；口径与实数均出自 scripts/repo-boundary-gate.sh）

```
$ scripts/repo-boundary-gate.sh
  ⑦ 仓储包边界（SP7a 甲组·收口 / §22.2c #1 两半）
    仓储包允许清单（本次生效的全部条目）：
      - internal/repo
    判据一·仓储包外建库句柄：4 处（基线 4，终值 0）
      internal/store/sqlite.go:51:	db, err := sql.Open("sqlite", dsn)
      internal/mcs/board/board.go:62:	db, err := sql.Open("sqlite", path)
      internal/datad/assignment_pg_integration_test.go:58:	w.appDB, err = sql.Open("pgx", dsn)
      internal/datad/assignment_pg_integration_test.go:64:		w.setupDB, err = sql.Open("pgx", sup)
    债·尚未租户化的调用点：59 处（基线 59，终值 0，随 SP7b 归零）
    债·显式 SystemCtx 调用点：12 处（基线 12；每处都是评审面，不许悄悄增长）
    判据二·裸库句柄逃出包外：1 处（基线 1，终值 0；允许清单对本条不豁免）
      internal/mcs/board/board.go:84:func-returns-handle DB
    ⑦ 持平（建库 4 · 逃逸 1）✓ 未回升；终值均为 0，尚未达标
```

### 四处存储的建表点（36 / 11 / 5 / 9）

```
$ for p in internal/store/migrations internal/mcs/board/schema.sql internal/fanout internal/datad; do printf '%-34s ' $p; grep -rhoP 'CREATE TABLE (IF NOT EXISTS )?\K\w+' $p | sort -u | wc -l; done
  internal/store/migrations          36
  internal/mcs/board/schema.sql      11
  internal/fanout                    5
  internal/datad                     9
```

### task 表跨存储重名（须在 SP7a 前定名，见 docs/task-table-collision-analysis.md）

```
$ grep -rlP 'CREATE TABLE (IF NOT EXISTS )?task' internal/mcs/board/schema.sql internal/datad/pgstore.go
  internal/mcs/board/schema.sql
  internal/datad/pgstore.go
```

### RLS 相关代码（C.3 裁定不做 RLS；实测本就零实现）

```
$ grep -rn 'row_level_security\|SET LOCAL\|app\.tenant' --include=*.go --include=*.sql . | wc -l
  0
```

## 四、本包未覆盖的（明说，不留白）
| 项 | 为什么本包给不出 |
|---|---|
| 三类越权负测（伪造 tenant / 跨租户读 / 缺 context） | 🔴 **受控仓储 API 尚未实现**，负测无被测对象。架构设计 A§12 已标「未建」 |
| 紧急面平台不可用负测 | 需实际部署环境，本包为静态仓库分析 |
| 敏感内容判定器覆盖面 | 判定器未实现（§22.5 为目标态） |
| 语义层的一切 | 机器判不了，见本文件头注 |
