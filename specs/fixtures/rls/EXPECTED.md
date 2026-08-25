# 人层 RLS 三条负测的期望输出（乙域 RV-乙12）

✅ **本目录的 fixture 已在真实 PG16/17 上实跑**（2026-08-25，业主派工代跑）。
🔴 **首版失败三处，全部是执笔方的错**（详见 setup.sql 头注）：
  ① PG15+ public schema 不再默认授 CREATE；② GRANT 成员资格写在 SET ROLE 之后缺 ADMIN OPTION；
  ③ dw_migrate 未设 NOINHERIT ⇒ 自动继承 dw_owner，**t4 第三条负测根本不成立**。
兼容版跑 t4/t1/t2/t3 **全符合预期**，RLS 语义本身验证通过。
⚠️ **执笔方未持有原始日志**：`/data/dw-rls-fixture/logs/*.log` 在本机不可达，
  故本节据**派工转述**记录，**非执笔方自取**。原始日志入仓前，
  「已实跑」这一事实的载体仍在代跑方手上。

⚠️ 以下为历史说明（首版时的阻塞，留档）：
· 执笔方环境：无 `psql`／`postgres` 二进制，`docker` 守护进程不可达
  （`permission denied … /var/run/docker.sock`）；
· 🔴 **乙域门方环境同样没有**：`command -v psql/postgres/pg_ctl/pg_isready` 均无输出、
  `docker` rc=1 —— 其自报的四条 PG 命令**也未执行**。
⇒ **代跑须落到有 PG 环境的一方（ops）**，不是「门方自然会跑」。
🔴 **故本节只是【期望】，不是【实测结果】**；在有人真跑过并贴出输出之前，
**不得据此声称 C.3a.1a 已验收**。这与本仓「ops 无主机权限」属同一类阻塞。

**执行前置（乙域 RV-乙13）**：
1. 🔴 **只在隔离的测试库里跑**（`createdb dw_rls_fixture`），**不得对任何真实环境执行** ——
   `setup.sql` 会 DROP ROLE / DROP TABLE。
2. 执行账号须有 **CREATEROLE** 权限，并能 `SET ROLE`（脚本内已 `GRANT … TO CURRENT_USER`）。
3. 🔴 三个 role 一律 **NOLOGIN、无口令** —— **fixture 不得在仓内留任何凭据**；
   它们只用于 `SET ROLE` 切换权限面，不用于真实连接。

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
