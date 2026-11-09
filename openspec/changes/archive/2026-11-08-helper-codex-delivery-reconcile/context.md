# 最小基线与环境

本单基线6e2c836491f8ba506833d6f849fbe73530e13f92。现有helper没有独立的原生投递对账主规格；已读hub-lifecycle/context.md、spec.md，task-dispatch-lock/context.md、spec.md，以及helper-first-offer-idempotence和r52-delivery-status进行中件。仓中未发现覆盖本单的已冻结平台能力时间线，不据聊天把拟实现能力写成已上线。

codexDriver的poll对unknown且无turn ID直接返回内部failed receipt；原生history回放未带起止时间；adapter无游标重订阅与driver替换不能恢复旧对象证据。inbox_lifecycle已有complete native terminal事务和unknown防重边界。本步只补分析/失败测试，详细代码坐标、只读本机第二样本、来源与风险在docs/reviews/helper-codex-delivery-reconcile/ANALYSIS.md。

原生测试使用仓锁0.159.2二进制，私有HOME/CODEX_HOME、清空继承环境、loopback不可达推理端点，无凭据。只起自己的stdio子进程并在结束时回收；不操作在线helper/daemon，不访问ts3dev55机器。系统Python首轮规范门导入缺依赖，锁依赖venv重跑读数另记。原子写锁只证明本任务互斥，不增加部署权限。


## 第二步实现环境（2026-11-08）

唯一写者实取CODEX_THREAD_ID=01a1bacf-9317-7b91-bf86-0a0c88c834f6；入口仍同分支5ed477a5，已从工蜂独立fetch并核rulings fe4ad167。实现位置为tools/sessionhelper/sessionhelper/codexDriver.py、driver_adapter.py、inbox_lifecycle.py；新增常规库存tests/test_codex_delivery_reconcile.py并登记scripts/helper-test-files.txt。既有dispatch测试的合成原生时刻改为与dispatch intent一致，保留100/123/124断言的用例明确给其合成intent 99/122；未放宽产品证据门。

锁依赖解释器 /tmp/coder-0232-locked-venv/bin/python3；OpenSpec /tmp/coder56solzzc-openspec-1.16.0/node_modules/.bin/openspec；原生二进制仍/tmp/coder-stale-protocol-01592/codex及第一步同SHA。变异入口tests/helper-codex-delivery-reconcile/mutations.py只复制Git跟踪的.py到一次性本地目录，不上传包。原生原文、反测、变异与规范门最终读数见docs/reviews/helper-codex-delivery-reconcile/IMPLEMENTATION.md及raw/impl/。

完整门暴露轻量__new__ adapter调用约定：新计时/证据属性对尚未完整初始化的caller提供既有缺省值；不改blocker授权/路由语义。test_codex_stale_thread.py中的断线锁竞争夹具现要求未提交queued及turn/start=0，保留RECONNECT_CROSSED_ADMISSION_LOCK原断言。
