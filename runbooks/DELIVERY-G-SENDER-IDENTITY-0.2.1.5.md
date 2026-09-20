# 0.2.1.5 发件人标识交付

## 结果

- Agent 到飞书的共同出口在 `routeToFeishu` 最终入队前统一生成可见正文。
- 发件身份只取已认证信封的 `env.From`；不采信正文自称。
- 落款优先展示仓内已有的 `SessionEndpoint.FullSessionName`，不存在时使用 canonical `SessionName`，不新增人工显示名映射。
- 回复说明使用真正可路由的 canonical `SessionName`。实现中发现 `FullSessionName` 当前不能被 `dispatchSelector` 解析，因此没有把“可读但送不到”的名字写成回复承诺。
- 普通 Agent 消息写明 `回复方式：发送 #<canonical session> <内容>。`；真实回送测试已从飞书入口送回原 WebSocket。
- producer 与 `no_directory` 会话明确写“单向 Agent 输出，不可直接回复”。
- 巡检告警不走 Agent 共同出口，已在 `internal_notify.go` 的实际入队正文单独标 `DingWei 巡检告警`，并明确不可直接回复。
- 正文所有以 `@` / `#` 起始的行统一缩进一格；回复说明本身不以 selector 起始。

## 防双重落款

现有 `reply_prefix` 有四个赋值入口；真正把它写入正文的是 sessionHelper `reply_body`。Hub 只把下列前缀视为可信旧落款：

- `【<已认证 session>】`
- `【…·<已认证 session>】`

任意其它 `【...】` 不能抑制真实落款。这样保留旧 helper 自动回复，又不要求所有 helper 与 Hub 同步升级。

## 可证伪证据

基线：

- `go build ./...`：rc=0
- `go vet ./...`：rc=0
- `go test ./... -count=1`：rc=0（恢复两发变异后的最终全仓回归）

变异一：删掉 Hub 新增 Agent 落款、保留回复说明和全部其它逻辑；全仓测试 rc=1，唯一失败为 `TestAgentFeishuOutboundBodyIdentifiesAuthenticatedSenderOnce`。已还原。

变异二：删掉巡检告警落款、保留正文和“不可直接回复”说明；第一次全仓除目标 `TestInternalNotify/入队成功返回_202_且正文落到队列` 外，另撞一条时序族 `TestSessionNameReconnectKeepsNameAndReleaseReuses`；未把它算作覆盖证据。原样复跑后 rc=1，仅 `TestInternalNotify` 目标用例转红。已还原。

## 没有做、没有证明

- 没有改 `notifyNewSession`、control task、scheduler 等非 Agent、非巡检系统通知；依裁定另卡，不在本版扩围。
- 没有部署生产；生产动作仍由组长出单、prodops 执行。
- 没有宣称 producer 可回复；其接收循环明确忽略普通入站。
- 没有宣称 `FullSessionName` 可作 selector；真实回送实验证明当前应使用 canonical `SessionName`。
- Python sessionHelper 全量发现式测试未通过：本机缺少 `pexpect`，79 条中 78 条运行、1 个模块导入报 `ModuleNotFoundError`；本版没有修改 helper 产品逻辑，仅把其版本常量由 `0.2.1.4` 抬到 `0.2.1.5`。未把该环境失败包装成通过。
