# Tasks

- [x] T-01 读取4a基点OAuth/CIMD/issuer/PG与AUTH-FIRST，核官方RFC/MCP资料，形成设计页；只docs/openspec。
- [x] T-02 对公共元数据、redirect三类、持久额度、可信代理、并存/回退与GET排障写入DCR-001～008；CodeBuddy原报告只记SHA与纠正结论，不复制私有原件。
- [x] T-03 组长过目设计并确认A-DCR-01～03与迁移编号；此项完成前不写产品代码。
- [ ] T-04 实现独立PGclient/限流/有界GC与装配，迁移号获确认后核无撞号。
- [ ] T-05 实现登记/发现/分型、精确回调与5分钟snapshot收尾，保持CIMD/内置/业务权限。
- [ ] T-06 真实HTTP注册至initialize全链、无预建Agent、三回调类型和错误/容量/重启/代理边界。
- [ ] T-07 PKCE、非回环、secret注册、限流四项必红及矩阵各保护的业务断言变异；不以编译错/panic计刀。
- [ ] T-08 实现冻结源canonical＋mcpgw/extseat/m8三遍PG strict，SKIP逐名，相关cmd/store/focused race与strict。
- [ ] T-09 qwen独立复核、组长终审；无发布令不推main、不部署、不出发布件。

## 交接记录

- 2026-11-19；coder56solzzc；基点4a349ee59a1d8d7b8b9c562c462bded2f8b0f192。完成：仅设计审阅稿与规范；阻塞：设计待组长过目、实际CodeBuddy redirect形式与PG迁移号待确认。下一步：先校验并经Git交设计页，不先实施；实现验收格均未勾选，不把基线门当实现完成。

- 2026-11-19；coder56solzzc；设计源64f08d658f99aeec9092bc61003769ef4d597ccd。完成：设计严格校验48/0、基线canonical rc0，两profile各1290，原日志与SHA见docs/reviews/mcp-dcr-design/REPORT.md；产品零改。阻塞：T-03设计确认、实际回调和迁移号待定。下一步：经Git交组长审阅，不先写产品；本轮基线门不等于DCR功能完成。

- 2026-11-19；coder56solzzc；按08:37:52Z组长有条件通过修订四项及公开资料，PG暂0115。完成设计更新；下一步立即实现。实际IDE回调仍缺、无部署授权；不实现Agent去重。

- 2026-11-19；coder56solzzc；候选4f4289cf首轮canonical双profile通过，末尾PG认领五子格因固定旧迁移清单未含0115而红，保留原日志；已明确补0115名单/3表/计数，不弱化断言，新增清理快照补格及精确HTTPS JSON配置。开发补验rc0，下一步固定修订源统一重跑正式门及29刀。无main/部署。
