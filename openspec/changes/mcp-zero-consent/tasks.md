## 实施

- [x] T-01 核裁定、指定基线、原子新锁、独立分支。
- [x] T-02 OpenSpec新变更与配置/权限/回退文档。
- [x] T-03 自动命名、目标现核、单/多目标与原入口兼容实现。
- [x] T-04 新PG迁移、旧行/长名/回退约束、并发race三遍。
- [x] T-05 各判据正反与逐刀变异，官方0.167 URL-only单目标E2E。
- [x] T-06 全包、canonical两profile、strict与SHA证据。
- [x] T-07 同分支提交推送核ls-remote并交独立复核。

- [x] T-08 三项核验后留证接管，独立工作树合并r56.2。
- [x] T-09 login三域隔离、三格负例与去前缀两格目标红。
- [x] T-10 真浏览器单/多目标、相关包/canonical/strict与证据追加交审。

## 交接记录

- 2026-11-09 coder56solzzc，基线fce2ede8；完成取锁与裁定读取，已建立新工作树和变更文档；阻塞无；下一步实现与隔离验证。写者01a1be21，ZERO-CONFIG结项不动旧树/旧锁，M3后置。

- 2026-11-09 coder56solzzc，产品123e0e6（在f37实现上追加防空策略），全部指定门通过；30/30目标变异红、CLI URL-only原生E2E rc0、M8 1512/0/37、canonical双profile各1129/1129，strict29/0；首轮作废读数保留。阻塞无；下一步推送/核远端与证据交审，桌面未测，不部署。

- 2026-11-09 coder56solzzc，产品123e0e6db92c7fdc30cd693d186affa2b4fb7bf2已推工蜂新分支，ls-remote逐字一致；本次证据追加随后独立提交并再次核远端tip。完成全部实现与作者门；阻塞无；下一步交独立复核，保留写锁至合入/结项指令，不部署。

- 2026-11-09 coder56solzzc，写者01a1bf0d；按改派核远端c85b4e93、原树clean/同HEAD、ps无目标进程后接管，原锁留证；合并r56.2并修复login分域，三格负例通过、去前缀两格分别目标红；阻塞无；下一步浏览器与完整门禁、同远端分支追加交审。

- 2026-11-09 coder56solzzc，冻结实现10e6c2213；三格用途隔离、去前缀两格目标红、原30刀与新增两条CSP浏览器变异均目标红；真Chromium单/多目标均完成换票，canonical双profile1129各、m8 1512/0/37、完整三包race三遍、strict29/0通过。已核最新rulings dcfd24ef，新增补30/31与本次合并r56.2一致，无另增本单需求；main仍fce2ede8，tasks/v0.1-B为7afbfebc。首次完整多包运行碰源码写入期间vet类型信息不一致，留作废原文，冻结后整条命令重跑中；本段下一步等rc0后追加证据提交并推送同远端分支交审；M3后置、不部署。

- 2026-11-09 coder56solzzc，冻结源10e6c2213整条多包命令重跑rc0（packages-final），无FAIL；前轮vet并行写入失败仅在superseded。实现后仅追加本次证据/任务交接，不改产品与测试字节；下一步同分支快进推送核验并交组长独立复核。

- [x] T-11 合并 r56.2 新 tip d5a6fbbc，连接页采用 r56.2 CSP、选择页保留已验证回调源；重跑浏览器两格／两条 CSP 变异／两包／m8／canonical。

- 2026-11-09 coder56solzzc，写者01a1bf6a；三条实读满足后接管，旧锁保留，新工作树追加合并 d5a6fbbc，无 rebase／改写；读数与首轮相同的 rulings dcfd24ef 和任务板 7afbfebc 已核。完成连接页 CSP 冲突处置与飞书 HTTPS 确认页夹具；下一步冻结源并重跑指定门，M3 仍后置，不部署。

- 2026-11-09 coder56solzzc，冻结merge 2c403d430（父d311864c、d5a6fbbc），所有指定门通过：真Chromium单／多目标、两条实际CSP撤除红、feishulogin99／mcpgw237、m8全包1512/0/37、canonical两profile各1129/1129及27Python／2shell rc0、strict29/0。长TMPDIR/socket124字符引起的中间canonical失败保留，短TMPDIR下10格及完整重跑均绿。下一步同工蜂分支快进推送核远端后交独立复核，再核M3接管条件；未部署、不自验收。

- [x] T-12 按组长指定三项实测接管，merge第三发布值0de00bc4（不rebase），login三域/去前缀重跑，A/B单多目标四格真浏览器与三条CSP撤除红，完整门后只收尾冻结。

- 2026-11-09 coder56solzzc（01a1bf9a，产品merge3bd3d97b9、测试源2e8878510、本次证据追加）：完成T-12，三域2刀、原30刀、CSP3刀红，A/B四格实际换票，mcpgw239/0、m8 1512/0/37、canonical双profile1129/1129及27Python/2shell rc0、strict29/0。首轮CSP源顺序断言作废原文保留；未改产品/既有断言或超时。阻塞：独立复核/是否发布待乙P1计划；下一步普通push核远端并交审后冻结，不增名单/Console功能、不部署，M3锁未动。
