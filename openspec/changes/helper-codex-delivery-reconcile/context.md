# 最小基线与环境

本单基线6e2c836491f8ba506833d6f849fbe73530e13f92。现有helper没有独立的原生投递对账主规格；已读hub-lifecycle/context.md、spec.md，task-dispatch-lock/context.md、spec.md，以及helper-first-offer-idempotence和r52-delivery-status进行中件。仓中未发现覆盖本单的已冻结平台能力时间线，不据聊天把拟实现能力写成已上线。

codexDriver的poll对unknown且无turn ID直接返回内部failed receipt；原生history回放未带起止时间；adapter无游标重订阅与driver替换不能恢复旧对象证据。inbox_lifecycle已有complete native terminal事务和unknown防重边界。本步只补分析/失败测试，详细代码坐标、只读本机第二样本、来源与风险在docs/reviews/helper-codex-delivery-reconcile/ANALYSIS.md。

原生测试使用仓锁0.159.2二进制，私有HOME/CODEX_HOME、清空继承环境、loopback不可达推理端点，无凭据。只起自己的stdio子进程并在结束时回收；不操作在线helper/daemon，不访问ts3dev55机器。系统Python首轮规范门导入缺依赖，锁依赖venv重跑读数另记。原子写锁只证明本任务互斥，不增加部署权限。
