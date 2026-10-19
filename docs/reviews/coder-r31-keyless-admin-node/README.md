# R31 REWORK 349281e1：主密钥缺席时管理员仍显示托管节点

基准7ffd62307caa97d441f083611ebde7d1322c04cd。旧路径从cfg.hosted/Orchestrator.HostedItem取节点名；wireHosted未配置主密钥会提前退出，因此旧页面在关闭托管时取不到已有席位节点名。此后继只修这个读取路径与测试、证据。

新增 hosted.Store.AgentNodeName：直接参数化 SELECT节点名，按席位agent_id联结g4_hosted_seat/g4_node。只读取节点名，不读取账号/凭据，不构造编排器，不做DDL、不启动托管任务。wireNewPlatform在PG池存在时独立接入API.AdminHostedNodeName，和创建装配/主密钥/访问allowlist无关。普通Agent列表的Hosted子对象回调及创建开关不改。

缺席位或节点记录返回空名；缺任一托管表按SQLSTATE42P01返回空名，管理员Agent整页继续200。其他查询错误继续上报，HTTP拒绝整页，不返回部分Agent数据。HTTP的active/admin人类cookie门、拒Authorization、上限/游标/读取审计及普通来源隐私不变。无DDL、无迁移账本变更。

## 新增生产接线证据

cmd/workpulse/r31_admin_agent_keyless_test.go使用真实迁移后的临时PG、PGStore、真实wireNewPlatform与N3 cookie。测试无主密钥、cfg无Hosted回调，也不注入AdminHostedNodeName；只通过生产接线读管理员响应。五场景：已落席位节点名正确；两托管表皆不存在；仅席位表不存在；仅节点表不存在；无关列错误不被缺表降级吞掉。前三类缺表结果都是200、Agent仍在、host为空；同时创建路由仍未注册，读取审计恰一条，无账号列泄露。

## 验证

- CHECKS.json记录18个Go指定撤修和恢复绿；原14格全部保留，新增四格：删生产独立接线、清空节点查询结果、删缺表兼容、吞掉无关列错误。指定标记为ADMIN_NODE_KEYLESS_WIRING_MISSING / ADMIN_NODE_TABLE_ABSENCE_BROKE_LIST / ADMIN_NODE_UNRELATED_ERROR_HIDDEN；编译失败/导入失败不算红。
- 同一脚本正向/恢复包含真实生产接线格，16个受测源/测试文件运行前后SHA一致；页面正向6/0、三个指定撤修红、恢复6/0。页面产品代码本次未改。
- full-go.log为identity、identity/httpapi、cmd/workpulse全包（严格临时PG与旧R30兼容二进制）。build.log/vet.log为全包构建及hosted/httpapi/cmd静态检查；boundary.log为仓库棘轮，无债务增加。
- 父7ffd的完整证据仍是原锚历史证据；本目录记录此增量，不将父manifest冒充当前源码manifest。MANIFEST.json覆盖本增量源/测试/设计与原始日志，提交后从Git blob核验。

全部限本机惰性夹具、本地HTTP及临时隔离PG，不访问43、不部署。以上为作者自测，独立结论归togpt60。
