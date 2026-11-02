# Tasks

变更：r15-session-history-persist；基线 `c248506cf120c3af2a06eeaeeae3038f596537b1`。本单只写 OpenSpec 提案，不写产品代码、不实现、不 apply、不操作现网／预发。

## 1. 提案

- [x] T-01 核实内存 ring、重启／断线清理、mux epoch/cursor 和现有持久表的职责，完成 proposal、design、context、spec。
- [x] T-02 按 rulings ㉚～㉜ 回写 PM-R15-01～08 定值，并同步容量、淘汰、隐私与验收规格。

## 2. 后续（另行授权）

- [ ] T-03 待业主／PM 另行授权后派实现；本单不授权实现、apply 或环境操作。
- [ ] T-04 由独立执行者验收重启、重连、淘汰、权限、撤权清理、游标连续性、容量／TTL、分页至最早记录、滚动位置、到底／gap 提示、实时切点、迁移和回退；未执行不计 PASS。
- [x] T-05 按 rulings ㉞ 定值 PM-R15-09：每页最多 100 个事件；4 MiB 与服务端 2 秒时限沿用原建议。

## 交接记录

- 2026-11-02T03:33:37Z · togpt60lunazzc.zzc · commit 42b9c9238cdec9ab69cf42b9347a5d82c1345f2b
  - 完成：在 c248506cf120c3af2a06eeaeeae3038f596537b1 核对事件 ring、关闭清理、SSE／Console mux 快照和 cursor 现状；根据只读事实报告写入 4 份 OpenSpec 文档，含 8 项 PM 决策和 11 个验收场景。
  - 阻塞：PM-R15-01～08 未定，推荐值仍为提案，不是已批准实现参数。
  - 下一步：strict 已通过；推送本分支并核对远端 tip/tree；等 PM 决策后另派回写／实现。

- 2026-11-02T04:30:43Z · togpt60lunazzc.zzc · commit 03e3cf3be242bdfcb95af7a597cffe1385e2b6cd
  - 完成：依 rulings ㉚～㉜ 将 PM-R15-01～08 标记已定；修订持久化容量与最旧淘汰／公平性建议，明确 eventRing 独立预算和不调整建议；更新规格淘汰验收并删除固定 1000 条场景。
  - 阻塞：无；T-03 实现仍未授权。
  - 下一步：strict 已通过；提交、推送并核对远端 tip/tree；等待后续授权。

- 2026-11-02T04:33:30Z · togpt60lunazzc.zzc · commit 8746611baf410d242db9ca84bcc99e356ed62f4d
  - 完成：按 rulings ㉝ 补充 Console 向上分页、游标、页面／请求预算建议、锚点保持、到底／gap 提示与实时／重启衔接；勘察 renderer 确认现有滚动容器只有贴底跟随，向上加载属新增前端工作。新增 R15-S012～S016；R15-S006 已覆盖他人不可见。
  - 阻塞：PM-R15-09 页大小及单次请求预算待业主确认；T-03 实现未授权。
  - 下一步：strict 已通过；推送本分支并核对远端 tip/tree。

- 2026-11-02T05:18:57Z · togpt60lunazzc.zzc · commit 待提交
  - 完成：依 rulings ㉞ 将 PM-R15-09 定为每页最多 100 个结构化事件，保留 4 MiB／2 秒时限；更新 design、spec 验收与历史建议行，T-05 闭合。
  - 阻塞：无；T-03 实现仍未授权。
  - 下一步：strict 已通过；提交并推送本分支，核对远端 tip/tree。
