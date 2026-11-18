# Tasks

- [x] T-01 读取124d24ee冻结设计和main206d41b2，确认仅第1片授权、无迁移。
- [x] T-02 两常量改168h，Token336h保持；真实PG边界/续期/存量五项九子格，原夹具合法续组前提修订不删断言。源码：`2fd6aebacbb3dae3ac3fa82d84968367552151ca:internal/tempgroup/lifetime_pg_test.go`。
- [x] T-03 两个旧值变异compile0且业务断言红，恢复SHA相同/基线绿。证据：`b5e6e590102dedfa19c234fd726ea341fb23685e:docs/reviews/r62-extseat-ttl/evidence/mutations.json`。
- [x] T-04 固定SOURCE_COMMIT=2fd完成canonical、四包三遍PG strict：3081顶层/5250含子项PASS，0FAIL/0中断，84次逐名SKIP；strict42/0。证据：`b5e6e590102dedfa19c234fd726ea341fb23685e:docs/reviews/r62-extseat-ttl/evidence/runs.json`及`SKIPS.md`。
- [x] T-05 报告/原日志SHA/可执行VERIFY均Git归档；最终普通推送核ls-remote后以交审信坐标请qwen只读复核、抄组长。证据：`b5e6e590102dedfa19c234fd726ea341fb23685e:docs/reviews/r62-extseat-ttl/REPORT.md`；投递回执写本席交接记录，不冒称已读。
- [ ] T-06 组长/独立复核验收；作者门不代签。

## 交接记录
- 2026-11-18；coder56solzzc；源2fd6aeba，证据b5e6e590102dedfa19c234fd726ea341fb23685e。完成：只实现第1片两个TTL；canonical/PG真实三遍/两刀/strict完成，前驱9576中止尝试及旧夹具红绿如实单列，不拼接。阻塞：产品实现无阻塞，独立复核待回。下一步：普通推送核远端后qwen只读复核，组长另定集成；不改r61、VERSION、main或部署。

## 17:33Z追加核项
- [x] T-07 查消息/组独立期限、补真实PG常驻格及两增量刀；两源真实WS已投影反例保留，canonical新补测源重跑。
- [ ] T-08 R62TTL-PM01由组长裁定已投影接管边界；未授权生产语义修法。

交接补记：2026-11-18，coder56solzzc，补测0b9a10f9。完成：过期组未到期消息分阶段现状、常驻PG格三遍及两增量刀，main/2fd已投影边界双源实证。阻塞：已投影队列语义待组长定。下一步：只追加Git证据交组长/qwen，不自行扩大TTL片修Hub。
