## Tasks

- [x] T-01 在 PG 投影事务中取得来源授权人显示名/短名与目标短名快照。
- [x] T-02 生成固定署名抬头及 Hub 专属来源 meta，并覆盖幂等与伪造场景。
- [x] T-03 通过真实 PG、Hub 路由、extseat Wait 验证回信闭环。
- [x] T-04 为五项语义破坏编写带独立标记的变异验证脚本。
- [x] T-05 运行指定 Go、helper、OpenSpec 门并记录读数。
- [ ] T-06 提交单一变更并推送后核对远端分支坐标。

## 交接记录

- 日期：2026-11-09；执行者：togpt60lunazzc；commit：待提交。
  - 完成：`mcpgw`（Node 22 浏览器）、`extseat`、`extshared` 全包通过；`m8 -p 1` 全包 PASS（230.810s）；锁定 venv helper 构建门 `BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`；OpenSpec strict 31/31；五刀 5/5 命中各自语义标记。
  - 阻塞：无。
  - 下一步：单一提交、推送新分支并核对 `ls-remote`。
