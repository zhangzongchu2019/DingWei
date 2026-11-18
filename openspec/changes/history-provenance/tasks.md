# 任务

- [x] T-01 设计有界 provenance sidecar schema、唯一关联键与 migration 编号顺延规则。
- [x] T-02 定义剥除客户端 provenance、Append 同事务关联与乱序 fail-closed 行为。
- [x] T-03 定义原 history 分页 envelope 透传及旧 renderer 兼容边界。
- [x] T-04 设计 Agent／timer 刷新回放、DOM/DB/API 读数与字段/trust/关联删保护。
- [x] T-05 定义 sidecar 的 retention、共享容量 accounting、quota eviction 与 deletion queue 清理。
- [x] T-06 明确增强后超 1 MB 的事务回滚、单 gap、sidecar 保留及无重复事件回归判据。

## S-R61-N3-IMPL（2026-11-18）

冻结产品/测试源 `a32c877cf6ef21a909d2806f4b08e3e925d05d98`；下列 `commit:path` 均可 `git cat-file -p` 直接核验。

- [x] T-07 原样导入批准设计：`c3b3db27c9532ab19a80d769da55b98f02e08696:openspec/changes/history-provenance/design.md`；该首提交仅7个设计文件，与批准6a60a1a6指定目录相同。
- [x] T-08 五元组、primary/trust/ID/lifecycle精确匹配与Append同事务消费：`a32c877cf6ef21a909d2806f4b08e3e925d05d98:internal/store/session_history_provenance.go`。
- [x] T-09 独立history迁移0078、digest/非连续编号与计费/有界清理：`a32c877cf6ef21a909d2806f4b08e3e925d05d98:internal/store/history_migrations/0078_history_provenance.sql` 及同源 `internal/store/session_history_provenance_test.go`。
- [x] T-10 超限/SQL错误回滚消费、保留至TTL、既有oversize/gap与无重复事件：同源 `internal/store/session_history_provenance_test.go` 的 `TestHistoryProvenanceAppendRollbackAndOversize`。
- [x] T-11 真实WS入站、DB/HTTP/SSE、历史刷新DOM正负格及同名替换：同源 `internal/m8/history_provenance_test.go` 与 `view-service/history-provenance-browser-regression.js`。
- [ ] T-12 SOURCE_COMMIT构建门、store/m8三遍、PG strict、focused race、浏览器全量、strict与逐名SKIP；完成后补不可变证据坐标。
- [ ] T-13 删保护40刀及还原基线原始证据；完成证据提交后补坐标。
- [ ] T-14 togpt60独立复核与组长集成裁定；作者交件不替代此项。

## 交接记录

- 2026-11-16；执行者：togpt60lunazzc；commit：86c86025。完成：只读调研和提案；下一步：补充实现设计，不改产品码。
- 2026-11-17；执行者：togpt60lunazzc；commit：待提交。完成：补齐 sidecar/事务/容量/分页/renderer/回归实现设计；阻塞：实现等待 R60 定锚。
