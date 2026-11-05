# Tasks

- [x] T-01 复核并冻结基线架构事实：身份、全名解析、在线清单、Hub 路由、AMR、`session_delivery` 和 delivery_ack v2。
- [ ] T-02 由业主裁定令牌/OAuth 生命周期、队列 TTL、在线租约、编组命名与 mention 范围。
- [x] T-03 完成管理员预建 HTTP 席位及每席位 token 哈希/吊销设计；不得写明文凭据。
- [ ] T-04 实现 MCP `2026-07-28` OAuth metadata 与 Streamable HTTP endpoint，并验证四工具列表及标准客户端互操作。
- [ ] T-05 实现权限等价的 REST 在线清单、发消息、有限收件等待和 ACK 接口。
- [ ] T-06 将 HTTP 适配器接入 Hub 普通消息路由、编组授权、AMR 和 `session_delivery`；扩展 ACK v2 时保持 helper/driver 现有行为。
- [ ] T-07 实现在线租约、离线队列过期、席位/来源限流、审计脱敏、吊销中断与队列容量保护。
- [ ] T-08 完成安全/功能测试：授权、IDOR、token 哈希及吊销、Origin、MCP/REST 等价、轮询上限、ACK 幂等与 helper 回归。
- [ ] T-09 至少实施六项变异（移除编组判定、吊销校验、ACK 所有权/generation、等待上限/过期、Origin 校验、两接口权限等价），每项由原判据杀死。
- [ ] T-10 运行构建与严格 OpenSpec 校验，补充 MCP 互操作记录和部署/回滚方案，另行提交实现变更。

## 交接记录

- 2026-11-05，执行者 `togpt60lunazzc`，主要内容提交 `b19582cbc6be…`；验证/交接更新随最终 tip。
  - 完成：新增 OpenSpec 提案、基线上下文、10 条增量需求及分期任务；无产品代码修改。单变更 strict `validate agent-http-mcp-gateway --strict --no-interactive` rc=0；全仓 strict `validate --all --strict --no-interactive` rc=0，24/24、0 failed；`git diff --check` 通过。首轮校验因缺少 ADDED delta 标题失败，已修正后通过。MCP 互操作未实测，规格/Hub 当前支持状态未核实。
  - 阻塞：T-02 需要业主决定；MCP 互操作仍未实测。
  - 下一步：推送本提案分支并核对远端 tip，send.py 回执后冻结等待评审。
