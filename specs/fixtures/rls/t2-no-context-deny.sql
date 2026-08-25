-- 负测 ②（乙域 RV-乙12）：缺 dw.principal 时读写默认拒
-- 🔴 期望 SELECT 返回 **0 行**，不是全部 —— fail-closed，见 C.3a.1b
SET ROLE dw_app;
BEGIN;
  SELECT count(*) AS must_be_zero FROM demo_item;          -- 期望 0
  INSERT INTO demo_item(owner_principal, body) VALUES ('alice','no-ctx');  -- 期望 ERROR
ROLLBACK;
RESET ROLE;
