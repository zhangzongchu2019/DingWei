# Tasks

- [x] T-01 读取现有 Driver 契约及 Antigravity ACP 实测事件，完成驱动与 Adapter 接入。
- [x] T-02 加入模拟 ACP 回合测试，覆盖回答先于终态、重复信封不重投、正文冲突拒绝。
- [x] T-03 完成仓库完整构建测试、真实容器部署及 Hub 回合投递复验。
- [x] T-04 从不可变提交打包，核对包哈希并将坐标交给 opus50tc2k。
- [ ] T-05 由页面用户确认 dwgemini4 回答的可视呈现；独立复核与正式发版由 opus50tc2k 处理。

## 交接记录

- 2026-11-20，当前执行者；源码 commit `4aeca81dd22320c3afd0ece194fd229421a17a1e`，tree `adba94bf25ab86146a6b0c9c7f4b25e0777347c9`。完成：完整门 rc=0（两套 helper 各 1301 项，27 个 Python 与 2 个 shell 入口），OpenSpec strict 52/52；四项定向变异均在对应断言转红；目标容器真实 ACP 回合与 Hub 自检消息均 accepted→done，结构化事件开关已开。包 `sessionhelper-0.2.3.42-antigravity-4aeca81d.tar.gz` SHA256 `c96b7dbc8daa83e92cbf87c593dc03813e771a201b6e7e470412b57285aa423c`，放入 opus50tc2k 容器 `/data/ai-dev-data/incoming/antigravity-driver/`，Hub 投递 `dw-dlv_286bc127c64a4a6036e3217736fccae0` 已在其 helper 记录 accepted。阻塞：页面用户尚未确认渲染效果；下一步 T-05 与独立复核。
