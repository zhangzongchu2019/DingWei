# S-MCP-P1-SENDWAIT 开发交件

基线 LEN最终tip 5737f089a1e2ccafa4c978e9e90ce10fa9f2921b；分支 coder56solzzc/mcp-p1-sendwait。产品提交 f4de840d5（直父LEN最终tip）及完整身份不可得时拒绝回程修正4b6b792d38aea92271f55c120240f5d0fba9cd41；证据交件tip/tree由回执给出。只做r56.4 AG模式，不恢复S1、不改M3；无部署/重启/真实推理或现网身份写入。结果为开发自测，独立验收归组长。

## 结果

- MCP与REST同步提供send/recv/check/list/clear，旧三名兼容且ack不变；未发布check_messages。recv与旧Wait行为、25秒默认及租约确认机制保持。
- send可同步等待，服务端硬上限55秒（rulings补59及补60，最新046d691cdcf59d05c4d5efada7e3d884e980712f）；存在wait_reply_seconds时expect_reply默认true，显式false尊重。以受理receipt.CreatedAt为下界、AG当前目标ID为来源，只领取第一条可用新来件；received确认成功才随结果返回单条reply。超时保留受理字段，返回reply:null/timed_out:true/hint，MCP文本也含recv/check提示。重试沿用client_message_id、to/body/expect_reply不变，可改变等待窗口而不重发。
- 空唤醒和check以PG强制ReadOnly事务读取身份/组和候选，无FOR UPDATE/SHARE、无token续期、presence、TouchGroup或队列写；候选领取复用原短claim租约事务。入口鉴权一次；context取消立即结束。2.1秒空等待的PG触发器写计数为0。
- check只返回待收count和delivery_id/from/created_at/body_bytes摘要，queued与leased记录前后字节级相同。clear有协议/服务双确认门，只把本席已到达queued/leased改received，返回数量与ID；重调0，不改outbox、不续AG。
- 新短名分别映射原四scope，MVPScopes与PG校验字符串不改，缺映射拒绝；原令牌无需重新授权。
- 服务器对expect_reply投影追加对应中文提示，提示计入LEN的1MiB总正文预算。DW_MCP_EXTERNAL_AUTO_REPLY_DISABLED取接收方精确全名，缺省opus50tc2k.zzc、prodops.zzc；禁通配/客户端开关，非法配置拒绝。生成和出口双闸拒绝外部自动回程，显式全名回信仍允许。
- identity.FullName在属主查询失败时会退回裸名；新保护先拒绝不完整席位身份，避免精确关闭名单被这一失败路径绕过。原红灯与修复后结果保留。
- PG按Agent设置＋Console主人/管理员可改的方案已由补59认可，明确后续另单，本次不加迁移或Console写入口。LEN首次能力context文档随本证据补齐，不修改已交LEN源码和基线坐标。

## 时间预算与边界

cmd/workpulse/main.go public/internal HTTP Server只设ReadHeaderTimeout=5s，未设ReadTimeout/WriteTimeout；仓库deploy/nginx-dingwei.conf为proxy_read_timeout3600s。官方[Codex MCP文档](https://learn.chatgpt.com/docs/extend/mcp?surface=app)与[配置参考](https://learn.chatgpt.com/docs/config-file/config-reference)确认mcp_servers.<id>.tool_timeout_sec缺省60秒。补59正式将服务端等待定为55秒，留下受理/确认/响应预算，不提供扩大到60的入口；两个send名称的工具说明与schema都标明55秒。

本地HTTP探针实际运行55秒等待窗口：50秒客户端预算在50.05秒截止；60秒预算在55.07秒收到timed_out结果，均符合断言。此探针使用回环真实HTTP和模拟无回复服务，不冒称桌面客户端或现网端到端实测。先期60/75假设探针已被补59替代，旧超时类型匹配断言失败原文保留，未计通过。

现网代理只引用补59给出的prodops只读S-RO-NGINX-BODY回执f5785ab69a6e：443 location / client_max_body_size64m、proxy_read_timeout3600s，cmjrt-wildcard.conf SHA前缀bbcf55c4；50443外层不可核。本席未自行触碰现网或据仓库配置冒称已核实际配置。

## 验证

| 门 | 最终读数 |
|---|---|
| 全仓go test -p 2 -timeout 40m ./... | rc0；68包通过、13包无测试文件；5011 pass事件/0 fail/67条件skip（含父子用例，非叶子用例数） |
| mcpgw / extseat / extshared / tempgroup | 239/0/2skip；48/0；7/0；8/0 |
| cmd/workpulse | 259 pass/0 fail/6条件skip |
| m8 -p 1 | rc0，1517 pass/0 fail/37条件skip |
| canonical run-build-tests.sh | rc0；claude/codex各1132/1132，92文件，27 Python文件/2 shell |
| OpenSpec1.16.0 strict | 32 pass/0 fail |
| 指定＋补充变异 | 9/9非编译失败且命中对应标记 |
| 实际D＋A只读PG接线补充探针 | rc0，PRODUCTION_GROUP_READONLY_WIRING_OK |


最终门和全部计数、条件跳过、原始日志SHA见SUMMARY.json。要求命令：mcpgw/extseat/extshared/tempgroup与cmd/workpulse由全仓 `go test -json -count=1 -p 2 -timeout 40m ./...` 覆盖；另独立m8 `-p 1`、scripts/run-build-tests.sh双profile与OpenSpec1.16.0 strict。普通用户Go1.22.2、独立HOME、剥离继承SH_/DW_/DINGWEI_/DATAD_，测试临时PG严格模式。

九项变异：去提示、生成端开关失效、绕过出口开关、check误领取、去clear确认、缺scope仍放行、别名分叉、每次唤醒写PG、完整身份解析失败仍允许回程。全部要求非编译失败并命中对应标记。首轮生成端变异引起未使用变量/导入的编译红不计入，修正变异后重新命中MCP_EXTERNAL_REPLY_SWITCH，原文保留。最终九刀在4b6b792d3独立detached树上运行且逐刀恢复原字节；复核命令见mutations.py的--root/--out参数。

声明的条件跳过未当作通过；没有真实模型调用、生产更新、scope扩张或新账本编号。所有源码与报告经私有Git交付，无源码对象存储交换。

提交前再次fetch裁定面046d691cd，已核补60与当前代码一致：55秒硬上限，无扩大到60的环境配置。补59后续PG/Console方案不进入本分支产品代码。
