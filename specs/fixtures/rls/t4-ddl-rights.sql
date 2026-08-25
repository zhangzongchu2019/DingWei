-- 负测 ⑤/正测（乙域 RV-乙15）：DDL 权的归属须实证，不能靠 GRANT ALL 的字面
-- 🔴 PostgreSQL 里 `GRANT ALL ON <表>` **只给表级 DML**；
--   `ALTER TABLE` / 建 policy **要求属主身份**（或属主 role 的成员）。

-- 正测：dw_migrate 经 dw_owner 成员资格取得 DDL 权（期望**成功**）
SET ROLE dw_migrate;
BEGIN;
  SET ROLE dw_owner;                                   -- 成员资格 ⇒ 可切到属主
  ALTER TABLE demo_item ADD COLUMN IF NOT EXISTS probe_col text;   -- 期望成功
  ALTER TABLE demo_item DROP COLUMN IF EXISTS probe_col;
ROLLBACK;
RESET ROLE;

-- 负测：dw_app 无 DDL 权（期望**两条均报 must be owner of table demo_item**）
SET ROLE dw_app;
BEGIN;
  ALTER TABLE demo_item ADD COLUMN IF NOT EXISTS bad_col text;     -- 期望 ERROR
ROLLBACK;
BEGIN;
  CREATE POLICY p_bad ON demo_item FOR SELECT USING (true);        -- 期望 ERROR
ROLLBACK;
RESET ROLE;

-- 负测：dw_migrate **不切属主时**也无 DDL 权（证明 DDL 来自成员资格，不是 GRANT ALL）
SET ROLE dw_migrate;
BEGIN;
  ALTER TABLE demo_item ADD COLUMN IF NOT EXISTS bad_col2 text;    -- 期望 ERROR
ROLLBACK;
RESET ROLE;

-- 负测 ⑥（乙域 RV-乙16）：应用面不得取得属主/迁移 role
-- 🔴 生产中 dw_app 的持有者**不得是** dw_owner / dw_migrate 的成员。
--   本 fixture 为使 SET ROLE 可用把三 role 都授给了 CURRENT_USER（夹具便利），
--   故此处**先撤销**再验，验完还原 —— 不撤销就测不出真实边界。
REVOKE dw_owner, dw_migrate FROM CURRENT_USER;
SET ROLE dw_app;
BEGIN;
  SET ROLE dw_owner;      -- 期望 ERROR：permission denied to set role "dw_owner"
ROLLBACK;
BEGIN;
  SET ROLE dw_migrate;    -- 期望 ERROR：permission denied to set role "dw_migrate"
ROLLBACK;
RESET ROLE;
GRANT dw_owner, dw_migrate TO CURRENT_USER;   -- 还原夹具便利，供后续脚本使用
