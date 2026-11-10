## Context

SENDWAIT基于LEN最终tip，承接AG模式，不恢复暂停的S1。旧WaitMax=25秒、旧工具默认25秒均保持。

## Decisions

- 同一client_message_id沿用Send幂等键，等待窗口不入发送hash；expect_reply沿用原hash防选择漂移。等待以receipt.CreatedAt作下界，只领取本AG当前目标的新来件；确认成功后才返回正文，超时保留accepted及reply:null/timed_out:true/hint。
- 协议入口Authenticate只做一次。空唤醒以PG强制ReadOnly事务读取token/身份/组与候选，既无FOR UPDATE/SHARE也无续期、presence、TouchGroup；有候选才调用原短事务claim并做received。客户端context取消中止轮询。
- check用同一强制ReadOnly路径，返回delivery_id/from/created_at/body_bytes与count，包含queued与leased，不释放租约。clear有协议与服务双确认门，只将本席queued/leased改received，不发消息、不改outbox、不续AG；重复返回0。
- 短名通过requiredScope显式映射并验证其属于固定MVPScopes，未映射拒绝；不引入新scope与重新授权。
- 外部期待回程提示由ExternalMessagePrefix服务器函数生成，LEN预算同步计入此提示。服务端DW_MCP_EXTERNAL_AUTO_REPLY_DISABLED按精确全名配置，缺省opus50tc2k.zzc、prodops.zzc；显式手动全名发送允许。回程生成和routeExternalSeat出口都检查，客户端meta不作为允许开关。
- 后续PG/Console存储方案：按Agent布尔开关，主人/管理员可设，敏感名单默认关闭其他开启；仅方案，本次不实施迁移。

## Timeout evidence

cmd/workpulse/main.go public/internal Server只设ReadHeaderTimeout=5s，ReadTimeout/WriteTimeout均未设置；deploy/nginx-dingwei.conf三处proxy_read_timeout3600s。官方[Codex MCP文档](https://learn.chatgpt.com/docs/extend/mcp?surface=app)及[配置参考](https://learn.chatgpt.com/docs/config-file/config-reference)确认mcp_servers.<id>.tool_timeout_sec默认60秒、可按服务器覆盖。故留出受理和响应预算，补59正式定服务端硬上限55秒，不提供放大到60的入口。预算依据是官方默认超时及受理/响应开销；本地50/60秒HTTP客户端下的55秒窗口探针结果见RESULT，不冒称桌面实测。补59给出prodops现网只读回执f5785ab69a6e：443 location / 的client_max_body_size64m、proxy_read_timeout3600s，cmjrt-wildcard.conf SHA前缀bbcf55c4；50443外层不可核。
