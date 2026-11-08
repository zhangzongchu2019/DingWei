# Helper Codex投递证据对账提议

## Why

本变更针对传输替换或通知丢失后原生回合已完成而本地投递仍未终态，以及入场超时后没有turn ID而无法只读核查的缺口。第一步只提交分析与基线失败测试，明确可见输出、原生完成证据和投递ACK三条链，防止通过补造时间或盲目重投消除积压，第一步经复核后，第二步按冻结口径实现本地只读对账、实际提交时钟和原代次证据恢复，保持未确认不重投的边界。

需求来源：组长派单request_id 932d3ef7daa0，rulings〔51〕仓库 `git@git.code.tencent.com:wego-ai/dingwei.git` / `review/opus50tc2k/rulings` / `24985813e3a8d3434e7b1872d6df9b6ada9ffa12` / `docs/rulings/S-TEAM-MIGRATE-HOSTED-20261017.md`。写锁更正以2026-11-08T10:23:37Z认证消息为准。第二步需求来源为组长2026-11-08T10:58:44Z派单（本机认证收件dw-dlv_7fc4f64296b13e2db32a4f1e6cd101c9）和rulings〔51〕-补3 fe4ad167990bb5bfd7593cf6145462f93de661d8；第一步5ed477a5已通过，授权同分支追加实现。

## What Changes

第一步分析/基线红测保留。第二步实现codexDriver、driver_adapter及inbox_lifecycle的受限只读恢复、真实turn/start时钟和原ACK代次事务证据门；不新增schema、配置键或版本，不改Hub/View。常规边界测试登记完整库存，钉0.159.2的原生组和逐刀变异显式执行；不以展示完成推进ACK。本变更不apply/archive，不据本地实现宣称线上已生效。

## 待确认假设

- ts3dev55报告52条已完成与101次替换的逐条关联未由本席核查。
- 本机夹具证明放大前半链，完整daemon故障链尚待后续授权实现/复核。
- 历史可信范围已核为本席全部driver dispatch记录；从未dispatch且尚未materialized的空binding不进入候选。账本缺失不能证明未执行，unknown不据此重投；不假定LIMIT 1足够。

## 待 PM 决策

本单第二步无新增待决事项。HCD-PM01已由〔51〕-补3冻结为不加schema，若确需迁移停报；HCD-PM02已冻结为单席1请求、同thread至少15秒、每轮200条触顶incomplete留pending及1秒原生时间容差。HCD-PM03子Agent/多thread路由仍排除，详细设计待本单交件后的独立派单，不在本单开始。

## Impact

第二步实现挂点为codexDriver、driver_adapter、inbox_lifecycle及既有driver账本detail；相关tests、显式库存计数及本变更文档同步。现网、他席机器、绑定配置均不操作，不新增部署/重启/数据修复授权。
