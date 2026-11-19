# Tasks

- [x] T-01 读取124d24ee冻结设计和main206d41b2，确认仅第1片授权、无迁移。
- [x] T-02 两常量改168h，Token336h保持；真实PG边界/续期/存量五项九子格，原夹具合法续组前提修订不删断言。源码：`2fd6aebacbb3dae3ac3fa82d84968367552151ca:internal/tempgroup/lifetime_pg_test.go`。
- [x] T-03 两个旧值变异compile0且业务断言红，恢复SHA相同/基线绿。证据：`b5e6e590102dedfa19c234fd726ea341fb23685e:docs/reviews/r62-extseat-ttl/evidence/mutations.json`。
- [x] T-04 固定SOURCE_COMMIT=2fd完成canonical、四包三遍PG strict：3081顶层/5250含子项PASS，0FAIL/0中断，84次逐名SKIP；strict42/0。证据：`b5e6e590102dedfa19c234fd726ea341fb23685e:docs/reviews/r62-extseat-ttl/evidence/runs.json`及`SKIPS.md`。
- [x] T-05 报告/原日志SHA/可执行VERIFY均Git归档；最终普通推送核ls-remote后以交审信坐标请qwen只读复核、抄组长。证据：`b5e6e590102dedfa19c234fd726ea341fb23685e:docs/reviews/r62-extseat-ttl/REPORT.md`；投递回执写本席交接记录，不冒称已读。
- [x] T-06 组长/独立复核验收；作者门不代签。证据：qwen 独立复核 PASS 提交 e9ab4cbf；组长终审冻结 3639406c2；随 r62 集成第一段 5248e813a11443f1f0a2948508337fd1e6ee5eec 现网发布 8213f12937aba18080a384bcabd2ae483d246098（结项件 6bbca0d8d2a951015b88ceed6eff12b8de567d9c）。

## 交接记录
- 2026-11-18；coder56solzzc；源2fd6aeba，证据b5e6e590102dedfa19c234fd726ea341fb23685e。完成：只实现第1片两个TTL；canonical/PG真实三遍/两刀/strict完成，前驱9576中止尝试及旧夹具红绿如实单列，不拼接。阻塞：产品实现无阻塞，独立复核待回。下一步：普通推送核远端后qwen只读复核，组长另定集成；不改r61、VERSION、main或部署。

## 17:33Z追加核项
- [x] T-07 查消息/组独立期限、补真实PG常驻格及两增量刀；两源真实WS已投影反例保留，canonical新补测源重跑。
- [x] T-08 R62TTL-PM01于2026-11-18T18:05:27Z裁定：本片不改语义，通用在途授权归切片三；安全格与两源红探针已归档：`c30389636f3086d58f1138ff13f82c329a5a5e7e:docs/reviews/r62-extseat-ttl/group-expiry-followup/REPORT.md`。

交接补记：2026-11-18，coder56solzzc，补测0b9a10f9。完成：过期组未到期消息分阶段现状、常驻PG格三遍及两增量刀，main/2fd已投影边界双源实证。阻塞：已投影队列语义待组长定。下一步：只追加Git证据交组长/qwen，不自行扩大TTL片修Hub。

交接补记：2026-11-18，coder56solzzc，承接c3038963与18:05:27Z裁定。完成：主REPORT补已知边界，PM01处置完成；0b9安全格、两源红探针及原日志保持，未新增肯定既有投递边界的回归。阻塞：本片无待定语义，独立验收仍待回；切片三通用在途授权设计不属于本单完成项。下一步：普通追加推送核远端，告组长／qwen文档新锚；不改产品、main、r61或部署。
