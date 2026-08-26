# C-P0 细案 v1(devo48 拍板,prodops 组内审;张宗楚 07-27 裁决=实施层组内自决,不再上报)

前提锚:契约面冻结=internal/bridge@d56cf15d(BR-01~14 一并冻,prodops 已 AGREE);interface-contracts-v1 生效(tag^{commit}=10701574)。
hub 现状(main@a5f7c25 起算,已 fetch 复核):internal/m8/hub.go **4945 行单体**;Type B 根因坐标=hub.go:158 `sessionReadIdleTimeout=75s`+coder/websocket **Read context 过期即关整条连接**——driver(CLI 会话)慢/挂 ≠ 连接该死,正是契约 O3a 反面。

## 步骤与顺序(3a 前置解锁 A 组,再修根因)

**C-P0-3a StubHub 测试件(先行,纯新增零风险)**
- internal/bridge StubHub 参考实现:Deliver/Events(Seq+Ack+重放)/Interrupt/Health+五种 FaultOp 注入+AckChain 断言;确定性 Clock。
- 验收=BR-01/03/04/05/06/07/08/13/14 可执行绿。
- 产出即通知 A 组(claudek3):E门夹具对接面就绪(O4,禁另起)。

**C-P0-2 WS-driver 解耦(Type B 根治,动 main 侧 hub)**
- 工作分支 wip/c-p0-bridge(基 main),不动生产。
- 改法:①WS 读循环保活与 driver 状态分离——read idle 不再直接关连接,server ping/pong 承担传输层活性;②新增 driver health 端点(三态,禁 presence,契约 §3.1);③idle 关闭仅当 health 可用且非 ok 时允许(O3c),close 必带 close_reason 入账。
- 验收=BR-02/09/10/11 四负例+hub 既有测试回归绿。
- 上线另走发版纪律(coordinator 归口,硬 gate 张宗楚亲执,失败即回滚)——本步交付止于分支+测试证据。

**C-P0-3b hub 独立起停**
- hub 从 sessionHelper 单体析出独立 cmd 入口(配置注入,可单独 start/stop/restart),shutdown 走 CloseShutdown 有序关闭。
- 验收=BR-01+独立起停脚本化(prodops 运维视角审此步重点)。

**C-P0-4 飞书 unlock mock+生产件骨架**
- Unlocker 两实现:mock(登记制 fail-closed,demo MockAuth 同型)+生产件骨架(飞书盖章服务独立化,真接线属 P5 授权域不在本批)。
- 验收=BR-12。

**C-P0-5 集成自测收口**
- BR-01~14 全绿;真实现与 stub 跑同一套契约测试(conformance);出收口报告(锚+判据矩阵)。

## 工期与边界

- 估:3a=0.5-1d / 2=1-1.5d / 3b=0.5-1d / 4=0.5d / 5=0.5d;串行关键路径≈3-4d,3a 完成即解锁 A 组不等后续。
- 全程零生产变更;任何部署走发版纪律四要素+授权。
- 用例前置已满足(BR 冻结@d56cf15d),跑通即过,争议=补用例(规约23)。

## Docker 立项(同批组内批准)

张宗楚裁决=组内批准即启动。流程:prodops 出规格稿(三主机收一 Docker/镜像打 sha/同构环境)→devo48 组内审一轮→批后即启动。规格稿指针发我即可。
