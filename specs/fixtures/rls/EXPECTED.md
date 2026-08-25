# 人层 RLS 三条负测的期望输出（乙域 RV-乙12）

⚠️ **本目录的 fixture 尚未在真实 PostgreSQL 上跑过。**
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
```

| 件 | 期望 |
|---|---|
| `t1-write-as-other.sql` | 两条 DML **均被拒**：INSERT 报 `new row violates row-level security policy`；UPDATE 报同样错误或影响 **0 行** |
| `t2-no-context-deny.sql` | `must_be_zero` = **0**（🔴 不是全部）；INSERT 报 RLS 违规 |
| `setup.sql` 末尾自检 | 返回 **3 行**（dw_app / dw_migrate / dw_owner），`rolcanlogin` 与 `rolbypassrls` **均为 `f`** |
| `t3-no-residue.sql` | `alice_rows` = **1**；`must_be_unset` = **`<unset>`**；`must_be_zero` = **0** |

🔴 **若 t3 的 `must_be_unset` 不是 `<unset>`**，说明实现用了残留型 `SET` 而非 `SET LOCAL`，
或领域 SQL 未经事务 —— 直接对应 C.3a.1a #5 / #5a / #5c，属阻断。
