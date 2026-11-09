## Tasks

- [x] T-01 在 PG 投影事务中取得来源授权人显示名/短名与目标短名快照。
- [x] T-02 生成固定署名抬头及 Hub 专属来源 meta，并覆盖幂等与伪造场景。
- [x] T-03 通过真实 PG、Hub 路由、extseat Wait 验证回信闭环。
- [x] T-04 为五项语义破坏编写带独立标记的变异验证脚本。
- [x] T-05 运行指定 Go、helper、OpenSpec 门并记录读数。
- [x] T-06 提交单一变更并推送后核对远端分支坐标。
- [x] T-07 为 `send_message` 增加可选 `expect_reply`，同步 MCP schema、REST 分发和接口文档。
- [x] T-08 将回信选择纳入 request_hash 并持久化 PG outbox，true 投影才写 reply policy。
- [x] T-09 以生产 completed-turn 路径覆盖 true 回程、false 缺省与 Wait 来源，并增加删策略写入的第八刀。
- [x] T-10 更新原迁移账本断言及 S-EXTSEAT-SENDER 预审更正记录。
- [x] T-11 将迁移重新编号为 PG 0110，同步账本断言、记录并重跑全部交付门和八刀变异。

## 交接记录

- 日期：2026-11-09；执行者：togpt60lunazzc；commit：待提交。
  - 完成：`mcpgw`（Node 22 浏览器）、`extseat`、`extshared` 全包通过；`m8 -p 1` 全包 PASS（230.810s）；锁定 venv helper 构建门 `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`；OpenSpec strict 31/31；五刀 5/5 命中各自语义标记。
  - 阻塞：无。
  - 下一步：单一提交、推送新分支并核对 `ls-remote`。
- 日期：2026-11-09；执行者：togpt60lunazzc；commit：`71ba12c82825d620641b7af7b9db875bd08e0e8e`。
  - 完成：预审补项 T-07～T-10 已在该提交实现；`mcpgw`、`extseat`、`tempgroup` 全包通过，严格 PG+Hub completed-turn E2E 通过，迁移包通过，变异 8/8 语义判红。锁定运行时的标准构建门和远端提交核对待完成。
  - 阻塞：标准总门正在运行；尚未推送。
  - 下一步：门禁 rc=0 后追加验证读数，推送分支并核对 ls-remote。
- 日期：2026-11-09；执行者：togpt60lunazzc；交付提交：`19c43b10857b8d0210e834c7f50b08f45f346d4b`。
  - 完成：锁定 venv 标准构建门 `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`；OpenSpec strict 31/31；八刀语义判红；提交已推送，`git ls-remote` 核实远端为 `19c43b10857b8d0210e834c7f50b08f45f346d4b`（tree `3d9056896c226618f4c4ac99d653351ff81631c7`）。
  - 阻塞：无。
  - 下一步：由独立复核席复核。
- 日期：2026-11-09；执行者：togpt60lunazzc；commit：待提交。
  - 完成：PG 0110 改号并同步账本；store/...、extseat/mcpgw/tempgroup、严格 PG m8 全包、workpulse 编译、OpenSpec strict 31/31、八刀语义变异 8/8、`scripts/run-build-tests.sh` 均通过（`BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`）。
  - 阻塞：无。
  - 下一步：提交并推送，核对远端坐标。
