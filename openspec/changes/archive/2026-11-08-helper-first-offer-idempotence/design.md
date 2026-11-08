# 首offer幂等设计

## 根因与修复

首次写者仍由SQLite CAS决定，不增加Hub全局锁、重试循环或新后台任务。函数读到同一活动job的OfferConnSeq已为正时直接返回当前持久快照。CAS失败后重新读取，只有JobID相同、非终态且OfferConnSeq为正时才把这次竞争作为已完成的首offer返回。读失败、job消失、已换job、终态或仍未冻结都沿用原错误。

这两个提前返回保证已有OfferConnSeq、Revision、Stage、ActivationConnSeq与receipt状态不会因陈旧offer再写而变化。没有新增接口、持久状态或错误码；对应HOF-001。原调用者提供空client/job等既有边界不扩大授权。

## 确定性判据

在Repository包装器中阻塞两个读取同一未冻结raw后的CAS，两方都到屏障后先放序号20的写者，再放序号10的竞争者；不靠sleep触发概率。中间格额外提交installed回执，证明CAS失败重读保留新阶段。陈旧初始快照分别在offered/downloaded/restarted阶段重放，序号和Revision保持不变。新job/终态/仍未冻结的CAS失败继续拒绝。

一刀语义变异同时撤掉两处新增冻结幂等返回，双CAS及多Revision判据都须恢复语义红，恢复后绿。原样保留r48产品+测试覆盖提交的红读数，不把测试覆盖提交说成原始r48 tree；产品blob单列。
