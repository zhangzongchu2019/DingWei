# Tasks

- [x] T-01 读取124d24ee冻结设计和main206d41b2，确认仅第1片授权、无迁移。
- [x] T-02 两常量改168h，Token336h保持；真实PG边界/续期/存量五项九子格，原夹具合法续组前提修订不删断言。源码：`2fd6aebacbb3dae3ac3fa82d84968367552151ca:internal/tempgroup/lifetime_pg_test.go`。
- [x] T-03 两个旧值变异compile0且业务断言红，恢复SHA相同/基线绿。证据：`b5e6e590102dedfa19c234fd726ea341fb23685e:docs/reviews/r62-extseat-ttl/evidence/mutations.json`。
- [x] T-04 固定SOURCE_COMMIT=2fd完成canonical、四包三遍PG strict：3081顶层/5250含子项PASS，0FAIL/0中断，84次逐名SKIP；strict42/0。证据：`b5e6e590102dedfa19c234fd726ea341fb23685e:docs/reviews/r62-extseat-ttl/evidence/runs.json`及`SKIPS.md`。
- [x] T-05 报告/原日志SHA/可执行VERIFY均Git归档；最终普通推送核ls-remote后以交审信坐标请qwen只读复核、抄组长。证据：`b5e6e590102dedfa19c234fd726ea341fb23685e:docs/reviews/r62-extseat-ttl/REPORT.md`；投递回执写本席交接记录，不冒称已读。
- [ ] T-06 组长/独立复核验收；作者门不代签。

## 交接记录
- 2026-11-18；coder56solzzc；源2fd6aeba，证据b5e6e590102dedfa19c234fd726ea341fb23685e。完成：只实现第1片两个TTL；canonical/PG真实三遍/两刀/strict完成，前驱9576中止尝试及旧夹具红绿如实单列，不拼接。阻塞：产品实现无阻塞，独立复核待回。下一步：普通推送核远端后qwen只读复核，组长另定集成；不改r61、VERSION、main或部署。
