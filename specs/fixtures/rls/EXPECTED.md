# 人层 RLS 三条负测的期望输出（乙域 RV-乙12）

✅ **已在真实 PostgreSQL 17.11 上实跑**（2026-08-25T15:10:03Z，代跑方 ops）
原始日志入仓：`specs/fixtures/rls/logs/pg17-noinherit-run-20260825T151003Z.log`（2709 字节，逐字保留）。
来源路径（代跑方）：`/data/dw-rls-fixture/logs/pg17-noinherit-run-20260825T151003Z.log`。
⚠️ **文件名保持与来源一致**（`pg17-noinherit-run-…`）—— 我入仓时曾改名为 `pg17-run-…`，
  那会让证据与其来源对不上；**证据件不改名**，改了就得另附对照。
✅ **失败日志亦已入仓**：`logs/original-setup-run-20260825T151003Z.log`（2315 字节，13 个 ERROR）。

### 🔴 失败日志带来的两处发现（派工转述里没有，是读原件读出来的）

**发现一：建面自检在【全线失败】时仍打印「通过样」。**
  该次运行 `CREATE TABLE` 被拒 ⇒ 后续 relation 全不存在 ⇒ policy / GRANT / INSERT 连锁失败，
  共 13 个 ERROR；**而末尾自检照样输出「三 role 齐、两列均 f」。**
  ⚠️ 自检只查了 role 属性，**没查建面的产物** —— 与本仓「闸报了通过其实没测」同族，
    这次出现在 fixture 自己身上。
  ⇒ 已扩自检：表在否 · RLS 启用且强制 · 四条 policy 齐 · 预置行数（含带上下文的 alice=1）。
    **首版失败时 `table_exists` 会是 `f`** —— 这一行就是该补丁的判据。

**发现二：`NOINHERIT` 那条【不是这份日志抓出来的】。**
  全文 `grep -c INHERIT` = **0**：setup 在建表处即失败，根本没走到能暴露继承语义的地方。
  ⇒ 三处修正中，①② 有日志实证，**③（NOINHERIT）是推理得出的**——
    它由「t4 第三条负测在 INHERIT 下测不出任何东西」这一分析支撑，
    并由通过版日志中该负测**确实报错**间接印证。
  ⚠️ **不得说「三处失败都由日志抓出」** —— 那会把一次推理算成一次实测。

## 期望 vs 实得（逐条对照，取自上述日志）

| 项 | 期望 | 实得 | |
|---|---|---|---|
| setup 建面自检 | 3 行，`rolcanlogin`/`rolbypassrls` 均 `f` | `dw_app/dw_migrate/dw_owner` 三行，两列均 `f` | ✅ |
| t4 正测 | dw_migrate 切属主后 ALTER **成功** | `ALTER TABLE` ×2 | ✅ |
| t4 拒测 dw_app ALTER | `must be owner of table demo_item` | 同左（:17）| ✅ |
| t4 拒测 dw_app CREATE POLICY | 同上 | 同左（:20）| ✅ |
| t4 拒测 **未切属主的 dw_migrate** | 同上 | 同左（:27）| ✅ 🔴 **靠 NOINHERIT 才成立** |
| t1 以他人身份 INSERT | RLS 违规 | `new row violates row-level security policy` | ✅ |
| t1 以他人身份 UPDATE | 拒或 0 行 | 同上（ERROR）| ✅ |
| t2 无上下文 SELECT | **0 行**而非全部 | `must_be_zero = 0` | ✅ |
| t2 无上下文 INSERT | RLS 违规 | 同左 | ✅ |
| t3 `alice_rows` | 1 | 1 | ✅ |
| t3 `must_be_unset` | **空**（PG17 返空串非 NULL）| 空 | ✅ |
| t3 `must_be_zero` | 0 ⇐ **判「无残留」以此为准** | 0 | ✅ |
| t3 负测④ 属主可自解除 | `owner_sees_before=0` → ALTER 成功 → `after=2` | 0 → `ALTER TABLE` → 2 | ✅ |

