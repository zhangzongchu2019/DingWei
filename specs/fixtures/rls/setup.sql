-- 人层 RLS fixture · 建面（乙域 RV-乙12 要求的三条负测的公共前置）
-- 🔴 三个 role 分离，见 C.3a.1a；dw_app 必须 NOBYPASSRLS。
DROP TABLE IF EXISTS demo_item;
DROP ROLE IF EXISTS dw_app;
DROP ROLE IF EXISTS dw_owner;

CREATE ROLE dw_owner LOGIN PASSWORD 'owner_pw' NOBYPASSRLS;
CREATE ROLE dw_app   LOGIN PASSWORD 'app_pw'   NOBYPASSRLS;

SET ROLE dw_owner;
CREATE TABLE demo_item (
  id              bigserial PRIMARY KEY,
  owner_principal text NOT NULL,
  body            text NOT NULL
);
ALTER TABLE demo_item ENABLE ROW LEVEL SECURITY;
ALTER TABLE demo_item FORCE ROW LEVEL SECURITY;   -- 🔴 属主也不豁免（C.3a.1a #2）

-- 🔴 逐命令声明，且写命令必须带 WITH CHECK（C.3a.1a #4a/#4b）
--    只有 USING 时，dw_app 能把 owner_principal 写成他人 —— 这正是乙域 RV-乙12 第 ① 条。
CREATE POLICY p_sel ON demo_item FOR SELECT
  USING (owner_principal = current_setting('dw.principal', true));
CREATE POLICY p_ins ON demo_item FOR INSERT
  WITH CHECK (owner_principal = current_setting('dw.principal', true));
CREATE POLICY p_upd ON demo_item FOR UPDATE
  USING      (owner_principal = current_setting('dw.principal', true))
  WITH CHECK (owner_principal = current_setting('dw.principal', true));
CREATE POLICY p_del ON demo_item FOR DELETE
  USING (owner_principal = current_setting('dw.principal', true));

GRANT SELECT, INSERT, UPDATE, DELETE ON demo_item TO dw_app;
GRANT USAGE, SELECT ON SEQUENCE demo_item_id_seq TO dw_app;
RESET ROLE;

-- 预置两人各一行（以属主身份插入，故须临时给上下文）
SET ROLE dw_owner;
BEGIN; SET LOCAL dw.principal = 'alice';
  INSERT INTO demo_item(owner_principal, body) VALUES ('alice','a-1');
COMMIT;
BEGIN; SET LOCAL dw.principal = 'bob';
  INSERT INTO demo_item(owner_principal, body) VALUES ('bob','b-1');
COMMIT;
RESET ROLE;
