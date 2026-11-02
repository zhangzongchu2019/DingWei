# Tasks

变更：r15-session-history-persist；基线 `c248506cf120c3af2a06eeaeeae3038f596537b1`。本单只写 OpenSpec 提案，不写产品代码、不实现、不 apply、不操作现网／预发。

## 1. 提案

- [x] T-01 核实内存 ring、重启／断线清理、mux epoch/cursor 和现有持久表的职责，完成 proposal、design、context、spec。
- [x] T-02 按 rulings ㉚～㉜ 回写 PM-R15-01～08 定值，并同步容量、淘汰、隐私与验收规格。

## 2. 后续（另行授权）

- [ ] T-03 待业主／PM 另行授权后派实现；本单不授权实现、apply 或环境操作。
- [ ] T-04 由独立执行者验收重启、重连、淘汰、权限、撤权清理、游标连续性、容量／TTL、迁移和回退；未执行不计 PASS。

## 交接记录

- 2026-11-02T03:33:37Z · togpt60lunazzc.zzc · commit 42b9c9238cdec9ab69cf42b9347a5d82c1345f2b
  - 完成：在 c248506cf120c3af2a06eeaeeae3038f596537b1 核对事件 ring、关闭清理、SSE／Console mux 快照和 cursor 现状；根据只读事实报告写入 4 份 OpenSpec 文档，含 8 项 PM 决策和 11 个验收场景。
  - 阻塞：PM-R15-01～08 未定，推荐值仍为提案，不是已批准实现参数。
  - 下一步：strict 已通过；推送本分支并核对远端 tip/tree；等 PM 决策后另派回写／实现。

- 2026-11-02T04:30:43Z · togpt60lunazzc.zzc · commit 待提交
  - 完成：依 rulings ㉚～㉜ 将 PM-R15-01～08 标记已定；修订持久化容量与最旧淘汰／公平性建议，明确 eventRing 独立预算和不调整建议；更新规格淘汰验收并删除固定 1000 条场景。
  - 阻塞：无；T-03 实现仍未授权。
  - 下一步：strict 已通过；提交、推送并核对远端 tip/tree；等待后续授权。
