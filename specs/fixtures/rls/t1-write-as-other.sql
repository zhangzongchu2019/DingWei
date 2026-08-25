-- 负测 ①（乙域 RV-乙12）：dw_app 不得把 owner_principal 写成他人
-- 期望：两条 DML 均被拒（new row violates row-level security policy）
SET ROLE dw_app;
BEGIN;
  SET LOCAL dw.principal = 'alice';
  -- 期望 ERROR：以 bob 的名义插入
  INSERT INTO demo_item(owner_principal, body) VALUES ('bob','forged');
ROLLBACK;
BEGIN;
  SET LOCAL dw.principal = 'alice';
  -- 期望 ERROR 或 0 行：把自己的行改挂到 bob 名下
  UPDATE demo_item SET owner_principal='bob' WHERE body='a-1';
ROLLBACK;
RESET ROLE;
