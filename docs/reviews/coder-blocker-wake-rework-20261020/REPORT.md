# 第85条 D2 六项附注闭合

仓库wego-ai/dingwei，分支review/coder/pilot-blocker-wake-impl；单个后继追加在原2e6b95063132bda9ceae4b346d519901256f4ead之上，不改写已交件。组长审、prodops交叉核；未部署、未动席位或宿主。

## 六项

1. 路由入队和outbox标记仍为两个提交，但Hub专属blocker信封现用目标＋signal ID的SHA256确定delivery ID。SQLite INSERT OR IGNORE对该稳定ID幂等。真实本地WS＋SQLite回归故意令可靠入队后Finish失败、保留pending，清空Hub内存claim模拟重启，再调度，证明两个实际入队调用ID完全一致、原投递存在且outbox终止。只撤确定性ID时指定BLOCKER_RESTART_DUPLICATE_DURABLE_DELIVERY红。未声称完整进程kill重启或物理exactly-once，标准可靠传输仍可能重发同一ID，由原helper持久去重。
2. 删除canInput重复canRead项；非任务读者必须是本卡明确waiting principal才能input，开发/审查身份不会自动获答复权。
3. 每blocker/认证actor累计256个新input、滚动60秒10个；owner也受限，SQL事务内计数及写入，429拒绝不改变卡或outbox。幂等重放先核，不占额度；测试同时验证满额重放、拒绝无部分写、时间窗口恢复和累计上限。系统条件事件不冒充人工答复。
4. 被等方仅按精确卡ID读取where、expected_input、kind及最小ID/version/state；不读spec、SHA、角色、恢复检查、答复历史或其他卡列表，task_version=0；移除指派后的历史缓存继续剥离旧上下文。真实HTTP和store均有正负例，包含非被等方拒绝、空ID枚举拒绝。不能据此查看整个spec/任务。
5. 0057只是此前设计保留，未发布迁移；当前实际0058追加在0056后，有意跳号，无空壳DDL、无重编号。实现文档已写清。
6. docs/runbooks/PILOT-PREDEPLOY-ROLLBACK.md为必须引用的回滚计划：先冻结Console/Agent/MCP及后台worker试点写，等待事务完成、确认冻结，再保存一致备份并回旧版；回滚期间继续冻结试点写/执行，恢复兼容门对账并另令签收后才解冻。不授权实际执行。

## 最终门

默认并行全仓Go rc0，65包：52测试包通过、13无测试，30条件跳过逐项在GO-PACKAGES.json。vet、race、canonical均rc0（canonical具体计数见原始log）；完整45项撤修全部指定断言红，Go/helper模块正对照及恢复均rc0。全仓/vet/race/canonical冻结Git tree 521b3cb13f486f0369ca4903cff208311b26a13a；最终完整tree 29d292495fe24a45cbc65ccc43025309d259c78c仅改离线撤修检查器一个期待marker，生产与所有Go/Python单测、canonical输入一致，见INPUT-EQUIVALENCE.json。tree不是commit，候选包含对应字节。

helper整棵源码/测试与原D2相同，本轮未重跑765项完整helper，原候选读数保留作前序；本轮执行blocker helper模块和canonical，不冒称独立验收或新全套helper通过。未改PG迁移、helper锁/版本或镜像。环境私有HOME、离线依赖、无外部DSN，VM-4-12-ubuntu UID1001 Go1.22.2 Python3.12.3。

## 初跑及限域

首轮撤修运行保留withdrawals-initial：waiter-no-private-get实际已在私有字段泄露断言红，但旧marker名仍为整任务拒绝；因语义改为最小卡视图，映射同步成BLOCKER_WAITER_MINIMAL_CONTEXT，完整45项重跑。未把首轮计为通过。
开发阶段还修正两个夹具：真实WS注册完成前取client曾nil；改为观测注册；新卡创建后task version改变，读取测试改取当前state。旧测试将where一概视为私有，现按业主本次明确共享where/needed更新，仍验证spec/角色/恢复检查不外泄。

本分支未混入project_context后继4b923f93，最终集成须同时保留它；D3/D1仍基于各自已交候选，集成时须保留本件新门，不以旧同文件覆盖。
