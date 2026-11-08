# Tasks

- [x] T-01 复核 waiting admission、首次渲染、历史重建与 R15 分页入口。
- [x] T-02 建立事件先后可控的浏览器回归，覆盖活动回合、刷新和空闲对照。
- [x] T-03 实现共用时间戳定位函数并接入各时间线插入路径。
- [x] T-04 证明旧 renderer 红、追加变异红；运行指定浏览器回归与 10×空闲/满载轮次。
- [x] T-05 检查 SHA 钉值，运行 `scripts/run-build-tests.sh` 和 OpenSpec strict。
- [x] T-06 更新证据、提交并推送，核对 `ls-remote`。

## 交接记录

- 2026-11-04，执行者 `togpt60lunazzc`，实现提交 `0847df1473dfc431730bf16f931369eb49a80a88`（后续交接记录于文档提交）。
  - 完成：共用定位函数、真实 HTTP 历史回放回归、旧 renderer/append 变异、空闲与满载轮次、相关浏览器套件、OpenSpec strict 和完整构建门均完成。
  - 阻塞：无。
  - 下一步：交组长独立复核；本分支冻结，不再追加。
