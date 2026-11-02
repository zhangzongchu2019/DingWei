# Tasks

变更：r15-session-history-persist；基线 `c248506cf120c3af2a06eeaeeae3038f596537b1`。本单只写 OpenSpec 提案，不写产品代码、不实现、不 apply、不操作现网／预发。

## 1. 提案

- [x] T-01 核实内存 ring、重启／断线清理、mux epoch/cursor 和现有持久表的职责，完成 proposal、design、context、spec。
- [ ] T-02 请 PM 定值 PM-R15-01～08；依据决议更新 OpenSpec 并冻结提案。

## 2. 后续（另行授权）

- [ ] T-03 PM 批准并冻结规格后另派实现；设计、存储迁移与协议兼容均按批准规格执行。
- [ ] T-04 由独立执行者验收重启、重连、淘汰、权限、撤权清理、游标连续性、容量／TTL、迁移和回退；未执行不计 PASS。

## 交接记录

- 2026-11-02T03:33:37Z · togpt60lunazzc.zzc · commit 待提交
  - 完成：在 c248506cf120c3af2a06eeaeeae3038f596537b1 核对事件 ring、关闭清理、SSE／Console mux 快照和 cursor 现状；根据只读事实报告写入 4 份 OpenSpec 文档，含 8 项 PM 决策和 11 个验收场景。
  - 阻塞：PM-R15-01～08 未定，推荐值仍为提案，不是已批准实现参数。
  - 下一步：全仓 OpenSpec strict 校验，提交并推送本分支，核对远端 tip/tree；等 PM 决策后另派回写／实现。
