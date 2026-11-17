## ADDED Requirements

### Requirement: R60-001 冻结输入与当前连接
集成版本 MUST 从指定图标热修基点按③④①②合入冻结源，统一0.2.3.40，保留可信来源图标与错卡护栏；持久IPC心跳 MUST 使用当前连接及其能力，inventory MUST 精确97。

#### Scenario: R60-S001 重连不向旧连接发心跳
- **WHEN** 同一IPC依次连接有心跳能力的两代WS和无能力第三代WS
- **THEN** 前两代分别recorded，重连后旧发送数不增，第三代unsupported，删除current包装本格业务断言红

### Requirement: R60-002 原请求责任恢复
组合链路 MUST 在控制回执真实两秒写背压失败后保留unknown原身份和正文，通过dw_send_status观察自动writer恢复原delivery，不盲重做；两次CLI同ID MUST 仅一条Hub创建。

#### Scenario: R60-S002 真实helper未知回执恢复
- **WHEN** 持有真实Hub写槽使已提交回执写达到两秒截止，调用方通过真实MCP查状态
- **THEN** unknown可见且正文/原source保留，释放写槽后同source恢复原delivery，Hub创建一次；删写截止或同source保护须业务红

#### Scenario: R60-S003 双CLI同ID
- **WHEN** 两个独立安装send.py进程使用同request_id发送同正文并显式查状态
- **THEN** source/delivery一致且Hub创建一次，删第二次原ID复用须业务红

### Requirement: R60-003 离线显示责任完整
集成回归 MUST 分别覆盖封口已有viewer的gap与连接关闭，以及离线非空队列的gap与拒绝新显示责任；不改变既有离线状态帧/readonly/正常关闭合同。

#### Scenario: R60-S004 封口已有真实viewer
- **WHEN** 已建立viewer之后后台接纳封口，再执行离线清理
- **THEN** gap可见且真实连接关闭，分别删该分支gap与CloseNow须业务红

#### Scenario: R60-S005 离线待显队列
- **WHEN** 受控队列有待显字节、状态写槽被同步持有，开始离线清理
- **THEN** gap立即可见且新显示责任不获准入，释放槽后readonly离线状态和1000关闭保留，分别删pending-gap/stopped须业务红

### Requirement: R60-004 固定源完整门与失败保存
正式交件 MUST 对同一clean SOURCE_COMMIT执行canonical/build/四树PGstrict Go/浏览器/strict与五轮串行m8，保留原始日志、rc和逐名SKIP/FAIL。五轮至少三轮全绿；只允许指定两格已知interrupt，越界、R60改动文件用例失败或全绿不足三即停。

#### Scenario: R60-S006 已知两格范围
- **WHEN** 五轮m8出现interrupted(9)
- **THEN** 逐名保留，仅FeishuOnce/Flapping与ViewMentionRoute指定子格可用例外，其他或未知归属停报，不以单跑覆盖
