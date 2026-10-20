# 第85条 D3 附注闭合（作者候选）

仓库wego-ai/dingwei，分支review/coder/pilot-feedback-impl；在原48befea2bfbd8cbe0685f35be5a8c5fbbfad4576后追加单个提交，不改写原交件。组长审、prodops交叉核；未部署、未升级、未动席位/43/dw123、未发真实飞书或导入真实PRD。

## 必修与附注

1. TestPilotQuestionProviderErrorUnknownAndNeverResends通过真实processQuestions→真实SQLite outbox，用合成身份绑定夹具及fake QuestionSender返回非空provider ID与error。第一次读通知必须unknown；第二次process后发送次数仍1。撤掉sendErr检查命中QUESTION_SENDER_ERROR_NOT_UNKNOWN指定红，既有unknown不盲重发、UUID/私有接收人门仍运行。此项不声称真实飞书故障实验。
2. 最后身份check在Prepare已提交后失败、本次尚未调用provider：临时失败回queued；撤权cancelled。Prepare内部check失败导致SQLite rollback：临时失败pending_config；撤权cancelled。当前PM失效同样取消，空收件人/未绑定仍待配置，不能把未配置误判撤权。真实SQLite事务和合成身份check注入分别命中对应前后路径；实际PG锁/发送顺序仍由原PG测试覆盖，发送计数为0。
3. 新DeferUnsentQuestionNotice是可信worker专用恢复接口，按outbox ID/问题/代次/收件人CAS，只改queued/pending_config/dispatching。只有本次成功Prepare且未调用provider的证明才能把dispatching回queued；过去dispatching在本次没有新Prepare证明时只能unknown，保留attempted_at，绝不推断未发送。sent/unknown/cancelled不可回排队。回执写入异常后的已尝试发送仍保留dispatching→unknown原恢复语义；不以通道错误自动重试。
4. question-context移除ListUsers平台目录。挂R9任务按当前任务组；仅挂spec按owner创建或其active Agent所在组并集；发现组后在现有PG组/成员/Agent/用户锁内重新证明owner仍相关，R9当前角色再次重核。联系人只给最小ID/显示名。owner可用allow_user显式输入最多20个稳定用户ID；先前由owner配置的policy PM/leader也属于明确允许的人选。此查询不建立对象授权，实际配置/转派仍完整重核。没有姓名猜测或凭飞书open_id选人。
5. 稳定user ID升序分页：默认50、上限100，5分钟加密游标绑定actor/spec/task/显式集合/limit/当前授权摘要；移组/失效/显示名等变化409。测试含不同用户ID的多页无遗漏、页面上限、移组后游标拒绝，以及发现候选组后、加锁前实际离组的窗口。spec-only候选组最多200，超出失败关闭；每页仍扫描相关成员并算摘要，未声称增量分页索引优化。
6. PM/leader在HTTP身份查找前，以及直接store的幂等hash/写入前trim并小写。真实HTTP大写转派成功并返回小写；直接store policy/assign及大小写同一请求重放验证。未改写历史数据库。

页面显式人选区同时支持创建与仅详情路径；按联系人游标逐页加载，精确ID查找，不再因全平台目录截断丢人选；极大名单提示精确ID选取。403时清空人员缓存/输入。D3浏览器15格，含详情页单独打开的owner入口；没有将问题答复变成执行指令或签收。

## 测量与证据

最终输入是Git tree 5175f2cee711a68ccd0ee3982213ed61bf9a6c85，不是commit。不可变Git标签refs/tags/evidence/coder/feedback-rework-input-5175f2ce已发布；FROZEN-TREE-FETCH.json记录全新空仓从工蜂独立fetch、peeled tree一致。审查方先fetch该标签和候选分支，再按REPRO-GATES.py、REPRO-BROWSERS.py及tests/pilot-feedback/withdrawals.py可重跑。

全部最终门都在同一最终树导出/private HOME/清空环境/离线Go依赖下实际运行：全仓默认并行Go rc0，65包，52测试包通过、13无测试文件，30条件跳过逐项保留在GO-PACKAGES.json；vet、定向race、canonical rc0（canonical计数见日志）；完整45撤修全部designated_red，前后Go/Feishu/helper模块正对照rc0；10套浏览器rc0，D3新增/更新15格。accepted-go的最后单独全仓与final-gates的vet/race/canonical、final-withdrawals、final-browsers共同构成最后正式读数；失败轮次未改写。

helper源码和测试与原D3完全同blob，未重跑773项完整helper；本轮跑question helper模块和canonical，不冒称新helper全套。SQLite账本、PG schema、依赖锁、镜像与候选版本0.2.3.7不变。VM-4-12-ubuntu，UID1001，Go1.22.2/Python3.12.3；无外部DSN与真实服务凭据。VERIFY.py逐Git blob、大小、SHA核验源码和本目录原始证据，独立审查仍待。

## 两次全仓失败与对照

早树gates/go.log中TestTerminalViewEmptyBufferRequestsRefresh在2.25秒读刷新信封超时；最终树final-gates/go.log中TestBudgetRealPGDeadlineAndCapacity断言background SQL remains: active=2 err=nil，两轮全仓rc1原样保留。并发门全部结束后，REPRO-CONDITIONAL-PAIRS.py按原D3候选48befea2/最终树5175f2ce交替三次跑两个失败用例；6轮结果分布{('baseline', 0): 3, ('final', 0): 3}，原始日志和条件在conditional-pairs。再单独跑同最终树完整默认并行全仓，最后rc0见accepted-go，不把前两轮改写成通过。

UNCHANGED-FAILURE-INPUTS.json证明consolemux、repo、pgtest完整子树及终端Hub实现/测试两文件与原候选Git对象相同。这与定向对照只限域说明受审改动之外的输入和复现情况，失败原因仍未实证，不擅自认定环境故障，也未放宽时间/SQL清理断言或修改相邻产品。

## 早轮与边界

早轮树966d4884be7ec38c0518eaa465721731bbada671也已发布不可变标签refs/tags/evidence/coder/feedback-rework-early-966d4884，并有空仓独取回执；其gates/withdrawals/browsers完整保留；随后发现仅详情页的owner看不到显式选人区，才将人选区改为创建/详情共用，并补第15格浏览器。早树到最终树只有Go HTML常量和该浏览器脚本两文件；尽管逻辑其他部分相同，最终树仍实际重跑所有上述门，不冒称旧读数是新树。
开发初跑还发现空PM必须待配置而非撤权、合成worker夹具必须先明确配置PM，均已修正并有正反覆盖；曾误用questionCreate测试helper参数导致编译错误，修正为原签名。没有将编译错误或这些开发失败计入撤修红。

本件未混入project_context后继4b923f93或D2后继ddad3cff，也未混入D1候选bc5d6257所新增的模板/new_spec_id接口。最终集成必须同时保留这些分支的权限/限流/模板与new_spec_id语义；单分支全仓通过不等于合并后通过。回滚冻结写入门在D2后继的docs/runbooks/PILOT-PREDEPLOY-ROLLBACK.md，正式执行单应引用；本件不授予操作授权。
