# A 路初始消费者接口

基线262f4747；PG0105由A编写，D仅0106。本件为接口中间件，未经最终门，不作交件验收。

- D消费者见types.go的Groups。CreateTempGroup持有A传入repo.Tx，分配AG后调用CreateExternal回调一次，再写双成员。这样AG名称决定external名称，且np_group_member外键成立；失败由A事务整体回滚。D不调用Service、不另开事务。
- RecheckGroup必须锁组、核席位是本组成员、generation、到期/解散；返回指定Target（与external恰好双成员）。TouchGroup持久事件键去重，kind只用extshared两种值。
- C调用IssueToken(ctx,extshared.Principal,short,targetFullName)，仅限完成飞书服务端验证的内部路径；A再核tenant/主体字段与目标活跃，不暴露自行签发端点。
- B调用Authenticate(ctx,bearer) (Seat,error)、Recheck(ctx,Seat) error、ListOnline(ctx,Seat)、Send(ctx,Seat,to,body,clientMessageID)、Wait(ctx,Seat,max)、Ack(ctx,Seat,deliveryID,gen)。业务入口再次现核快照，不接受JSON Seat作为凭据。
- token首次返回raw，Seat带TokenID；后续raw认证计算哈希、恒时比较。Recheck不续token；成功ListOnline/Wait/Send/Ack才触达presence。AG仅发送首次受理与received首次成功确认触达。
- 收件权威PG，lease到期原delivery重取，ACK只有received；外部→helper用Projector消费outbox，入SQLite后只改投影状态，不充received。
- 共享包当前未定义target/receipt结构；本包DTO是消费者结果和接口容器，内部固定字段引用extshared.Principal/AGCode/phase/stage/kind/errors，不另定义这些枚举。集成若补共享DTO可平移，不改语义。
