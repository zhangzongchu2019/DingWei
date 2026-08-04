# T17 fan-out 代理 部署 runbook 终稿(两阶段,v2 终态架构)

前提:设计=v1.1 冻结@1b67eec+v2 增补(t17-fanout-design-v2-addendum.md)过 delta 审;实现=v1@6e612bc+v2 增量全绿。
**飞书开放平台零操作;外部依赖=发布授权唯一。**(旧草稿"订阅方式切换/Token+Encrypt Key 获取"两外部件作废,见 v2 增补 §J)

## 0. 前置(一次性,全内部)

- prodops:生成随机 verification_token 写入生产 Hub unifiedrobot bot channel 记录(内部共享秘密,非开放平台件);同值入代理 sinks[prod].token。
- prodops:nginx 反代路径(仅 https 接入 bot 与管理 API 需要;纯 ws 序可先不开公网)。
- 凭据:FEISHU_APP_ID/SECRET 从生产 env 复制入代理 outbound.bots+accesses(600 权限)。
- 部署代理 systemd(Restart=always),`/healthz` 绿+配置代次核对。

## 阶段 1:影子并联(生产零变更、零重启)

**原理**:飞书长连接多连接=事件随机分流到其中一条(非广播)。代理 ws 接入与生产 Hub 自有 WS 并存:直达生产的事件照常处理;落到代理的事件经投递 API(带 token,明文归一 payload)送回生产。两路都到生产=零丢失;Hub 消息 ID=飞书 msg_id+INSERT OR IGNORE=零重复。

1. 配置代理:accesses[unifiedrobot,ws] + sinks=[{prod,authoritative,all,none,token},{test,besteffort,whitelist,测试环境,token}],白名单先放一个内部测试账号。热载开启 ws 接入。
2. 核 `/metrics`:代理侧事件计数开始增长(≈总量一半)、prod_delivered 与之吻合、spool 深度≈0。
3. **无损验证(核心证据)**:生产 Hub 消息表每小时计数 vs 切换前基线一致(两路合流=总量守恒);按 feishu_msg_id 抽样核零重复。
4. 测试链全验:白名单账号消息→测试 Hub 收到(约半流量,影子期已知特性,阶段 2 后 100%)→测试 Hub CLI 回复→飞书收到带[测试环境]后缀。
5. 出站托管预验:测试 Hub 出站已走代理(v1 即有);核托管 ACK 链路 outbound_id→代理审计→真 msg_id。
6. 观察窗:**建议 ≥24h**(spool/DLQ 零异常、代理重启自愈=WS 断开期间事件全落生产直连路、消息率守恒持续成立)。

**阶段 1 回滚**:管理 API/SIGHUP 热关 ws 接入(秒级)→ 全部事件回生产直连,生产全程无感。

## 阶段 2:接管翻转(单一 env 变更,低流量窗)

前提:阶段 1 观察窗全绿+发布授权。

1. 生产 Hub env:`FEISHU_TRANSPORT=proxy`+`FEISHU_PROXY_URL`+`FEISHU_PROXY_TOKEN`(出站 caller token,后缀=none 档)。
2. 重启生产 Hub(唯一生产侧操作):其 WS 断开→飞书事件 100% 落代理接入→投递 API 全量进生产;出站切托管 ACK。翻转期间零事件真空(代理接入一直在收它那一半,断开瞬间变全量)。
3. 验证(30min):消息率守恒;测试 Hub 白名单流量升至 100%;出站:生产发一条真实消息→outbound_id ACK→飞书实收→审计日志真 msg_id 关联;名称解析(按名发信)经 /resolve 透传实测。
4. 观察窗 2h:metrics 双向、spool/DLQ、healthz。

**阶段 2 回滚(一步)**:生产 env 切回 `FEISHU_TRANSPORT=ws`+重启——分流语义下代理接入可保持 ON 不动(回滚后自动回到阶段 1 影子态,不丢消息);要彻底退出再热关接入。

## 真机证据清单(prodops 审环境)

- [ ] 阶段 1:消息率守恒对比表(基线/影子期,每小时)+feishu_msg_id 零重复抽样
- [ ] 阶段 1:代理 kill -9 重启一次→WS 断开期事件经生产直连路无损(计数守恒不破)
- [ ] 阶段 2:翻转前后消息率守恒+测试 Hub 白名单 100% 到达
- [ ] 出站托管链:outbound_id→代理审计→Lark msg_id 三点关联样本×3+[测试环境]后缀实收截图
- [ ] DLQ 演练:注入恒败出站件→重试退避→DLQ+告警触发
- [ ] 热载防呆:烂配置 PUT 被拒(代次不动)+authoritative sink 无 force 删除被拒
- [ ] 停测试 Hub 5min→生产两向不受扰(besteffort 隔离)

## 值守与告警

- 告警:authoritative 投递 spool 深度>阈值、DLQ 新增、代理 ws 断连>60s、healthz 503、配置代次异动(未经审计的变更)。
- 代理=进出双向关口:值守等级按生产命脉;systemd Restart=always+双向 spool WAL=崩溃自愈;阶段 2 后生产直连路仍可一步 env 回滚兜底。
- 凭据/token 旋转:管理 API 单向设置+热载,旋转窗口 fail-closed 语义与 v1 同。

## 已废弃(留档勿执行)

旧草稿(t17-fanout-deploy-runbook-draft.md@9159734)的"订阅方式长连接→回调 URL 切换"方案:被 v2 终态架构取代,该路径需开放平台操作+Token/Encrypt Key 获取,现全部不需要。
