# R62 外部 MCP Agent 同权化切片计划

基线：main `206d41b26dab646c0e50a111eed5a45ccf7c72f6`。来源：业主 rulings ㊿-补129／㊿-补131，rulings tip `86e135328`。这是一份设计与估算计划，不是实现授权。

2026-11-19设计修订：以㊿-补132③（rulings `148f1be7`）覆盖原“入普通组即解散AG”和“内部默认开”前提。对应 S-R62-EXTSEAT-AMEND，设计基 `124d24ee`；仅改设计，不改已冻结TTL `3639406c` 或产品。

## 交付口径

| 顺序 | 切片与范围 | 迁移 | 回退 | 粗估 | 建议独立复核 |
|---|---|---|---|---:|---|
| 1 | 存活期：临时 AG 3→7 天，外部消息 24 小时→7 天，token 保持 14 天；边界与续期测试 | 无；改常量和测试 | 回退常量，只影响新建/续期，不缩短已写入的 expiry | 1–3 人日 | qwen38mvb4 |
| 2 | Unicode 展示名：飞书原名作为显示属性；保留 ASCII 地址；列表、成员卡和 mention picker 显示无歧义全名 | 需要；计划槽 0112，为 `np_agent` 加可空 `display_name`，回填失败时按 canonical address 显示 | 关闭新显示字段使用，显示 ASCII 地址；保留字段和数据 | 3–7 人日 | coder56 实施，qwen38mvb4 独立审身份和路由 |
| 3 | 普通组与保留临时配对并存；新增不建配对开户、未编组拒绝/显示；授权 owner/admin 入多个 `np_group`，全入口统一授权；跨租户独立准入/tenant label；统一处理PM01在途件 | 需要；0113仍为计划槽：可选AG关联、开户与组授权解耦、授权路径及tenant registry；不再计划转换标记/强制解散；复用现有两类组 | 可停新开户/成员变更，保留已有普通组与有效配对；无AG席位须先验证旧构建兼容，不能造AG回退 | 原6–12人日需重估：无AG地址/接口兼容与PM01未定，不能将原区间当新增工作已覆盖 | qwen38mvb4 与 opus50tc2k 审并存授权、无组拒绝、过期镜像与在途件 |
| 4 | 摘要抄送：内部默认关、外部默认开，显式偏好优先；每Agent开关、严格命令、verified映射、耐久outbox与回环抑制 | 需要；计划槽0114，偏好表与通用摘要outbox；保留旧extseat队列至排空；不迁改用户显式选择 | 停新copy，保留偏好/队列；不得借回退强开内部或覆盖外部opt-out；已发消息不可回收 | 原6–13人日，新增缺省/迁移矩阵需实现前核估 | qwen38mvb4 审隐私/授权，另请飞书集成席审API |
| 5 | Hub 侧每个外部 Agent 最多保留 500 条消息，兼顾 7 天 TTL、终态清理、幂等 tombstone 与显式容量拒绝 | 大概率需要；计划槽 0115 为按席计数/索引；若事务级锁＋现有表可证明并发上界，可免新增表 | 关闭清理器、不回收更多历史；已删正文不可恢复 | 4–9 人日 | qwen38mvb4 独立审 PG 并发、幂等与容量边界 |
| 6 | MCP upload feasibility：比较 Hub 文件票、MCP multipart/stream、飞书文件资源的权限/大小/存储/分享路径，仅报告 | 无 | 无代码上线 | 2–5 人日 | prodops 或负责 MCP/文件服务的独立席 |

原估算总计22–49人日保留作历史基线，不是本次新增可选AG/兼容/PM01工作后的重新报价。切片三须在接口和在途语义定后重估；现仍未包含上线、飞书权限、跨租户审批和安全/性能专项。切片一已有独立冻结件，本设计不改变其状态；切片二是无歧义显示基础；切片三先于面向普通组成员的完整抄送；切片五依赖终态与幂等策略冻结。

## 必须冻结的关系与生命周期

### 临时 AG 与普通编组

采用㊿-补132的并存语义：加入普通组只增加membership，保留临时AG、原稳定目标及期限，不因入/离普通组续AG。无显式组直发在身份/租户前置检查通过后，共享真实普通组或有效原配对任一成立即可；两路均有效时普通组优先，同类按稳定组引用固定序选择并记录。临时配对仅两名原成员，不扩展到目标的其它组员；临时AG可达性镜像不得独立当成普通组放行。显式组请求只核所选组，不以其它授权回退。AG与普通组编码/模型不混用。

外部Agent可以加入多个普通组。离开一个/最后一个普通组只撤其membership；仍有效的原配对仅对原目标继续可用，失效AG不重建。AG到期只停配对，不删真实普通组关系。token到期停MCP鉴权但保留身份和组行，重新认证不续消息或重开AG。所有API、MCP、inbox/outbox投影与AMR走同一判断。原指定目标开户仍保留兼容路径，不扩外部席自助邀请/入组权限。

### 不建临时配对的入口

验证/准入后可省略目标、不创建AG，仍分配稳定身份。未加入真实普通组前，通信工具按既有HTTP403/协议1003、reason `not_permitted`拒绝，无正文/他席列表/消息/租约写入，不续组；工具发现不是通信授权。Console显示“已验证／未编组”“临时配对未创建”“目标—”。管理员入普通组后按该组授权，不强制补建AG。无AG新地址编码与旧AG-only入口兼容在切片三技术设计冻结；不得造AG0或猜目标，也不改旧席地址。

### PM01：授权变化后的已投影件

