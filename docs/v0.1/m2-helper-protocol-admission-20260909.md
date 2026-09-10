# M2 sessionHelper 协议准入

状态：已实现，禁止部署；上线硬门尚未满足。

## 定位

M2 的目标是先拦截当前 `send.py` 一类“建连即发内容”的一次性客户端，提高伪造门槛。它不是身份证明：当前 API secret 可由同一 owner 多席共享，持有 secret 的客户端能够仿造 `session_capabilities` JSON。

## 开关

`WP_AGENT_HANDSHAKE_ENFORCE=on` 同时启用：

1. WSS 连接发送 `content`、`agent_route` 或空 `meta.type` 信封前，必须先发送 `session_capabilities`。
2. WSS 握手请求的会话名必须通过既有 `sessionNameRequestWarning` 规范；不合规直接返回 400。

默认 `off`。在至少两席具备并实测持久身份出站能力前，禁止在环境中打开。

## 不受影响的路径

- Hub/workpulse 内部信封不经过 session WSS 入站门。
- view 页输入从 View Service 到 Hub 的签名内部端点进入。
- 飞书桥入站不经过 session WSS 入站门。
- session 的 `session_capabilities`、delivery ACK、session event、terminal output 等协议控制信封不属于 content/agent_route 门。
- producer 也是 sessionHelper；本实现让 producer 在发送业务内容前同样先发 capabilities。

## 安全边界

- `helperReady` 只绑定当前内存 `sessionClient`，重连和 `connSeq` 变化后必须重新握手。
- Hub 从当前连接确定 key/session，不信任 capabilities 正文自报身份。
- 该门不抵抗“持共享 API secret 并模仿官方 capabilities”的客户端。若威胁模型要求抵抗此类客户端，必须另行引入设备密钥和 challenge/PoP，不得把 M2 结论升级为可信身份。

## 验收

1. 未发 capabilities 的连接发内容：收到明确 error，目标零投递。
2. 同一连接发 capabilities 后再发内容：正常投递。
3. `content`、空 type、`agent_route` 均受门控；协议控制类型不受影响。
4. 非法会话名在 M2 开关启用时握手失败。
5. producer 首帧包含 capabilities，随后业务输出不被误伤。
6. 真机 `send.py` 负测只能在上线硬门满足并取得部署授权后执行。
