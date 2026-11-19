# Tasks

This change is design-only. Implementation checkboxes are future work and are intentionally not marked complete.

- [x] Record owner decisions ㊿-补129 and ㊿-补131 against ruling tip `86e135328`.
- [x] Verify the implementation baseline against main `206d41b26dab646c0e50a111eed5a45ccf7c72f6` and record source coordinates.
- [x] Specify seven-day group/message lifetime and fourteen-day token preservation.
- [x] Specify Unicode display names without changing canonical ASCII addresses or AMR routing.
- [x] Specify summary copy, owner controls, single-Agent shorthand, and internal/external recipient mapping.
- [x] 指定组内通信、保留可选临时配对与普通组并存、离组/到期及租户准入/展示；原转换前提由㊿-补132③覆盖。
- [x] Specify a 500-record per-external-Agent retention bound and idempotency/backpressure behavior.
- [x] Include file-upload feasibility only; prohibit implementation in this change.
- [x] Provide staged slice plan with migration needs, rollback, estimates, and reviewer suggestions in `docs/design/R62-EXTSEAT-SLICES.md`.
- [x] Before any implementation, re-check main and migration allocation; current `0112` onward numbers are planning placeholders only. Evidence: final allocation in release 8213f12937aba18080a384bcabd2ae483d246098 `internal/store/migrate/migrations/0112_np_agent_display_name.sql`, `0113_extseat_groups.sql`, `0114_agent_feishu_summary_copy.sql` (DCR took 0115).
- [x] Implement slice 1 only after a separate implementation assignment and review. Evidence: TTL slice frozen 3639406c2 (qwen PASS e9ab4cbf); NAME slice frozen ceab869cf.
- [x] Implement later slices only after their explicit assignment and acceptance gates. Evidence: GROUPS frozen 4fd154f8f, COPY frozen bd8d238b4, AMEND design eb194143; integrated 5248e813a11443f1f0a2948508337fd1e6ee5eec and released 8213f12937aba18080a384bcabd2ae483d246098 (closeout 6bbca0d8d2a951015b88ceed6eff12b8de567d9c).

## S-R62-EXTSEAT-AMEND

- [x] T-01 核原设计124d24ee与业主㊿-补132③/148f1be7；只追加设计，不改TTL3639406c或产品。
- [x] T-02 写明真实普通组/有效配对并存、显式组不回退、AG镜像、入组/离组/AG与token到期矩阵。
- [x] T-03 写无目标/不建AG入口，未编组MCP拒绝、Console显示、后续入普通组及兼容边界。
- [x] T-04 内部抄送缺省关、外部缺省开，显式偏好/verified映射规则及Scenario同步。
- [x] T-05 更新PM01已投影件边界、切片/迁移计划/回退/验证矩阵，不提交实现SQL。
- [x] T-06 全仓OpenSpec1.16.0 validate --all --strict与仓AGENTS要求的canonical测试门；保存源锚/原日志。证据：r62 集成报告 444643076f5933c4657535f8b5f0e2511c16b4e4:docs/reviews/r62-integration/REPORT.md（strict 51／0、run-build-tests rc0）。
- [x] T-07 交件证据、任务交接、普通推送及远端坐标核验；独立复核/是否实现由组长另令。证据：组长逐片冻结（TTL 3639406c2、NAME ceab869cf、GROUPS 4fd154f8f、COPY bd8d238b4）并现网发布 8213f12937aba18080a384bcabd2ae483d246098。

## 交接记录

- 2026-11-19 · coder56solzzc.zzc · 基设计124d24ee3818762f977920d5a015e4dfdca80663。完成：AMEND设计/spec/计划修订；阻塞：无产品阻塞，验证尚待执行；下一步：冻结设计源、跑strict及canonical、追加证据。R6到件即优先复验；本记录不是实现或部署授权。
