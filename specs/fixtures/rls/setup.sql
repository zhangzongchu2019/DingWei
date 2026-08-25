-- 人层 RLS fixture · 建面（乙域 RV-乙12 / RV-乙13 / RV-乙15）
--
-- 🔴 **本件已在真实 PG16/17 上实跑并据实修正**（业主派工，2026-08-25）。
--   首版在真实 PG 上**失败三处，全部是执笔方的错**：
--     ① `dw_owner` 无 public schema 的 CREATE 权 —— **PG15+ 起 public schema 不再默认授 CREATE**，
--        我按旧版 PG 的默认行为想当然；
--     ② `GRANT dw_owner TO dw_migrate` 写在 `SET ROLE dw_owner` **之后** ⇒ 缺 ADMIN OPTION 而失败。
--        成员资格应由**平台 ops 在建面阶段**授予，即须在 SET ROLE 之前；
--     ③ `dw_migrate` 未设 `NOINHERIT` —— 默认 INHERIT 下它**自动继承** dw_owner 权限，
--        🔴 于是 t4 的第三条负测「未切属主的 dw_migrate 亦须被拒」**根本不成立**：
--        它无需 SET ROLE 就有 DDL。**是 NOINHERIT 才让那条负测有意义。**
--   ⚠️ 这三处印证了我此前那句：「**更可能是我的合同写错，而非 PG 行为意外**」——
--     静态写得再全，不跑就不知道。
--
-- 🔴 执行前置，缺一不得跑：
--   1. 只在隔离的测试库里跑（如 createdb dw_rls_fixture），不得对任何真实环境执行
--      （本脚本会 DROP ROLE / DROP TABLE）。
--   2. 执行账号须有 CREATEROLE 权限（建三个 role），且须能 SET ROLE 到它们。
--   3. 🔴 三个 role 一律 NOLOGIN、无口令 —— 只用于 SET ROLE 切换权限面，不用于真实连接（乙域 RV-乙13）。
DROP TABLE IF EXISTS demo_item;
DROP ROLE IF EXISTS dw_app;
DROP ROLE IF EXISTS dw_migrate;
DROP ROLE IF EXISTS dw_owner;

-- 三具名 role，职责分离（C.3a.1a / C.3a.1c）：
--   dw_owner   建面（由企业 ops 持有：库内建表/改结构/建 policy）
--   dw_migrate 迁移期 DDL
--   dw_app     只跑领域 SQL（NOBYPASSRLS · 非属主 · 无 DDL）
-- ⚠️ 库本身由平台 ops 建（本 fixture 假定库已存在）；
--   🔴 属主可自解除 FORCE RLS（NO FORCE / DROP POLICY / OWNER TO）⇒ 风险收敛到企业 ops 单点
--     并以审计承载，不是被消除（C.3a.1c）。
CREATE ROLE dw_owner   NOLOGIN NOBYPASSRLS;
CREATE ROLE dw_migrate NOLOGIN NOINHERIT NOBYPASSRLS;
CREATE ROLE dw_app     NOLOGIN NOBYPASSRLS;
GRANT dw_owner, dw_migrate, dw_app TO CURRENT_USER;   -- 仅为本 fixture 能 SET ROLE
GRANT CREATE ON SCHEMA public TO dw_owner;      -- PG15+ default public schema no longer grants CREATE
GRANT dw_owner TO dw_migrate;                   -- platform ops establishes migration owner-role membership before SET ROLE

SET ROLE dw_owner;
CREATE TABLE demo_item (
  id              bigserial PRIMARY KEY,
  owner_principal text NOT NULL,
  body            text NOT NULL
);
ALTER TABLE demo_item ENABLE ROW LEVEL SECURITY;
ALTER TABLE demo_item FORCE ROW LEVEL SECURITY;

CREATE POLICY p_sel ON demo_item FOR SELECT
  USING (owner_principal = current_setting('dw.principal', true));
CREATE POLICY p_ins ON demo_item FOR INSERT
  WITH CHECK (owner_principal = current_setting('dw.principal', true));
CREATE POLICY p_upd ON demo_item FOR UPDATE
  USING      (owner_principal = current_setting('dw.principal', true))
  WITH CHECK (owner_principal = current_setting('dw.principal', true));
CREATE POLICY p_del ON demo_item FOR DELETE
  USING (owner_principal = current_setting('dw.principal', true));

-- 🔴 迁移期 DDL 归 dw_migrate；dw_app 无 DDL 权（C.3a.1a role 表）
-- ⚠️ GRANT ALL ON <表> 只给表级 DML，不给 DDL；ALTER TABLE / 建 policy 要求属主身份（或属主 role 成员）。
GRANT SELECT, INSERT, UPDATE, DELETE ON demo_item TO dw_app;
GRANT USAGE, SELECT ON SEQUENCE demo_item_id_seq TO dw_app;
GRANT ALL ON demo_item TO dw_migrate;          -- 表级 DML（仅此，不含 DDL）
RESET ROLE;

-- 预置两人各一行
SET ROLE dw_owner;
BEGIN; SET LOCAL dw.principal = 'alice';
  INSERT INTO demo_item(owner_principal, body) VALUES ('alice','a-1');
COMMIT;
BEGIN; SET LOCAL dw.principal = 'bob';
  INSERT INTO demo_item(owner_principal, body) VALUES ('bob','b-1');
COMMIT;
RESET ROLE;

-- 建面自检：三 role 均须存在、NOLOGIN、NOBYPASSRLS（期望 3 行且三列均为 f/f）
SELECT rolname, rolcanlogin, rolbypassrls
  FROM pg_roles WHERE rolname IN ('dw_owner','dw_migrate','dw_app') ORDER BY rolname;
