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

代码侧仍是**配置接线**，不是重写出口。不得借本版修改 `cmd/workpulse/internal_notify.go` 或扩充巡检内容。

### 第二轮只读核查与母本落点更正

现网 `workpulse` 配置位于容器内 `/etc/supervisor/conf.d/dingwei.conf`。执行方确认：

- 该文件不在宿主 bind mount 中；
- `docker diff` 将它标为容器可写层修改；
- 可见挂载只有两份 secret 与 `/data`，均不是该配置；
- invariant-runner 配置同样是容器可写层新增文件，且未显式设置 `DW_NOTIFY_SOCKET`；
- 当前只读信息无法定位一份会在容器重建时恢复这些配置的持久母本。

这证明只改当前容器可写层不能作为交付。不过后续仓内核查找到了持久母本：

- `deploy/image/supervisord/dingwei.conf.template` 是 supervisor 配置模板；
- `[program:workpulse]` 的 `environment=%(ENVIRONMENT_WORKPULSE)s` 是环境配置注入点；
- `supervisor-structure.schema.json` 与模板上的 `t112-environment` 注释共同约束键数和排序键名摘要，不记录键名或值。

组长在 ts-No.31 裁定：`0.2.1.3` 的整容器可重建性降级为登记项，不再阻塞计划内的 `0.2.1.1`。本版改走仓内模板母本，**不得**直接编辑容器可写层作为持久化手段。

`0.2.1.1` 现在的执行边界为：

1. 四项配置由 prodops 写入 `ENVIRONMENT_WORKPULSE` 的受控来源，再由仓内模板渲染；值不进仓、不回传；
2. 模板注释与 schema 同步更新为目标键集合的 `key_count` 和 `keys_sorted_sha256`；
3. 模板、schema、旧现网证据之间只允许一条**精确声明的迁移**，不能把任意漂移放行；生产生成新去值证据后必须删除迁移声明；
4. `alert-route-stays-wired` 与接线同批上线，不得提前部署到禁用态制造永久红噪声；
5. 整容器其它可重建性风险转入 `0.2.1.35`，不冒充本版已解决，也不再阻塞本版。

prodops 按 T-112 原机械算法完成了只读摘要：当前 workpulse 环境为 `12` 个键；加入本版四项后的目标为 `key_count=16`、`keys_sorted_sha256=acc4bf12c0c7fa0aaf5c006cb7b907f412203b7a2529b256d764c9fc29f52ad8`。取证未回传键名集合或任何值，未修改现网。

### 🔴 模板输入来源仍待裁定

全仓检索只找到模板里的 `environment=%(ENVIRONMENT_WORKPULSE)s` 占位符，没有找到仓内渲染器、持久输入清单或 `ENVIRONMENT_WORKPULSE` 的来源定义。模板与摘要闸能持久保存“应当有哪一组键”的单向承诺，**不能凭空恢复四项值**。

因此本件当前可以交付模板目标和判据，但生产执行单还必须指出 `ENVIRONMENT_WORKPULSE` 的持久来源。若执行时仍从当前容器现场拼出该字符串，重建后四项值仍会消失，等于原样复刻本版根因。在组长裁定该来源前，不得把“模板已更新”写成“接线已可持久恢复”。

## 凭据与目标值边界

- 本件、提交、日志与消息里只记录四项配置“已设置/未设置”，**不得记录值**。
- `WP_ALERT_BOT_CHANNEL` 必须取自现网已知成功的同一飞书机器人通道；不能凭名字猜。
- `WP_ALERT_CHAT_ENTITY` 必须取自该通道下、经业主本人入站消息核实的个人实体；不得使用 Agent session name。
- `WP_ALERT_CHAT_TYPE=personal`；`WP_NOTIFY_SOCKET` 必须与 runner 实际请求路径一致。
- 配置必须通过仓内 `dingwei.conf.template` 的 `ENVIRONMENT_WORKPULSE` 来源生成，不能只注入当前进程或当前容器。值始终不得回传。

## 执行顺序（由组长出单、prodops 执行）

### 1. 基线与回退点

1. 记录当前 workpulse `version/commit`、四项配置的 set/unset 状态、runner 状态；不输出值。
2. 记录仓内模板锚、渲染输入来源与当前实际 supervisor 配置摘要；备份内容不得进仓或消息。
3. 确认回退动作是恢复上一版模板渲染结果并重启 workpulse，且不会替换二进制。

### 2. 接通

1. 由仓内模板渲染 supervisor 配置；四项从 `ENVIRONMENT_WORKPULSE` 的受控来源注入，其中 socket 路径须与 runner 一致。
2. 只重启 workpulse；不改二进制，不重启其它服务。
3. 只读确认：workpulse 仍自报原 `version/commit`；socket 存在且为 Unix socket、权限 `0600`；四项均 `set/nonempty`（不输出值）。

### 3. 北极星：真实失败

