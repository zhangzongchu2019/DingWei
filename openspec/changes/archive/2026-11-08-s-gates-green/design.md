# 实施设计

## 仓储边界

五处增量逐项处置：兼容探针两处保留旧版表结构读写夹具；启动配置测试一处保留独立损坏数据库夹具；迁移校验一处改为 repo.OpenSQLite，SystemCtx 仅用于无租户数据的内存 schema；history 一处保留精确文件例外。history 依赖裸事务显式提交/回滚及异步单连接写队列，而 Pool 只提供回调事务；本次替换会扩大重写面，故暂不改。例外不豁免逃逸与租户负测。

## 依赖

沿用 r53 已复核的拼音实现，只登记突破，不改算法。

## 租户闸

请求/JSON 字段不当作 schema。DDL 中真实租户列不能因迁移目录而漏检；标记只证明存在性，行为测试与评审另证。具体实现及真实缺口待后续记录。


## schema识别评审面

旧扫描12行含JSON、查询、约束引用；新检测器从Go AST字面串及SQL文件仅识别CREATE TABLE列与ALTER TABLE ADD COLUMN声明，保留多行、引号、注释剥离和常量字面拼接测试。动态生成DDL不在静态覆盖内。np_external_principal真实tenant_key归extseat（0105迁移位置在store不能用来掩盖业务域），由显式表归属清单标注；其它真实tenant列仍归store。feishu/webhook.go与feishulogin/types.go仅请求/JSON字段，不触发。本提交仍如实红在store/extseat缺标记，未贴空标记转绿。


## extseat拒绝审计（㊾-补7 c71bc54e）

IssueToken在原tenant拒绝分支复用Identity.Store.AppendAudit（同一个audit_log）；Actor为provider/issuer/subject派生摘要，不含调用方tenant；Detail仅server配置tenant、固定op及deny。Target为空，时刻取服务时钟。资源解析前仅允许这笔审计写，不读目标或选择器；写失败只记固定日志，返回仍为原ErrTenantRejected与空值。PG用例实际验证存在helper.ho与不存在missing.ho均无resolver调用、持久审计同形。该方法没有HTTP写口，原error对象/文本/空返回钉死，现有上层整包回归另跑。静态或时间比较不替代人工时延复核。


## store拒绝审计（㊾-补7 c71bc54e）

StageClarificationInbound的两种元数据前置拒绝（Permission/Invalid）复用既有audit_log，源主体由issuer/subject派生摘要；不信任未通过校验的tenant，派生tenant明确标为unresolved，不通过查绑定来填值。固定op、deny与调用方服务时钟；target为空，不记录消息正文、parent、消息ID或调用方tenant。审计失败只写固定日志，原错误对象和零返回不变。没有新增表或迁移。测试包装真实隔离SQLite连接，分别统计审计写与所有其它SQL，资源spy=0且审计=1。此测试连接器需要一处OpenDB，仅精确测试文件登记例外，不增加产品建库或放宽escape。存在/不存在parent实际预置并核实，除随机行ID外五个审计字段逐字相同。该接口没有HTTP响应写口，保留原error对象和零结果；上层Hub原有状态/正文/头回归由m8整包门验证。时延仅证明没有目标解析，不作统计等时承诺。


## 长门取证方式（组长6458a0db4eaa五项条件）

store race按go test-list完整379项轮转8个独立进程，片内串行-count3；逐片实际执行集合与计划比对、全并集diff为空、每例run3且PASS/FAIL/SKIP总计1137，Skip列具体条件。Go包全局/环境属于进程，临时DB和目录逐夹具独立，无固定端口/共享可写目录。extseat单独完整包race3。原始整包长门若未完成而由本席处置，仅核实PID后发信号、保留真实非零退出与原因，不冒称该进程通过；完成的分片矩阵单独作为被授权的store race证据。
