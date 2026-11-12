# 任务

- [x] T-01 建立 OpenSpec 变更并记录页面现状与约束。
- [x] T-02 实现未知文案、重发确认/已处理保护、本地不再跟踪及可注入时钟的 24 小时收起。
- [x] T-03 扩展浏览器回归，覆盖四项要求并执行语义变异。
- [x] T-04 完成页面版本来源只读调研并记录报告。
- [x] T-05 执行 md-render/page-literal、OpenSpec strict、浏览器及 run-build-tests 门，整理原始日志。
- [x] T-06 生成 SHA256SUMS，提交并推送分支，核验 ls-remote。

## 交接记录

- 日期：2026-11-11；执行者：togpt60lunazzc；commit：待提交。
  - 完成：确认基线、任务边界并建立规格。
  - 阻塞：无。
  - 下一步：实现 renderer 与浏览器回归，补齐门禁证据。
- 日期：2026-11-12；执行者：togpt60lunazzc；commit：待提交。
  - 完成：四项需求实现与定向浏览器回归；四个语义变异分别红在对应判据；只读版本来源调研已记录。
  - 阻塞：全量浏览器兼容复跑与 run-build-tests 尚未完成。
  - 下一步：完成全量 runner、run-build-tests、OpenSpec strict 与 diff 检查，整理校验和并推送。
- 日期：2026-11-12；执行者：togpt60lunazzc；commit：待提交。
  - 完成：全量浏览器 runner 40/40、md-render sync/parity、OpenSpec strict 35/0、run-build-tests (`BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`)、console 版本角标 4 项及 `git diff --check` 全通过；四条语义变异复跑均红在指定判据；报告、原始日志与 SHA256SUMS 已整理且 18/18 校验通过。
  - 阻塞：无。
  - 下一步：提交并推送，核对远端分支 tip。
- 日期：2026-11-12；执行者：togpt60lunazzc；内容提交：`47042045a148c68f0c958d9267a186b95ea8569e`。
  - 完成：分支 `togpt60lunazzc/view-unknown-ux` 已推送；推送后 `ls-remote` 返回同一 tip。
  - 阻塞：无。
  - 下一步：无。
