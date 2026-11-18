# 任务

- [x] T-01 设计有界 provenance sidecar schema、唯一关联键与 migration 编号顺延规则。
- [x] T-02 定义剥除客户端 provenance、Append 同事务关联与乱序 fail-closed 行为。
- [x] T-03 定义原 history 分页 envelope 透传及旧 renderer 兼容边界。
- [x] T-04 设计 Agent／timer 刷新回放、DOM/DB/API 读数与字段/trust/关联删保护。
- [x] T-05 定义 sidecar 的 retention、共享容量 accounting、quota eviction 与 deletion queue 清理。
- [ ] 实现只在 R60 定锚后另行派单；本 change 目前只有设计。

## 交接记录

- 2026-11-16；执行者：togpt60lunazzc；commit：86c86025。完成：只读调研和提案；下一步：补充实现设计，不改产品码。
- 2026-11-17；执行者：togpt60lunazzc；commit：待提交。完成：补齐 sidecar/事务/容量/分页/renderer/回归实现设计；阻塞：实现等待 R60 定锚。
