# `0.2.1.1` · D 补：告警通路接线

## 范围

本版只把 `0.2.0.4` 已存在的 Unix-socket 告警出口接到业主的飞书个人会话；不增加告警种类、不改告警正文、不改产品代码。

北极星只有一个：**业主本人明确回复收到一条由真实巡检失败产生的告警。** `queued`、发送日志或数据库状态均不能替代该回复。

## 开工前只读实测（2026-09-19）

执行方在现网回报：

- workpulse 自报 `version=0.2.1.0`、`commit=aaecc083f9dbc8b7aa133cf0c0f244edac443005`；
- `WP_NOTIFY_SOCKET`、`WP_ALERT_CHAT_ENTITY`、`WP_ALERT_BOT_CHANNEL`、`WP_ALERT_CHAT_TYPE` 均为 `unset`；
- `/run/dingwei/notify.sock` 不存在；
- invariant-runner 正常运行，默认请求 `/run/dingwei/notify.sock`，最近一轮 9 条断言成立。

因此本版是**配置接线**，不是重写出口。不得借本版修改 `cmd/workpulse/internal_notify.go` 或扩充巡检内容。

## 凭据与目标值边界

- 本件、提交、日志与消息里只记录四项配置“已设置/未设置”，**不得记录值**。
- `WP_ALERT_BOT_CHANNEL` 必须取自现网已知成功的同一飞书机器人通道；不能凭名字猜。
- `WP_ALERT_CHAT_ENTITY` 必须取自该通道下、经业主本人入站消息核实的个人实体；不得使用 Agent session name。
- `WP_ALERT_CHAT_TYPE=personal`；`WP_NOTIFY_SOCKET` 必须与 runner 实际请求路径一致。
- 配置必须写入现网的持久母本，不能只注入当前进程或当前容器。母本位置由 prodops 只读核实后填入执行记录，值仍不回传。

## 执行顺序（由组长出单、prodops 执行）

### 1. 基线与回退点

1. 记录当前 workpulse `version/commit`、四项配置的 set/unset 状态、runner 状态；不输出值。
2. 备份实际 supervisor 配置母本，记录备份路径与文件摘要；备份内容不得进仓或消息。
3. 确认回退动作是恢复母本并重启 workpulse，且不会替换二进制。

### 2. 接通

1. 在持久母本写入四项配置，其中 socket 路径须与 runner 一致。
2. 只重启 workpulse；不改二进制，不重启其它服务。
3. 只读确认：workpulse 仍自报原 `version/commit`；socket 存在且为 Unix socket、权限 `0600`；四项均 `set/nonempty`（不输出值）。

### 3. 北极星：真实失败

1. 用临时断言配置制造一条**真实、可控、可恢复**的巡检失败，并以 `invariant-runner.sh once` 走生产同一条 runner → Unix socket → outbound → 飞书链路。不得用直接 POST 冒充真实失败。
2. 临时配置与临时状态文件不得覆盖生产断言集或生产心跳；执行后删除临时件。
3. HTTP `202`、队列 `done`、发送日志只用于排障，**均不判通过**。
4. 只有业主以 `source_kind=user` 明确回复“收到”，本格才通过。若业主回复“没收到”，原样记失败；若未回复，记“待判”，不得写成成功或失败。

若使用日志/数据库判断某一环“没有发生”，须先用一条已知成功投递证明同一查法能命中；阳性对照也查不到时，该探针无分辨力，停止下结论。

### 4. 阴格：关闭时必须 404

1. 保留 `WP_NOTIFY_SOCKET`，临时清空告警目标配置并重启 workpulse，使 socket 仍存在、handler 明确处于 disabled 状态。
2. 再以临时失败配置运行一次 runner；必须观察到 socket 请求返回 **HTTP 404**，且 runner 写出 `ALERT_DELIVERY_FAILED http=404`。不得接受 `202`，也不得以“没消息”代替 404。
3. 该轮不得产生飞书告警；若业主收到第二条，阴格失败并立即停。

这里不能把 `WP_NOTIFY_SOCKET` 一并清空：那只会得到 socket 不存在/连接失败，无法证明既有 `notify disabled` 的 404 语义仍在。

### 5. 恢复最终启用态

1. 恢复目标配置并只重启 workpulse。
2. 确认 socket 重新为 `0600`、四项配置均为 `set/nonempty`、runner 仍为 RUNNING。
3. 不再制造第三条告警；以配置形状和 socket 状态确认最终启用，避免给业主重复发验收消息。

## 失败与回退

以下任一发生即停在原地报告，不改告警内容、不换收件人碰运气：

- socket 未创建或权限不是 `0600`；
- workpulse 自报版本/提交变化；
- runner 得不到预期 `202`，或阴格不是 `404`；
- 系统显示已发送但业主未确认收到；
- 配置只能落当前容器，找不到可持久恢复的母本。

回退只恢复执行前 supervisor 配置母本并重启 workpulse；回退后复核四项状态与基线一致、socket 状态与基线一致、runner 仍运行。

## 可证伪判据

- 删除/清空任一目标配置后，启用态探针必须不再得到 `202`。
- 把 disabled 分支改成静默成功，阴格必须从预期 `404` 变红。
- 绕过 runner 直接 POST 不能满足北极星，因为它没有证明真实失败能触发整条链路。
- 没有业主本人“收到”的回复时，本件不得宣称发布验收通过。

## 明确没有证明的事

- 本件不证明所有未来告警内容都正确，只证明既有巡检失败能到达业主。
- 本件不证明飞书长期可用，也不建立第二通知渠道。
- 本件不改二进制、不改版本号；版本与装配由组长统一处理。
- 本件尚未执行生产变更；生产动作等待组长出单与业主当次授权。
