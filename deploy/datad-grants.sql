-- datad A-lite 纵深授权(契约 61e5938 §5① N-A3):assign 是 assignment 表唯一写路径的 DB 级收成。
-- 执行者:prodops 以超级用户(或表 owner)执行,不由 datad_user 自跑的 SchemaDDL 承载
--   (datad_user 无 GRANT/REVOKE 其它角色的权)。datad_user=datad 运行连接账号。
-- 目的:防 datad 之外的平台组件(View/board/其它连接)直写 assignment,破坏 §5①
--   "=1"(当代双角色齐+代一致)不变量。partial-unique(assignment_active_uniq)只保 ≤1;
--   =1 靠"唯一写路径";此授权把"唯一写路径"从程序约定升为 DB 强制(外部角色维度)。
-- 分层如实(§5① v1.2/A-MC5,不夸大单层):
--   · DB 层(本文件):保证"datad_user 之外的角色无写"+"首果账不可变"。
--   · 代码层(assignment_grant_test.go TestAssignSoleWritePath):保证 datad_user 进程内
--     仅 Assign() 写 assignment,防新端点用同一 datad_user 角色绕过(GRANT 不辨代码路径)。
--   两层合力=唯一写路径。

-- 1) 收回 PUBLIC 一切权限(新表默认对 PUBLIC 无 DML,显式收回防继承/防误授)。
REVOKE ALL ON assignment FROM PUBLIC;
REVOKE ALL ON assign_first_result FROM PUBLIC;

-- 2) datad_user=唯一写者:
--    assignment 需 SELECT/INSERT/UPDATE(翻代 active=false + 新 gen 双角色 INSERT);
--    无 DELETE——历史代永留(§1 PRIMARY KEY 含 generation,重派保留旧代)。
GRANT SELECT, INSERT, UPDATE ON assignment TO datad_user;
--    assign_first_result 首果账 append-only:SELECT/INSERT;无 UPDATE/DELETE——
--    首果不可变=幂等真相源(§3 重放读首果,绝不重算/改写)。
GRANT SELECT, INSERT ON assign_first_result TO datad_user;

-- 3) audit 纵深(原 §4,一并落):datad_user 对 audit 仅 INSERT/SELECT。
REVOKE UPDATE, DELETE ON audit FROM datad_user;

-- 4) 只读消费者(若部署了独立只读角色,如 View/board 只读面):仅 SELECT。
--    未部署独立只读角色则本段忽略——datad 自身只读端点走 datad_user 的 SELECT。
-- GRANT SELECT ON assignment, assign_first_result TO datad_readonly;
