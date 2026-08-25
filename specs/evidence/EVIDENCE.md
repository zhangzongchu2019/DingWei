# 评审证据包（作者产出，供门方复算）

生成时刻：2026-08-25T00:29:23Z
分支头：`05ccf171d3d45ebd25f24495af32563eb79b61b5`

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
  --- 语料 42 例 / 覆盖规则 15 条,全部通过
```

### speclint 变异体（改坏规则，其反例须随之失守 —— 证明语料有牙）

```
$ python3 docs/tools/speclint.py --mutate docs/tools/speclint-corpus.md
  --- 变异体 15 个,全部如期失守(语料有牙)
```

### 架构设计引用核验（A§ 本件 / § SpecDoc 分别解析）

```
$ scripts/arch-refcheck.sh
  本件内引用 A§ : 54 次 / 27 目标 · 悬空 0
  SpecDoc 引用 § : 161 次 / 66 目标 · 悬空 0
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
    SpecDoc: 件内 v3.19 · 状态文件 v3.19 ✓
    架构设计: 件内 v0.14 · 状态文件 v0.14 ✓
    原型: 件内 v1.16 · 状态文件 v1.16 ✓
  ⑤ 生效前置状态（§0.4）
    ✓ 三域 checklist 齐备（结构 / 安全 / 跨制品）—— 但【生效与否取决于门方执行结果，非文件存在】
  ---
  跨制品一致性检查通过（不含语义层，见脚本头注）
```

## 三、代码现状实测（架构设计 A§6.2 所载数字的来源）
### internal 包总数

```
$ ls -d internal/*/ | wc -l
  27
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

### 仓储包外 sql.Open 处数（判据：恒为 0；现为 8 ⇒ 不达标）

```
$ grep -rhoE 'sql\.Open\("(pgx|sqlite)"' --include=*.go internal | wc -l
  8
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