现有未投影层重核组，但SQLite已投影件可能在原目标上线后继续发送；本单不改变或宣称已修。并存后，AG到期并不撤普通组授权，普通组移除也不撤有效原配对；普通组消息不续AG，故AG先到期不应误伤普通组路径。已绑定某组的旧件不能偷换另一组/目标来续命。绑定授权失效后是否撤回、如何重授权与通知，归切片三内外一起定。七天消息TTL扩大在途暴露窗仍登记在案；当前只补未投影失权拒绝无正文的安全格，不以正向测试固化旧已投影行为。

### 令牌与临时组到期

令牌仍为 14 天滑动到期；到期后 MCP 鉴权停止，Agent 与普通组成员关系保留。原飞书主体重新验证后恢复同一 Agent ID，不创建第二身份。临时 AG 7 天到期只关闭 bootstrap 收发，不吊销 Agent，也不删除普通组。普通 `np_group` 沿用内部组现有生命周期，不被外部 AG expiry 连带删除。逐条消息从受理时间起 7 天到期；令牌/组续期不延长消息。

## 名称、提及和跨租户展示

地址、key、外部 principal 五元组继续作为权威唯一身份。Unicode `display_name` 可重复、可变，只用于展示；飞书部门信息不取。AMR 暂不支持 Unicode 显示名匹配，避免规范化/同形字、重名或解析规则引入误投。列表可以显示 `Unicode 名称 · canonical full name`；mention picker 以不变 Agent ID 选择并把 canonical ASCII 地址送入现有协议。纯文本中仍要求 canonical full name。

跨租户 external principal 只有在其 tenant 已通过明确的准入策略后才能被管理员加入普通组。列表显示管理员维护的 tenant label、Unicode 名和 canonical address；不要直接暴露原始 `tenant_key`，也不公开 open_id、union_id、issuer 或令牌。相同租户名/人名冲突时 canonical address 是最终消歧项。当前 `IssueToken` 的单租户拒绝逻辑不能被「可编组」静默移除；多租户准入和标签来源需独立评审。

## Feishu 抄送切片说明

内部Agent缺省关、外部Agent缺省开；已有显式on/off优先，不覆盖用户选择。唯一verified映射是发送必要条件，不等于内部已开启；内部须显式on才发。环境变量只能作rollout gate，不得强开内部默认或覆盖opt-out。外部收件人为其验证主体；内部沿np_agent.owner_lc→np_user→活跃np_identity查映射，无绑定不发、多绑定先选primary、不猜。

命令只接受可信飞书 sender identity，使用严格固定语法与 canonical Agent selector。一个 owner 只有一个可控 Agent 时可省略 selector；有多个则要求点名，歧义时不改设置。摘要为确定性元数据＋短 excerpt＋受鉴权的消息引用，不存/发完整正文；消息回调不重新进入 MCP。内部与外部双向消息走同一 copy policy 和耐久队列，主投递不依赖飞书可用性。

## 500 条历史和回收

500 条是每个 external Agent 在 Hub PG 权威历史中的收件与发件合计上限；外部对外部一条消息分别占用每个参与席位的一条。保留普通终态消息供已认证 external MCP 客户端读取；不把 helper SQLite、View 历史库或飞书 copy 计入此上限。

受理新消息时按席位串行化：先把过期 pending 变为 expired，再在同一事务内计算保留条目；达到 500 时只回收最早的终态记录。queued/leased/in-flight 不可回收；若 500 条均仍活动，则返回现有可见 queue_full 错误。客户端 message ID 的幂等 tombstone 保留至少 7 天，不能随正文条目提前删除。回收应批量且有索引/指标，避免每次请求做全表扫描。

## 文件上传可行性调查

当前能力有三段不连续：MCP gateway `tools/list` 不提供 `upload_file` 且测试固定断言 404；Hub File Capability 可给已认证 helper WS 的在线席位签发短期上传票；外部 MCP scope 仅四项通信权限，并没有上传 scope。直接复用 helper ticket 会要求外部席位冒充在线 CLI session，不符合当前身份绑定。

候选路径：

1. **专用 MCP `upload_file` 工具**：对传输层做受限流式 multipart/分片，上传票绑定 external Agent ID、目标 conversation/group、大小和到期时间。优点是 MCP 体验直接；缺点是要设计新 scope、资源配额、并发、内容扫描、断点/幂等和授权组绑定。
2. **Hub 文件服务短期 capability**：MCP 先通过认证 Hub 取得一次性票，再直传文件服务。复用现有文件服务的存储/票据，但必须把票绑定 MCP principal、group ACL 与最大字节，并避免把 token 放入 query/log。
3. **飞书文件 API 中转**：官方 IM 文件上传接口可取得 file_key，再把文件引用投递到 Agent。此路径依赖飞书 tenant token、应用权限、文件生命周期和跨租户下载权限；仅得到 file_key 不等于目标 Agent 有权下载。

推荐下一步只做独立 POC 规格：先确认文件服务是否支持 external principal/Agent ACL 与短期下载票，再比较方案 1/2。必须先定单文件大小、总配额、允许 MIME/扩展名、恶意内容检测、保留/删除期限、跨租户分享、下载审计、拒绝/过期 UX。当前阶段不改 gateway tool list、scope、Hub endpoint、file-service 或飞书应用权限。

飞书官方接口资料（供可行性后续验证，不构成已获权限或上线承诺）：

- [IM v1 API 概览：上传文件与下载文件](https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/im-v1)；页面说明上传走 `POST /open-apis/im/v1/files` 并取得 file_key。
- [飞书消息内容：file_key 引用格式](https://open.feishu.cn/document/server-docs/im-v1/message-content-description/create_json?lang=zh-CN)。
