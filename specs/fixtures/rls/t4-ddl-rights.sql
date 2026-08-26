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
-- 🔴 **本组于 2026-08-26 按乙域裁定重做 —— prodops 真机不符是【夹具错】，
--    不是实现错，也不是 PG17 行为差异。**
--   旧写法：`REVOKE … FROM CURRENT_USER;` 之后仍用 `SET ROLE dw_app` 验。
--   而代跑会话是 **superuser**，superuser 的 `SET ROLE` **不受成员资格约束** ——
--   于是 `permission denied` 永远不会出现，负测恒「不符」。
--   ⚠️ **用 superuser 会话假扮应用面，测的就不是应用面。**
--   ⇒ 必须 `SET SESSION AUTHORIZATION`，让 **session_user 真正变成 dw_app**。
--   🔴 **不得只改 EXPECTED** —— 那是把「测错了」写成「本该如此」。
REVOKE dw_owner, dw_migrate FROM CURRENT_USER;
SET SESSION AUTHORIZATION dw_app;
SELECT session_user, current_user;   -- 期望：两者均为 dw_app

-- 🔴 夹具自检：前置不成立时**本组结论无效**，须当场报错，不得让后面的断言照跑。
--   （同甲域 RV-甲47 那次：夹具没造出来，后面测的就不是它该测的东西。）
DO $$
BEGIN
  IF session_user <> 'dw_app' THEN
    RAISE EXCEPTION 'FIXTURE ERROR: session_user=% —— 未真正切到应用面，本组结果无效（若 NOLOGIN 角色不能作 session authorization，见文末回退方案）', session_user;
  END IF;
  IF pg_has_role(session_user, 'dw_owner', 'USAGE')
     OR pg_has_role(session_user, 'dw_migrate', 'USAGE') THEN
    RAISE EXCEPTION 'FIXTURE ERROR: % 仍持有 dw_owner/dw_migrate 成员资格 —— REVOKE 未生效，本组结果无效', session_user;
  END IF;
END $$;

BEGIN;
  SET ROLE dw_owner;      -- 期望 ERROR：permission denied to set role "dw_owner"
ROLLBACK;
BEGIN;
  SET ROLE dw_migrate;    -- 期望 ERROR：permission denied to set role "dw_migrate"
ROLLBACK;

RESET SESSION AUTHORIZATION;
GRANT dw_owner, dw_migrate TO CURRENT_USER;   -- 还原夹具便利，供后续脚本使用

-- 🔴 回退方案（乙域指定）：若本机 PG 不允许对 **NOLOGIN** 角色做 session authorization，
--   则**显式建立一个无任何成员资格的临时应用 login 角色**，并经该会话执行本组：
--     CREATE ROLE dw_app_surrogate LOGIN NOBYPASSRLS;   -- 不授予 dw_owner / dw_migrate
--     \c - dw_app_surrogate
--     （重跑上面两条 SET ROLE，期望同样 permission denied）
--     DROP ROLE dw_app_surrogate;   -- 验完即删
--   ⚠️ **不得退回用 superuser 会话假扮应用面** —— 那正是本组这次被判夹具错的原因。
