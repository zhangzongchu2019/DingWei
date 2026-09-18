# 迭代 3：文件能力身份模型 A

状态：实现中；未部署。

## 安全性质

A 提供的是「共享 key 下的会话级短时授权」。它**不能**识别请求方是否为官方 sessionHelper；持有同一 owner API secret 的任何客户端都可取得同样的能力。

能力只能由 Hub 在已经通过 API secret 认证的 WSS 连接上签发。Hub 从当前 `sessionClient` 取得 owner、key、session 与连接序号，忽略请求正文自报的身份。这里的 WSS 认证仍是共享 key 认证，不得提升表述为设备身份或官方 helper 证明。

## Bearer 边界

- token 是单对象、单方法、固定 Range、短 TTL 的 opaque bearer；HEAD 与 GET 分开签发。
- 原始 token 只在本机 helper 与专用 CLI 的进程内或受限 IPC 中传递，使用 HTTP `Authorization` header。
- token 不得进入 LLM 上下文、消息正文、命令行参数、URL/query、日志、transcript、collect、镜像或飞书同步。LLM 只看到 `file_ref`、公开元数据和实际返回的字节。
- 服务端仅保存 token 的 SHA-256，不保存或记录原始 token。
- capability 被复制到另一主机后，在 TTL 内仍能成功使用。这是模型 A 的预期性质，不是缺陷；验收必须保留该阳性用例，避免误报更高安全等级。
- token 过期、撤销、对象逐出或 ACL 改变后立即拒绝。拒绝响应不得泄露对象是否存在。

## 单位

所有产品边界使用二进制单位：`1 KiB = 2^10`、`1 MiB = 2^20`、`1 GiB = 2^30`。默认值为长文本阈值 4 KiB、长文本上限 8 MiB、单文件上限 16 MiB、总配额 500 GiB；运行时限制仍可配置为测试小值。
