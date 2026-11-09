# 任务

- [x] T-01 增加接入页字段说明并更新接口文档。
- [x] T-02 增加回调原因分类、安全页面/JSON说明及结构化日志。
- [x] T-03 为目标缺失增加内部安全分类，并覆盖回调原因、来源守卫与敏感信息断言。
- [x] T-04 执行原因映射变异、全包测试、m8、build.sh、真实浏览器与 OpenSpec strict；记录结果见 `docs/reviews/mcp-auth-diagnostics/RESULT.md`。
- [x] T-05 提交并推送指定分支，核验远端 tip/tree，回执不部署。
- [x] T-06 按复核意见补回调 AttemptID 不匹配的拒绝/不签发/一次性 state 负例，并为 HTML callback 错误页 CSP 增加产品回归断言及对应变异。

## 交接记录

- 2026-11-09，执行者 togpt60lunazzc.zzc，实现提交 `5036650853e56310ad2f703dd78a91179f3e7dac`，tree `6a3a5ffa38dc8e85afa4a7fac224d34acf232156`；已推送并核验远端。
  - 完成：实现授权说明、五类 callback 失败诊断、attempt/目标安全元数据和相关测试；新增本变更规格。
  - 阻塞：无；仍待将本交接状态追加到分支并复核最新远端坐标。
  - 下一步：已推送实现及验收证据；补记最终远端坐标并回执；不部署。

- 2026-11-09，执行者 togpt60lunazzc.zzc，源代码提交 `5036650853e56310ad2f703dd78a91179f3e7dac`。
  - 完成：10 个语义变异命中预期标记；三目标包、m8 全包、双 helper profile build gate、真实浏览器及 OpenSpec strict 均通过。
  - 阻塞：无实施阻塞；独立复核由接收席执行。
  - 下一步：复核远端最新 tip/tree 后发回执；严格按只实现范围，不部署。

- 2026-11-09，执行者 togpt60lunazzc.zzc，复核补强尚未提交。
  - 完成：增加 `CALLBACK_ATTEMPT_MISMATCH_BEFORE_ISSUE` 负例，检查 `FailureAuthorizationUsed`、签发调用计数为零及 state 重放失败；增加 `CALLBACK_FAILURE_CSP_POLICY` 响应头断言；两项产品变异均命中标记。变异脚本合计 12/12 判红，其中 11 项为产品行为变异，另 1 项 `browser-csp-count` 是既有测试计数变异。
  - 阻塞：首次以系统 Python 跑构建测试门时缺少 `websockets`，helper discovery 失败；已转用隔离虚拟环境重跑，结果待收齐。
  - 下一步：完成 mcpgw、feishulogin 与构建测试门，补记录数值，提交并推送追加提交后核验 `ls-remote`。
