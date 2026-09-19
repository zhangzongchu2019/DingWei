# `0.2.1.4` · D 补：告警通路接线

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

### 第二轮只读核查与最终落点裁定

现网 `workpulse` 配置位于容器内 `/etc/supervisor/conf.d/dingwei.conf`。执行方确认：

- 该文件不在宿主 bind mount 中；
- `docker diff` 将它标为容器可写层修改；
- 可见挂载只有两份 secret 与 `/data`，均不是该配置；
- invariant-runner 配置同样是容器可写层新增文件，且未显式设置 `DW_NOTIFY_SOCKET`；
- 当前只读信息无法定位一份会在容器重建时恢复这些配置的持久母本。

后续仓内核查找到了 supervisor 模板：

- `deploy/image/supervisord/dingwei.conf.template` 是 supervisor 配置模板；
- `[program:workpulse]` 的 `environment=%(ENVIRONMENT_WORKPULSE)s` 是环境配置注入点；
- `supervisor-structure.schema.json` 与模板上的 `t112-environment` 注释共同约束键数和排序键名摘要，不记录键名或值。

但全仓只有 `ENVIRONMENT_WORKPULSE` 占位符，没有渲染器、输入清单或值来源。模板能约束键集合，不能让四项值在重建后自行出现。组长在 ts-No.38 据此撤回 ts-No.31 的模板持久化裁定，并按北极星重新划界：本版负责把告警**现在接通**；整容器可重建性归 `0.2.1.3`，不再阻塞本版。

`0.2.1.4` 现在的执行边界为：

1. prodops 把四项写入当前生效的 supervisor 配置，使告警现在接通；值不进仓、不回传；
2. 同时把同一组值写入宿主机 `/home/ai-dev/dingwei-gray/container/secrets/workpulse-runtime.env`，保持 `0600 ubuntu:ubuntu`；该文件保存恢复材料，不会自动接线；
3. `alert-route-stays-wired` 与接线同批上线，不得提前部署到禁用态制造永久红噪声；
4. 明确接受：容器重建会丢掉 supervisor 接线、runner 与巡检；值仍在宿主，可按本件重新接线；
5. 整容器可重建性转入 `0.2.1.3`，不冒充本版已解决，也不再阻塞本版。

prodops 按 T-112 原机械算法完成的 `current=12`、`current+4=16` 摘要是**现网实然证据，只供交叉核对**，不得写成模板期望。模板应然计数应为原 5 键加本版四项，即 `key_count=9`；但原证据只保留 count 与单向 digest，没有保留键名集合。c56-No.3 的只读结论为**查不出**：无法诚实计算 9 键的 expected digest，也未从现网 12 键反推。

当前实然 12 与模板原应然 5 已不相同；本版加键后的实然 16 也必然不同于应然 9。这是 T-112 首次实际抓到的环境键集合漂移。必须逐键归因后再渲染，不能把现状回填为期望使闸恒绿。

### T-112 漂移发现（不阻断本版）

全仓检索只找到模板占位符，没有仓内持久值来源。T-112 的实然/应然摘要已经抓到现网键集合漂移；原应有键名集合未保存，故 9 键应然 digest 当前**不可恢复**。该发现归配置漂移/可重建性后续，**不得**拿现网摘要回填模板，也不再作为本版告警接通的前置。

## 凭据与目标值边界

- 本件、提交、日志与消息里只记录四项配置“已设置/未设置”，**不得记录值**。
- `WP_ALERT_BOT_CHANNEL` 必须取自现网已知成功的同一飞书机器人通道；不能凭名字猜。
- `WP_ALERT_CHAT_ENTITY` 必须取自该通道下、经业主本人入站消息核实的个人实体；不得使用 Agent session name。
- `WP_ALERT_CHAT_TYPE=personal`；`WP_NOTIFY_SOCKET` 必须与 runner 实际请求路径一致。
- 四项值只允许进入当前生效 supervisor 配置与宿主恢复文件；不得进入仓库、消息、日志或执行记录。宿主恢复文件只记录“已写入、mode/owner 正确”，不得输出内容。

## 执行顺序（由组长出单、prodops 执行）

### 1. 基线与回退点

1. 记录当前 workpulse `version/commit`、四项配置的 set/unset 状态、runner 状态；不输出值。
2. 备份当前实际 supervisor 配置，记录备份路径与文件摘要；备份内容不得进仓或消息。
3. 只读确认宿主恢复文件已存在、属主 `ubuntu:ubuntu`、权限 `0600`；不得读取或回传值。
4. 确认回退动作是恢复上一版 supervisor 配置并重启 workpulse，且不会替换二进制；宿主恢复文件保留，不作为回退删除对象。

### 2. 接通

1. 将四项写入宿主恢复文件，原子替换后复核 `0600 ubuntu:ubuntu`；不输出内容。
2. 将同一四项写入当前生效的 supervisor 配置，其中 socket 路径须与 runner 一致。
3. 只重启 workpulse；不改二进制，不重启其它服务。
4. 只读确认：workpulse 仍自报原 `version/commit`；socket 存在且为 Unix socket、权限 `0600`；四项均 `set/nonempty`（不输出值）。