🔴 **本次实跑【未覆盖】的部分，须写明**：
1. **负测⑥（应用面不得取得属主/迁移 role）未跑** ——
   日志 `fixture_sha=d5a8cfe`，而负测⑥ 加在其后的 **5c103a0**。
   ⇒ 「t1/t2/t3/t4 全符合预期」这句**只对 d5a8cfe 那版 t4 成立**，不含负测⑥。
   ⚠️ 不得据此宣称 RV-乙16 的负测已验。
2. 代跑用的是**兼容版建面** `setup.pg15plus-noinherit.sql`（日志 note 自述），
   该文件**不在本仓**；本仓 `setup.sql` 已把三处修正**并入正文**。
   ⇒ 下次复跑请直接用 `specs/fixtures/rls/setup.sql`，并核对其输出与本表一致。
3. 仅在 **PG 17.11** 上跑过；PG16 未见日志（派工称 16/17，实测日志只有 17）。

⇒ **结论（按证据说，不外推）**：RLS 语义面（写入越权 · 无上下文 fail-closed ·
  事务后无残留 · DDL 权归属 · 属主可自解除）**已在 PG17 上验证**；
  **负测⑥、PG16、以及本仓 setup.sql 本身的实跑，尚未取得证据**。

跑法（任一 PG ≥ 13）：
```
psql -f setup.sql
psql -f t1-write-as-other.sql
psql -f t2-no-context-deny.sql
psql -f t3-no-residue.sql
psql -f t4-ddl-rights.sql
```

| 件 | 期望 |
|---|---|
| `t1-write-as-other.sql` | 两条 DML **均被拒**：INSERT 报 `new row violates row-level security policy`；UPDATE 报同样错误或影响 **0 行** |
| `t2-no-context-deny.sql` | `must_be_zero` = **0**（🔴 不是全部）；INSERT 报 RLS 违规 |
| `setup.sql` 末尾自检 | 返回 **3 行**（dw_app / dw_migrate / dw_owner），`rolcanlogin` 与 `rolbypassrls` **均为 `f`** |
| `t4-ddl-rights.sql` 负测⑥ | **撤销成员资格后**，dw_app 执行 `SET ROLE dw_owner` 与 `SET ROLE dw_migrate` **均须报 `permission denied to set role`** —— 应用面不得取得属主/迁移 role（乙域 RV-乙16）|
| `t4-ddl-rights.sql` | 🔴 **dw_migrate `SET ROLE dw_owner` 后 ALTER 成功**（DDL 来自**属主 role 成员资格**）· **dw_app 的 ALTER 与 CREATE POLICY 均报 `must be owner of table demo_item`** · **dw_migrate 不切属主时 ALTER 亦报错** —— 证明 `GRANT ALL ON <表>` 不含 DDL（乙域 RV-乙15）|
| `t3-no-residue.sql` 负测④ | `owner_sees_before` = **0**（FORCE 下属主也受约束）· `ALTER … NO FORCE` **成功** · `owner_sees_after` = **2**。🔴 **这不是缺陷，是要记录的事实**：属主可自解除 ⇒ FORCE RLS 防误操作、不防有意绕过；唯一防线是**审计留痕**（C.3a.1c） |
| `t3-no-residue.sql` | `alice_rows` = **1**；`must_be_unset` = **空字符串**（🔴 实跑修正：PG17 下 `current_setting(…,true)` 事务后返回**空串**而非 NULL，coalesce 取不到默认值 —— 原写 `<unset>` 是我按 NULL 想当然）；`must_be_zero` = **0** ⇐ **判「无残留」以此为准** |

🔴 **若 t3 的 `must_be_unset` 不是 `<unset>`**，说明实现用了残留型 `SET` 而非 `SET LOCAL`，
或领域 SQL 未经事务 —— 直接对应 C.3a.1a #5 / #5a / #5c，属阻断。
