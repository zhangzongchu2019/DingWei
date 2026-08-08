-- datad A-lite 纵深授权 v2(契约 61e5938/caa5c3a §5① + N-A3 addendum cd06d98):
-- 三角色分权,assign 是 assignment 表唯一写路径的 DB 级强制。
-- 执行者:prodops 以超级用户(或表 owner)执行。幂等:可重复执行。
--
-- 角色模型(addendum ①②③/角色最小权限与 GC):
--   · datad_user        = 常规应用池登录角色(现网运行账号)。对 assignment/assign_first_result
--                         **零写权**(仅 SELECT)——任何常规 API/新增端点直写=permission denied。
--   · datad_assign      = assign 专用登录角色。仅 AssignGeneration 单事务所需最小权限;
--                         datad 进程以独立连接池(DATAD_PG_ASSIGN_DSN)使用,不与常规池共享凭据。
--                         独立池=池内连接固定本角色,无 SET ROLE,事务后无 role 泄漏面。
--   · datad_assign_gc   = 首果 30d GC 专用登录角色(DATAD_PG_GC_DSN)。仅能删 assign_first_result
--                         (且程序侧限定 task 已 closed+超保留期),对 assignment 零写权。
-- 密码由 prodops 部署时 ALTER ROLE ... PASSWORD 设置,不入仓。

-- 0) 角色存在性(幂等)。
DO $$ BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname='datad_assign') THEN
    CREATE ROLE datad_assign LOGIN;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname='datad_assign_gc') THEN
    CREATE ROLE datad_assign_gc LOGIN;
  END IF;
END $$;

-- 1) 收回 PUBLIC 与常规角色的一切 assignment/首果账写权。
REVOKE ALL ON assignment FROM PUBLIC;
REVOKE ALL ON assign_first_result FROM PUBLIC;
REVOKE INSERT, UPDATE, DELETE, TRUNCATE ON assignment FROM datad_user;
REVOKE INSERT, UPDATE, DELETE, TRUNCATE ON assign_first_result FROM datad_user;
-- 常规池保留只读(PutTask owner_conflict 校验读 assignment;运维可读账)。
GRANT SELECT ON assignment TO datad_user;
GRANT SELECT ON assign_first_result TO datad_user;

-- 2) datad_assign 最小权限=AssignGeneration 单事务实际所需,逐句对应 assignment_pg.go:
--    · assignment:SELECT(现当代/max gen 查)+INSERT(新代双角色)+UPDATE(旧代 active=false);
--      **无 DELETE**(历史代永留,§1)。
--    · assign_first_result:SELECT(幂等首查)+INSERT(首果落账);**无 UPDATE/DELETE**
--      (首果不可变=幂等真相源;GC 删除权在 datad_assign_gc,assign 角色不得删)。
--    · task:SELECT(状态门)+UPDATE(owner_session/status/updated_at 同事务);
--    · audit:INSERT(R2-4 事务内审计)+序列 USAGE(audit.id BIGSERIAL)。
--    不授其它表——本角色不得被其它 handler 复用。
GRANT SELECT, INSERT, UPDATE ON assignment TO datad_assign;
GRANT SELECT, INSERT ON assign_first_result TO datad_assign;
GRANT SELECT, UPDATE ON task TO datad_assign;
GRANT INSERT ON audit TO datad_assign;
GRANT USAGE ON SEQUENCE audit_id_seq TO datad_assign;

-- 3) datad_assign_gc:仅首果删除面(程序侧再限 closed+超期;越界删除有负例测试)。
--    task 只读(closed 判定);assignment 零权限。
GRANT SELECT, DELETE ON assign_first_result TO datad_assign_gc;
GRANT SELECT ON task TO datad_assign_gc;
REVOKE ALL ON assignment FROM datad_assign_gc;

-- 4) audit 纵深(原 §4):datad_user 对 audit 仅 INSERT/SELECT;两新角色无 UPDATE/DELETE
--    (未授即无,显式收回防误授)。
REVOKE UPDATE, DELETE ON audit FROM datad_user;
REVOKE UPDATE, DELETE ON audit FROM datad_assign;
REVOKE ALL ON audit FROM datad_assign_gc;

-- 5) 验收(addendum 部署生效门,证据须来自目标 PG,grants.sql 入仓不算生效):
--    \dp assignment / \dp assign_first_result / \dp task / \dp audit 封存 ACL;
--    datad_user 与任一非 assign 角色直 INSERT/UPDATE assignment|assign_first_result=permission denied;
--    datad_assign 经正式事务路径成功;datad_assign_gc 仅能删首果(不能写 assignment/改首果);
--    连接归池复用后 current_user 各池恒为本池角色(独立池无 SET ROLE=无泄漏)。