### 3. 北极星：真实失败

1. **执行前先通知组长，由组长提前告知业主下一条是演练；未得到组长确认不得触发。**
2. 用临时断言配置制造一条**真实、可控、可恢复**的巡检失败，并以 `invariant-runner.sh once` 走生产同一条 runner → Unix socket → outbound → 飞书链路。不得用直接 POST 冒充真实失败。
3. 临时断言的 ID、描述以及最终告警正文必须带明确标识 `DRILL-0.2.1.4-告警通路验证`，使手机通知与历史记录都能一眼识别为演练；这只改变失败素材，不缩短链路。
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

1. 从宿主恢复文件恢复四项目标配置并只重启 workpulse；不得把值打印到终端或日志。
2. 确认 socket 重新为 `0600`、四项配置均为 `set/nonempty`、runner 仍为 RUNNING。
3. 不再制造第三条告警；以配置形状和 socket 状态确认最终启用，避免给业主重复发验收消息。

### 6. 守住：接线丢失必须自动转红

巡检集新增 `alert-route-stays-wired`：

- 从 supervisor 取得 workpulse PID，读取**运行中进程**的实际环境，而不是读取一份可能未生效的配置；
- 断言 `WP_NOTIFY_SOCKET`、`WP_ALERT_CHAT_ENTITY`、`WP_ALERT_BOT_CHANNEL`、`WP_ALERT_CHAT_TYPE` 四项均 set/nonempty；
- 用进程环境中的 socket 路径做 `stat`，断言该路径当前确为 Unix socket；
- 输出只含 `configured=N/4 socket_unix=true/false`，不输出任何值或路径。

部署时须先完成接线，再更新巡检集；否则新断言会正确地立即转红。删除该断言的外科变异必须只打红 `TestAlertRouteInvariantTest.test_runtime_guard_covers_all_keys_and_unix_socket_without_values`（实际 unittest 名以 `-v` 输出为准），还原后全绿，最终提交不得含变异。

模板环境摘要的机械闸仍保留，但本版不把现网 12/16 摘要焊成应然标准。其漂移归因与模板应然迁移另行闭合，不影响运行时守护同批上线。

仓内变异实证（2026-09-19）：

- 靶点：只删除 JSON 中 `alert-route-stays-wired` 整项；变异前 `git diff --numstat` 显示巡检 JSON 仅为本次新增；
- 命令：`python3 -m unittest -v deploy.test_invariants_alert_route`；
- 结果：`rc=1`，唯一转红 `test_runtime_guard_covers_all_keys_and_unix_socket_without_values`，原因逐字为 `missing or duplicate alert-route-stays-wired`；
- 还原后同命令 `rc=0`；最终提交不含变异。

模板摘要变异实证（2026-09-19；该发只证明模板注释受 schema 约束，不为后来撤回的 16 键目标背书）：

- 靶点：只改 workpulse 模板注释的 `key_count`，`git diff --numstat` 为该模板 `1 1`；
- 命令：`python3 -m unittest -v deploy.image.supervisord.test_verify_supervisor_template.RepositoryTemplateContractTest`；
- 结果：`rc=1`，目标测试转红，失败类为 `ENV_DIGEST_MISMATCH_SCHEMA_TEMPLATE`、`program=workpulse`；
- 还原后整组模板/巡检测试全绿，最终提交不含变异。

现网禁用态阳性对照（2026-09-19，prodops 只读执行；未安装新巡检集、未改配置、未重启）：

```text
configured=0/4 socket_unix=false
rc=1
```

这证明的不只是“配置形状测试会检查断言存在”，还证明该断言面对四项全 unset、socket 不存在的真实状态确实会转红，不是恒真断言。该状态在接线后消失，因此本证据须与上线后同一断言的 `configured=4/4 socket_unix=true`、`rc=0` 配对判读。

🔴 此时不得把新 JSON 单独部署到生产巡检集：在接线尚未完成时，它会成为永久红项并掩盖其它新故障。断言与接线必须同批上线，验收看同一断言由上述红态翻转为绿态。

## 失败与回退

以下任一发生即停在原地报告，不改告警内容、不换收件人碰运气：

- socket 未创建或权限不是 `0600`；
- workpulse 自报版本/提交变化；
- runner 得不到预期 `202`，或阴格不是 `404`；
- 系统显示已发送但业主未确认收到；
- 宿主恢复文件不存在、权限/属主不符，或其写入与 supervisor 接线不是同一批动作。

回退只恢复上一版 supervisor 配置并重启 workpulse；回退后复核四项状态与基线一致、socket 状态与基线一致、runner 仍运行。宿主恢复文件保留，供重新接线使用；不得因回退删除唯一的值副本。

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
- **本版不解决“容器重建后仍然接通”。** 重建会丢掉接线，也会丢掉 runner 配置与巡检；但四项值已在宿主恢复文件留存，可按本件重新接线。该缺口归 `0.2.1.3`，已登记。
- 本件尚未执行生产变更；代码在仓里不等于现网生效。生产动作等待组长出单与业主当次授权。
