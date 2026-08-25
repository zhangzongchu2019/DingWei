-- 人层 RLS fixture · 建面（乙域 RV-乙12 / RV-乙13）
--
-- 🔴 **执行前置，缺一不得跑**：
--   1. **只在隔离的测试库里跑**（如 `createdb dw_rls_fixture`），**不得对任何真实环境执行** ——
--      本脚本会 DROP ROLE / DROP TABLE。
--   2. 执行账号须有 **CREATEROLE** 权限（建三个 role），且须能 `SET ROLE` 到它们
--      （下方 GRANT ... TO CURRENT_USER 即为此）。
--   3. 🔴 **三个 role 一律 NOLOGIN、无口令** —— fixture 不得在仓内留任何凭据；
--      它们只用于 `SET ROLE` 切换权限面，不用于真实连接（乙域 RV-乙13）。
--
-- ⚠️ 首版只建了 dw_owner / dw_app 却在头注自称「三 role 分离」，且用可登录 role ＋明文口令
--   写了明文口令 —— **件的自述与件的内容不符**，由乙域 RV-乙13 抓出。

DROP TABLE IF EXISTS demo_item;
DROP ROLE IF EXISTS dw_app;
DROP ROLE IF EXISTS dw_migrate;
DROP ROLE IF EXISTS dw_owner;

-- 三具名 role，职责分离（C.3a.1a / C.3a.1c）：
--   dw_owner   建面（**由企业 ops 持有**：库内建表/改结构/建 policy）
--   dw_migrate 迁移期 DDL
--   dw_app     只跑领域 SQL（NOBYPASSRLS · 非属主 · 无 DDL）
-- ⚠️ 库本身由**平台 ops** 建（本 fixture 假定库已存在）；
--   🔴 属主可自解除 FORCE RLS（NO FORCE / DROP POLICY / OWNER TO）⇒
--     该风险**收敛到企业 ops 单点并以审计承载**，不是被消除（C.3a.1c）。
CREATE ROLE dw_owner   NOLOGIN NOBYPASSRLS;
CREATE ROLE dw_migrate NOLOGIN NOBYPASSRLS;
CREATE ROLE dw_app     NOLOGIN NOBYPASSRLS;
GRANT dw_owner, dw_migrate, dw_app TO CURRENT_USER;   -- 仅为本 fixture 能 SET ROLE

SET ROLE dw_owner;
CREATE TABLE demo_item (
  id              bigserial PRIMARY KEY,
  owner_principal text NOT NULL,
  body            text NOT NULL
);
ALTER TABLE demo_item ENABLE ROW LEVEL SECURITY;
ALTER TABLE demo_item FORCE ROW LEVEL SECURITY;   -- 🔴 属主也不豁免（C.3a.1a #2）

-- 🔴 逐命令声明；写命令必须带 WITH CHECK（C.3a.1a #4a/#4b）
--    只有 USING 时，dw_app 能把 owner_principal 写成他人 —— 正是乙域 RV-乙12 第 ① 条。
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
GRANT SELECT, INSERT, UPDATE, DELETE ON demo_item TO dw_app;
GRANT USAGE, SELECT ON SEQUENCE demo_item_id_seq TO dw_app;
GRANT ALL ON demo_item TO dw_migrate;
RESET ROLE;

-- 预置两人各一行（属主身份插入，故须临时给上下文）
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
