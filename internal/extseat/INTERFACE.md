# A 路初始消费者接口

基线262f4747；PG0105由A编写，D仅0106。本件为接口中间件，未经最终门，不作交件验收。

- D消费者见types.go的Groups。CreateTempGroup持有A传入repo.Tx，分配AG后调用CreateExternal回调一次，再写双成员。这样AG名称决定external名称，且np_group_member外键成立；失败由A事务整体回滚。D不调用Service、不另开事务。
- RecheckGroup必须锁组、核席位是本组成员、generation、到期/解散；返回指定Target（与external恰好双成员）。TouchGroup持久事件键去重，kind只用extshared两种值。
- C调用IssueToken(ctx,extshared.Principal,short,targetFullName)，仅限完成飞书服务端验证的内部路径；A再核tenant/主体字段与目标活跃，不暴露自行签发端点。
- B调用Authenticate(ctx,bearer) (Seat,error)、Recheck(ctx,Seat) error、ListOnline(ctx,Seat)、Send(ctx,Seat,to,body,clientMessageID)、Wait(ctx,Seat,max)、Ack(ctx,Seat,deliveryID,gen)。业务入口再次现核快照，不接受JSON Seat作为凭据。
- token首次返回raw，Seat带TokenID；后续raw认证计算哈希、恒时比较。Recheck不续token；成功ListOnline/Wait/Send/Ack才触达presence。AG仅发送首次受理与received首次成功确认触达。
- 收件权威PG，lease到期原delivery重取，ACK只有received；外部→helper用Projector消费outbox，入SQLite后只改投影状态，不充received。
- 共享包当前未定义target/receipt结构；本包DTO是消费者结果和接口容器，内部固定字段引用extshared.Principal/AGCode/phase/stage/kind/errors，不另定义这些枚举。集成若补共享DTO可平移，不改语义。

## 授权资源与固定 scope（组长合同第6节）

依据工蜂 opus50tc2k/r53-integration，593ed5efdaa51bf671cc41fa5d8dc0b7c20ed78e 的 docs/design/R53-MVP-CONTRACT.md 第6节。

- main 由组长统一装配 Service.TenantKey（DW_FEISHU_TEAM_TENANT_KEY）与 Service.Resource（暂定 https://dw123.cmjrt.com/mcp），A/B/C 使用同一配置；A不读独立环境项，也不内置资源默认值。缺资源时签发与鉴权拒绝。
- 0105 令牌行持久 audience 与固定 scope 文本 `list_online send_message wait_for_messages ack`，四项全授予，不支持子集；Authenticate/Recheck及业务入口均从DB核资源与完整scope，失败不续期。B的metadata与权限面须同此合同。
- IssueToken 签名保持不变。C只交已验证的Principal，不交飞书 access_token/refresh_token；飞书凭据只在C回调内使用、用完即弃，不由A保存。

## C路消费者适配说明

C路k31t2075报告已独立fetch并核验A中间锚2e5e0b02b5d77db0428c5d357152ce7e418f54bf（tree 2b099a6150b6db951453a7c033c019025c77177d）及Groups与e261aaa5无差异。C使用IssueRequest／IssuedToken容器的消费者接口，目前仅假实现测试；与A公开签名语义一致但Go类型不直接满足。拟由组长集成层薄适配器调用A并映射Seat.ID、group和token／错误，A与C均不为此改包。本条为对接报告及集成待验证项，不是已完成真实装配、A路独立验收或阻塞结论。

## 投影锁与时钟重核

ProjectOne在同一PG事务中锁住outbox、外部席位/Agent/属主、AG及helper目标Agent/属主，并把锁内读取的Delivery.SourceAgent和目标快照交给Projector。SourceAgent不序列化到通讯响应；HubExtseatProjector不再通过Identity另取PG连接查询双方身份，只使用上述内部快照写稳定ID的SQLite队列。消费者不得把该内部Projector当公开HTTP输入面。

Authenticate取得令牌行锁后重新采样时钟，在Group消费者返回后再次核token/AG整点失效与回拨。业务入口复用同一检查；发送、收件领取与received确认在后续身份/消息锁返回后再核，拒绝排队期间已经过期的授权或消息。后台Run遇瞬态投影失败保留PG行并下一轮按原ID重试，错误回调不得记录Bearer或正文。
