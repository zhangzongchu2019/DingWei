-- 负测 ③（乙域 RV-乙12）：事务提交后同一物理连接不得残留 principal
-- 🔴 这是用 SET LOCAL 而非 SET 的唯一理由（C.3a.1a #5c）
SET ROLE dw_app;
BEGIN;
  SET LOCAL dw.principal = 'alice';
  SELECT count(*) AS alice_rows FROM demo_item;            -- 期望 1
COMMIT;
-- 同一连接，事务已结束
SELECT coalesce(current_setting('dw.principal', true),'<unset>') AS must_be_unset;  -- 期望 <unset>
BEGIN;
  SELECT count(*) AS must_be_zero FROM demo_item;          -- 期望 0（身份已失效）
ROLLBACK;
RESET ROLE;
