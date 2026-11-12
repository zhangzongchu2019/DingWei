## Tasks

- [x] T-01 建立 OpenSpec 变更与本地储存/批量确认设计。
- [x] T-02 修复 unconfirmed_retryable/retried 文案及计时排除。
- [x] T-03 实现按 session_name + delivery_id 的 30 天 localStorage 标记与 500 项上限。
- [x] T-04 增加默认取消的批量不再跟踪对话框。
- [x] T-05 扩展现有浏览器回归覆盖文案、持久化、过期、上限、批量取消/确认。
- [x] T-06 每类新增判据执行语义变异并记录红点。
- [x] T-07 只读记录 Hub admission_unknown 终态现状与 expired_unknown 设计利弊。
- [x] T-08 执行定向与全量浏览器、OpenSpec strict、run-build-tests、版本字面量扫描、diff 检查；整理 SHA256SUMS。
- [x] T-09 提交、推送并核验 ls-remote。

## 交接记录

- 日期：2026-11-12；执行者：togpt60lunazzc；commit：待提交。
  - 完成：页面文案/计时、本地标记、批量不再跟踪、回归与 Hub 只读报告均已完成；浏览器 runner 40/40 aggregate_rc=0、run-build-tests rc0、OpenSpec strict 37/0、md-render sync/parity、版本扫描与 diff 检查通过；6 项语义变异各在指定判据红。实现首提交 `bcf395dde871ebe331e4ddc9f8b1633ad5692d22` 已推送，`ls-remote` 一致。
  - 阻塞：无。
  - 下一步：无；本单完成。
