# R53 MCP MVP 接口约定（组长；集成分支 opus50tc2k/r53-integration）

提案：openspec/changes/agent-http-mcp-gateway（dc9684ad，业主 ㊹-补8 通过）。裁定：review/opus50tc2k/rulings 的 ㊹、㊹-补1～补8、㊺。共享类型：internal/extshared（本分支 bbd71ce3）。

## 迁移编号（组长分配，各路不得自取）
- PG：0105 归 A 路（外部主体、外部席位、令牌、外部收件箱、outbox）；0106 归 D 路（临时编组 AG：序号、成员、续期、解散）。
- SQLite：本版原则上不加；A 路若投影确需扩列，先报组长取号。

## 0. 分路与单写者（每路一个分支，只改自己的包；跨路改动先报组长）
| 路 | 执行者 | 分支 | 拥有的包／文件（暂定，定稿时按调研坐标修正） |
|---|---|---|---|
| A 外部席位核心 | coder56solzzc（执行者 01a19ca0） | coder56solzzc/r53-extseat | internal/extseat/（席位、令牌、租约、滑动续期）；外部席位的投递队列适配（复用 session_delivery） |
| B 协议层 | togpt60lunazzc（时间线路） | togpt60lunazzc/r53-mcp | internal/mcpgw/（2026-07-28 无状态 Streamable HTTP：server/discover、四工具）＋ REST；只调用 A、C 的接口 |
| C 飞书接入 | k31t2075 | k31t2075/r53-feishu-login | internal/feishulogin/（扫码回调、tenant 校验、显示名与拼音、内部键）；签发令牌只调 A |
| D 临时编组 | qwen38mvb4 | qwen38mvb4/r53-tempgroup | internal/tempgroup/（AG 序号、成员、3 天滑动、解散与通知）；Console 侧栏单列与编组页 |
| 集成 | 组长 | opus50tc2k/r53-integration | 迁移编号分配、main 装配、版本位、集成门 |

## 1. 共享数据（迁移编号由组长统一分配，各路不得自取）
- 外部席位：seat_id（内部）、agent_name（短名_AGn）、feishu_open_id、feishu_union_id、tenant_key、display_name、created_at、revoked_at。〔A 拥有〕
- 令牌：token_hash（不可逆）、seat_id、expires_at（滑动 14 天）、last_used_at（续期写库节流 1 分钟）、revoked_at。〔A〕
- 临时编组：group_name（AGn）、seq（全局唯一、持久）、created_by（feishu union_id）、created_at、last_used_at、expires_at（滑动 3 天）、dissolved_at、dissolve_reason。成员表：group_name、member_key、role。〔D〕
- 序号：独立计数表或自增主键，重启不回退、不复用。〔D〕

## 2. 跨路接口（Go 签名草案，定稿时可改名，不改语义）
- C → A：IssueToken(ctx, ident FeishuIdentity, shortName string, targetAgent string) (token string, seat Seat, group string, err)
  - A 内部调 D：CreateTempGroup(ctx, creator FeishuIdentity, external Seat, target AgentRef) (group string, err)
- B → A：Authenticate(ctx, bearer string) (Seat, err)   // 成功即按节流续期
- B → A：ListOnline(ctx, Seat)、Send(ctx, Seat, to string, body string)、Wait(ctx, Seat, max time.Duration) ([]Delivery, err)、Ack(ctx, Seat, deliveryID string, gen int) error
- A → D：TouchGroup(ctx, group string, at time.Time)   // 仅消息往来调用，轮询不调
- D → Hub：Dissolve(ctx, group, reason) 通知两侧、在途消息按 A 的过期规则处理
- Console（D）→ 后端：GET 相关 AG 列表（只列当前登录者名下 Agent 所在 AG）；GET 编组详情；POST 解散（主人或平台管理员，二次确认，审计）

## 3. 不变量（集成门逐条有用例）
1. 名单为空、无外部席位时，现有 helper 席位、send.py、Console 行为逐字节不变。
2. 外部席位只有四种通讯能力；任何执行、文件、宿主、升级接口对其拒绝。
3. 入库的文件链接、消息正文不因外部席位改写。
4. 吊销优先于续期；过期后须重新扫码。
5. 编组外不能通讯：外部席位只能寻址其 AG 内成员的显式全名。
6. 投递以 ACK 为准，未确认可重投；断流不丢消息。

## 4. 门（每路交件都要）
run-build-tests（短 TMPDIR）rc0；本路包与 m8／store 整包；race -count=3（分片、-timeout 90m）；涉及前端的浏览器回归＋变异；OpenSpec strict。

## 5. 组长定案（2026-11-06T04:2xZ，据 coder56 坐标调研 review/coder56solzzc/extseat-coord-survey-20261106 cdb264bf 第 6.4 节五问）
1. 身份：扩展 np_agent 增受控 external 类型（origin 值与名称校验分支，普通 helper 规则不放宽）＋ PG 侧表 np_extseat／np_extseat_token／np_external_principal；不另建独立实体。全名解析、编组成员、权限沿用同一身份源。
2. ACK：MVP 外部席位只有一个确认阶段「received」（客户端确认已收到）；不做注入／处理中终态，不冒充 driver。未确认消息自入队起 24 小时过期，标 dead 并给可查原因。
3. 跨库权威：外部席位收件箱（入队、领取、确认、过期）权威在 PG，与令牌、AG 同库；Send 的「受理＋TouchGroup＋入队」同一 PG 事务。外部→helper 方向经 PG outbox 幂等投影到 SQLite session_delivery（受理≠送达，返回 accepted）；helper→外部方向在 Hub 路由处写 PG 收件箱并 TouchGroup（同事务）。SQLite 现有 30 分钟／12 次策略不改、不套用到外部目标。
4. 续期：Authenticate 每次成功交互以 CAS 持久续到 now＋14 天（单行条件更新，无额外 touch 表）；Recheck 只核不续；presence 持久 last_seen（90 秒判离线）；AG 只由「受理成功的发送」与「首次有效 received 确认」续 3 天。
5. 共享类型：target／AG／principal／receipt／错误码由组长在集成分支 internal/extshared（暂名）一处定义并先推，各路只引用；跨路不自建表、状态或错误含义。