1. **执行前先通知组长，由组长提前告知业主下一条是演练；未得到组长确认不得触发。**
2. 用临时断言配置制造一条**真实、可控、可恢复**的巡检失败，并以 `invariant-runner.sh once` 走生产同一条 runner → Unix socket → outbound → 飞书链路。不得用直接 POST 冒充真实失败。
3. 临时断言的 ID、描述以及最终告警正文必须带明确标识 `DRILL-0.2.1.1-告警通路验证`，使手机通知与历史记录都能一眼识别为演练；这只改变失败素材，不缩短链路。
4. 临时配置与临时状态文件不得覆盖生产断言集或生产心跳；执行后删除临时件。
5. HTTP `202`、队列 `done`、发送日志只用于排障，**均不判通过**。
6. 只有业主以 `source_kind=user` 明确回复“收到”，本格才通过。若业主回复“没收到”，原样记失败；若未回复，记“待判”，不得写成成功或失败。

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

### 6. 守住：接线丢失必须自动转红

巡检集新增 `alert-route-stays-wired`：

- 从 supervisor 取得 workpulse PID，读取**运行中进程**的实际环境，而不是读取一份可能未生效的配置；
- 断言 `WP_NOTIFY_SOCKET`、`WP_ALERT_CHAT_ENTITY`、`WP_ALERT_BOT_CHANNEL`、`WP_ALERT_CHAT_TYPE` 四项均 set/nonempty；
- 用进程环境中的 socket 路径做 `stat`，断言该路径当前确为 Unix socket；
- 输出只含 `configured=N/4 socket_unix=true/false`，不输出任何值或路径。

部署时须先完成接线，再更新巡检集；否则新断言会正确地立即转红。删除该断言的外科变异必须只打红 `TestAlertRouteInvariantTest.test_runtime_guard_covers_all_keys_and_unix_socket_without_values`（实际 unittest 名以 `-v` 输出为准），还原后全绿，最终提交不得含变异。

同批还必须守住模板环境摘要：修改 `ENVIRONMENT_WORKPULSE` 的键集合而不同步 `t112-environment` 注释或 schema，模板校验测试必须转红。旧现网证据与目标模板之间的差异只能命中本次 workpulse 环境迁移；修改其它 program、字段或摘要仍须失败。

仓内变异实证（2026-09-19）：

- 靶点：只删除 JSON 中 `alert-route-stays-wired` 整项；变异前 `git diff --numstat` 显示巡检 JSON 仅为本次新增；
- 命令：`python3 -m unittest -v deploy.test_invariants_alert_route`；
- 结果：`rc=1`，唯一转红 `test_runtime_guard_covers_all_keys_and_unix_socket_without_values`，原因逐字为 `missing or duplicate alert-route-stays-wired`；
- 还原后同命令 `rc=0`；最终提交不含变异。

模板摘要变异实证（2026-09-19）：

- 靶点：只把 workpulse 模板注释的 `key_count=16` 改成 `15`，`git diff --numstat` 为该模板 `1 1`；
- 命令：`python3 -m unittest -v deploy.image.supervisord.test_verify_supervisor_template.RepositoryTemplateContractTest`；
- 结果：`rc=1`，目标测试转红，失败类为 `ENV_DIGEST_MISMATCH_SCHEMA_TEMPLATE`、`program=workpulse`；
- 还原后整组模板/巡检测试全绿，最终提交不含变异。

现网禁用态阳性对照（2026-09-19，prodops 只读执行；未安装新巡检集、未改配置、未重启）：

```text
configured=0/4 socket_unix=false
rc=1
```

这证明的不只是“配置形状测试会检查断言存在”，还证明该断言面对四项全 unset、socket 不存在的真实状态确实会转红，不是恒真断言。该状态在接线后消失，因此本证据须与上线后同一断言的 `configured=4/4 socket_unix=true`、`rc=0` 配对判读。

🔴 此时不得把新 JSON 单独部署到生产巡检集：在母本与接线尚未完成时，它会成为永久红项并掩盖其它新故障。断言与接线必须同批上线，验收看同一断言由上述红态翻转为绿态。

## 失败与回退

以下任一发生即停在原地报告，不改告警内容、不换收件人碰运气：

- socket 未创建或权限不是 `0600`；
- workpulse 自报版本/提交变化；
- runner 得不到预期 `202`，或阴格不是 `404`；
- 系统显示已发送但业主未确认收到；
- 配置没有经过仓内模板生成，而是只落当前容器。

回退只恢复上一版模板渲染结果并重启 workpulse；回退后复核四项状态与基线一致、socket 状态与基线一致、runner 仍运行。

## 可证伪判据

- 删除/清空任一目标配置后，启用态探针必须不再得到 `202`。
- 把 disabled 分支改成静默成功，阴格必须从预期 `404` 变红。
- 从巡检 JSON 删除 `alert-route-stays-wired`，配置形状测试必须唯一转红；否则“守住”本身没有守卫。
- 只改模板的 workpulse `key_count` 或 `keys_sorted_sha256` 而不改 schema，模板校验必须转红；只改旧证据到目标之间迁移声明以外的任一维，三方校验也必须转红。
- 绕过 runner 直接 POST 不能满足北极星，因为它没有证明真实失败能触发整条链路。
- 没有业主本人“收到”的回复时，本件不得宣称发布验收通过。

## 明确没有证明的事

- 本件不证明所有未来告警内容都正确，只证明既有巡检失败能到达业主。
- 本件不证明飞书长期可用，也不建立第二通知渠道。
- 本件不改二进制、不改版本号；版本与装配由组长统一处理。
- 本件尚未执行生产变更；代码在仓里不等于现网生效。生产动作等待组长出单与业主当次授权。
