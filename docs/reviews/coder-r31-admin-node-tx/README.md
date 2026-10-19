# R31 REWORK 4468405f：节点名进入授权与审计事务

基准02f1e1be070b9d32ba1efd12c4522e11878b5539。该父提交修复了缺主密钥时节点名为空，但节点名在ListAdminAgents提交后由HTTP逐行查询，越过授权锁与审计事务。本增量将私有节点名投影移回PGStore.ListAdminAgents内部。

## 最终行为

同一事务顺序：锁定并重核用户active/platform_admin（FOR SHARE）；to_regclass确认两张可选托管表；有表则有界列表SQL LEFT JOIN席位与节点名，无表则同一列表SQL投影空串；读取完整页面与节点名；写一条读取审计；提交。HTTP只序列化已取得的私有投影字段，节点名字段JSON默认忽略，不再作事务外查询。

缺任一托管表、没有席位/节点记录时host为空，列表正常；查询/扫描/投影/审计失败均整事务回滚、返回空页错误。无主密钥、无创建编排器仍能读节点名。移除已不用的只读回调及独立逐行查询方法，普通Hosted子对象、创建路由与所有既有权限/IP围栏保持。无DDL/迁移账本更改。

## 真实PG证据

- 并发列表先行：另一事务锁住g4_node，列表在取节点投影处阻塞。以pg_locks找到投影backend，确认还没有读取审计；启动真实SetUserType撤权，并用pg_blocking_pids核实其被同一个投影backend持有的授权锁阻塞。释放节点锁后，完整节点名页面与一条审计先提交，撤权才完成；之后读取拒绝且无新增审计。
- 并发撤权先行：撤权事务先UPDATE用户并持锁；列表必须被其阻塞。撤权提交后列表拒绝、空页、零读取审计。
- 两种次序均靠实际数据库锁读数同步，不用生产测试钩子或固定睡眠推定交错。短轮询仅等待已声明的数据库条件，并设置截止。
- 投影失败：隔离PG中把节点名列改名，必须返回错误与空页且审计零条。真实生产wireNewPlatform＋PGStore＋N3 cookie的既有五场景继续通过，新增验证HTTP投影错误时审计亦为零。

## 检查与指定撤修

CHECKS.json记录新的冻结源码正/反对照：原权限/隐私/审计撤修保留；节点名响应/无主密钥/缺表格跟随新接线更新；新增将投影查询换为池查询（跨backend）、去FOR SHARE、吞投影错误等指定撤修。跨事务查询或提前失去授权锁必须在ADMIN_NODE_PROJECTION_TX_SPLIT红，投影失败被忽略必须在ADMIN_NODE_PROJECTION_FAILURE_NOT_ATOMIC红，不能用编译/导入错误冒充。

full-go.log为严格本地embedded PG的identity/httpapi/cmd完整回归（包含旧R30二进制兼容格）；build/vet/boundary日志与VALIDATION.json记录其他门。MANIFEST.json覆盖增量源/测试/设计与证据，从提交Git blob核验。父锚的历史证据保留原锚含义；不能当新锚验收。

所有检查仅本机临时隔离PG、HTTP夹具与浏览器，不访问43，不部署。作者自测，等待togpt60独立差集结论。
