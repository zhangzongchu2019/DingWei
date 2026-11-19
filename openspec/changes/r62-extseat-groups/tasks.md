## 实施任务

- [x] T-01 导入批准设计，冻结 A-AMEND-02 与统一路由技术设计。
- [x] T-02 0113 迁移、共享授权与路由绑定。
- [x] T-03 无 AG 验证入口、稳定地址与 Console 成员管理。
- [x] T-04 所有通信入口、外部↔外部与逐消息重核。
- [x] T-05 常驻回归、五项变异与完整门已执行；m8 rc1仅已知3505中断，依组长口径保留两锚对照，见docs/reviews/r62-extseat-groups/REPORT.md。
- [x] T-06 交件前已复核main206d41b2/显示名d7910d36占0112，本片0113；本地交件备齐，普通推送核远端和独立复核请求随交件执行，未获独立验收。

## 交接记录

2026-11-19 coder56solzzc：完成批准设计导入及技术设计；产品尚未实现。无运行环境授权阻塞。下一步共享授权、迁移；R6 新源到达立即优先复验。证据坐标在后续提交记录与 REPORT 更新。

2026-11-19 coder56solzzc：完成产品/测试候选 `aaed8aaee6630e2683096c977a058da653fee7e2`（可git cat-file）及最终门；入口 `internal/identity/communication.go`、`internal/extseat/group_routing_pg_test.go`、迁移0113与本REPORT。canonical/真实PG三包/迁移三遍/race/浏览器46套/strict43全绿；m8三遍rc1只含已知3505中断，main与候选各200对照保留，不冒称绿。阻塞：待独立复核，非发布GO。下一步：推送核坐标请求qwen和组长，R6优先；无main/部署动作。
