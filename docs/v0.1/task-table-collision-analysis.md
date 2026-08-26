# `task` 表跨存储同名不同义 —— ✅ 已裁（业主 2026-08-24）

> 🔴 **本件已由「工程建议」转为「裁定的依据材料」。**
> **权威定义在 SpecDoc §5.6a**，不在本件 —— 本件只是当初的取证与建议过程。
> ⚠️ 结构域重走 ARCH-01 判「工程建议不能当权威定义」，**正确**：
> 一份分析无论多有道理，都不构成裁定。裁定是业主给的，落点是规范。
>
> **裁定：`datad.task` 留名 `task`；`board.task` 改名 `board_task`。**
> 实际改表随 board 收编做（SP7a / SP2），§4.5：无 conformance suite 前不得重写 board。

> 本席自查项（架构设计 **A§10** 已标为「须在 SP7a 之前定掉」；
> ⚠️ 架构设计自身章节用 `A§N`，SpecDoc 用 `§N` —— 跨文档引用须带来源类型）。
> ⚠️ **本件不改架构设计正文** —— 该件正在三域评审，动它会打断锚。评审收敛后再并入。

## 一、事实（实测，非推断）

两处**各自建了一张叫 `task` 的表**，字段、主键、状态枚举**无一重合**：

| | `board.task`（SQLite · `internal/mcs/board/schema.sql`） | `datad.task`（PG · `internal/datad/pgstore.go`） |
|---|---|---|
| 主键 | `task_id` | `id` |
| 归属 | `board_id` → `orchestrator(board_id)` | `owner_session` |
| 状态枚举 | `planning / running / done / failed / partial` | `proposed / dispatched / in_progress / review / blocked / waiting_gate / done / archived` |
| 特有字段 | `root_body` · `origin_addr` · `origin_envelope_id` · `result_version` · `critical_path_len` | `anchor` · `gate` · `eta` · `detail JSONB` |
| 引用面 | 7 个文件（board.go · audit.go · c26_seats.go · c6_owner.go · panel.go 等） | 3 个文件（pgstore.go · assignment_pg.go 及其集成测试） |

**两个状态枚举唯一的交集是 `done`。** 这不是「同一概念的两种实现」，是**两个不同概念用了同一个名字**。

## 二、它们各自是什么

- **`board.task`** ＝ **编排单元**：带 DAG（`critical_path_len`）、带来源信封（`origin_envelope_id`）、带结果代次（`result_version`）。它是 `mcsd` 调度语义的一部分。
- **`datad.task`** ＝ **工作项**：带 `gate`、`anchor`、`eta`、`owner_session`。它是**人在看板上看到的那个任务**。

## 三、当初的建议（已被业主采纳，此处保留取证过程）

🔴 **`datad.task` 保留 `task` 之名；`board.task` 改名 `board_task`。**

**判据不是改动量，是语义匹配**（改动量恰好相反：board 侧 7 文件 vs datad 侧 3 文件）：

SpecDoc 里的「任务」概念 —— §5.7 任务看板、§7 门禁纪律、§11.6 派发快照 ——
说的是**带门、带锚、可派发**的那个东西。`datad.task` 的字段（`gate` / `anchor` / `dispatched`）逐项对得上；
`board.task` 的字段（`root_body` / `origin_envelope_id` / `critical_path_len`）对不上，它描述的是编排内部结构。

⇒ **谁更像规范里那个词，谁留名。** 这条判据可复核，"谁改动小"不可复核。

## 四、执行时机

**不是现在。** §4.5 已定：**无 conformance suite 之前，不得以「重写」为由丢弃任一既有并发语义**；
架构设计 §12 也把 board 收编列为**优先级 3，放在有 conformance suite 之后**。

⇒ **现在定名，改名随 board 收编一起做**。定名与改名分开，是为了让 SP7a 的其余部分
（datad/pgstore 与 fanout 的收编，优先级 1 与 2）**不被 board 阻塞**。

## 五、须一并处理的连带

- `dispatch.task_id` 外键指向 `datad.task(id)` —— 保留名不变，此外键不受影响。
- board 侧 7 个文件的 SQL 与 Go 结构体需同步改名，属机械改动，但**须有 conformance suite 兜底**。
- 改名后应加一条恒零计数器：**「同一表名在两处存储各自建表的数」恒为 0**（同 E.3 #47b「权威定义唯一」的机器化）。
