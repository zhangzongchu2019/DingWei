-- 负测 ③（乙域 RV-乙12）：事务提交后同一物理连接不得残留 principal
-- 🔴 这是用 SET LOCAL 而非 SET 的唯一理由（C.3a.1a #5c）
SET ROLE dw_app;
BEGIN;
  SET LOCAL dw.principal = 'alice';
  SELECT count(*) AS alice_rows FROM demo_item;            -- 期望 1
COMMIT;
-- 同一连接，事务已结束
-- 🔴 实跑修正（PG17）：事务结束后 `current_setting(...,true)` 返回**空字符串**而非 NULL，
--   故 coalesce 取不到默认值 —— 期望值由 `<unset>` 改为**空**。
--   ⚠️ 判「无残留」应以下一行的 must_be_zero=0 为准；本行只作观察，不作判据。
SELECT coalesce(current_setting('dw.principal', true),'<null>') AS must_be_unset;  -- 期望：空字符串（PG17 实测）
BEGIN;
  SELECT count(*) AS must_be_zero FROM demo_item;          -- 期望 0（身份已失效）
ROLLBACK;
RESET ROLE;

-- 负测 ④（C.3a.1c）：确认 FORCE RLS 防的是【误操作】而非【有意绕过】——
-- 🔴 属主可自解除。本段**记录该事实**，不是要求它被拦住：
--   期望 ALTER 成功（属主本就有此权），故其**审计留痕**是唯一防线。
SET ROLE dw_owner;
BEGIN;
  SELECT count(*) AS owner_sees_before FROM demo_item;   -- 期望 0（FORCE 下属主也受 policy 约束）
  ALTER TABLE demo_item NO FORCE ROW LEVEL SECURITY;     -- 期望成功 ← 属主可自解除
  SELECT count(*) AS owner_sees_after  FROM demo_item;   -- 期望 2（约束已解除，看见全部）
ROLLBACK;                                                -- 回滚，不留副作用
RESET ROLE;
