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
