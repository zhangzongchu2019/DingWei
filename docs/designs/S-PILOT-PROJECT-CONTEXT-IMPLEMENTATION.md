# project_context 实现说明

业主第 85 条（rulings `4dbd95ed`，2026-10-20）批准按设计 `5b95f5fb` 实现。本件基 r32.1 `36afa621`，候选功能版本统一为 0.2.3.7；没有部署或席位升级。原设计保留，原稿“待审/只设计”的历史措辞不代表当前实现授权状态。

实现入口：`GET /api/agent/console/project-context`，只认单一专属 Agent bearer、拒 cookie/混用/重复头与身份选择器。identity 的 `WithProjectContextAuthorization` 按组→Agent→用户持锁，PG 普通事务允许 FOR SHARE；当前 Agent 实际编组及其中成员分 101 项 keyset 页取得，不读全平台身份表。锁定实际组全集、对应成员与用户，让同页任务和名册共用一段授权边界。候选任务也在仓储层最多 101 项一批；SQLite 单个只读事务内重核候选角色、版本、spec/revision/SHA，并投影精确任务引用的 criteria 和案例确认。没有调用失权清理、已读、通知或业务 audit 写方法。

不透明游标用 Hub 实例随机内存密钥做 AES-GCM 认证加密（比仅签名更严格，避免暴露内部候选 ID），绑定 actor/scope/limit/5 分钟期限/授权摘要及两个独立 keyset 位置。Hub 重启后旧游标不能解密，返回 400，由客户端从首页重取。授权摘要由当前组、成员、存活用户与身份、任务版本/归属、NoDirectory 可见性组成；逐页重核，变化返回 409。在线状态只是一次缓存采样，不纳入授权集合的稳定性承诺；在线连接还必须绑定相同的专属 Agent ID，不能借同名旧连接或旧账号连接冒充。

每页响应不超过 256KiB；按完整任务（连同 config 中对应 spec 摘要）或在线成员边界缩页，游标不会跳过被缩掉的数据；单项过大返回 413。config.specs 中带 task_id 明确对应关系，不返回 spec 全文、未关联 criteria、分享能力、入组码、IP/主机或邮箱。

helper 的 `project_context_v1` 固定路由、单次 HTTP（10 秒），IPC 15 秒、shim 20 秒。旧 datad 四键如存在只按键名一次提示，不读取其值用于新路径；三个历史 context URL 在转发到旧后端之前拒绝。旧 Hub/旧 helper 明确 unsupported/升级提示，受限 upload_only/send_body_only 模式不开放该 IPC。其他旧 datad 工具继续原行为，旧 handler 延迟导入可选 aiohttp，不让历史配置使新只读工具无法启动。

没有 schema 或迁移变更。旧二进制读原库兼容；回滚后新 context 能力消失，新 shim 明确 unsupported；不能把旧同名工具误当已恢复。运行时依赖锁原字节不变。本件不迁 MCP 配置、不启用托管、不发送 PM/飞书业务消息。
