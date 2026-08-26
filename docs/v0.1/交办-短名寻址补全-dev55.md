# 交办 dev55：短名寻址补全（会话间 + 飞书都支持短名，撞名才提示）

- 派发：manager（zzc-manager-2642），2026-07-12
- 负责实现：dev55（codex）；评审 + 部署：manager（同前，禁碰生产）
- 优先级：P1（次于①可靠投递忙时修复；建议①做完再做本任务，两者都动 hub.go，分开 commit）
- 提交身份：public GitHub `zhangzongchu2019 <tchangshark@outlook.com>`，无 claude 署名

## 需求（张宗楚 2026-07-12，两次提出）

发信方（无论飞书用户还是会话）寻址目标会话时，**应能只写短名**（如 `manager`/`dev55`/`developer`），后台在**发信方自己 owner 名下的在线会话**里把短名补全为注册全名（如 `zzc-manager-2642`）。**只有出现同名短名撞名时，才提示发信方补全名指定。** 现在必须打全名，太麻烦。

两个方向都要修（同一根因）：
1. **飞书用户 → 自己的会话**：飞书发 `#manager 内容`，现在因 `manager != zzc-manager-2642` 匹配失败。
2. **会话 → 会话**：如 ts3pocops ↔ dev55，`#dev55`/`#developer` 短名当前解析不到注册全名，报"目标会话不在你的账号下或不在线"。

## 现状根因（已定位，勿重复排查）

- 注册名由 `deriveRegisteredSessionName(requested, keyID, ownerKey)`（hub.go:989）服务端派生为 `<owner>-<短名>-<key末4>`，握手时用；但**寻址侧没做反向短名匹配**，全用精确名。
- 会话→会话：`routeSessionSelectorEnvelope`（hub.go:2516）把 `parseSelector` 出的短名直接 `sessionAddress(sessionName, c.keyID)`，`RouteEnvelope`→`sessionOnline(keyID, "manager")` 查不到（注册名是全名）。
- 飞书→会话 / 跨 key：`resolveOwnerSessionKey`（hub.go:2272）用精确 `sessionName` 在 `sessionClients[*][sessionName]` 找，短名找不到。
- 现成可复用：`onlineSessionsForOwner(ctx, ownerKey)`（hub.go:1583）返回该 owner 全部在线会话（含注册全名 `sessionName` + keyID）；`sessionNamePattern` 可从全名 `strings.Split(name,"-")[1]` 取短名（参考 deriveRegisteredSessionName 里的写法）。

## 实现要求

新增一个**短名→全名解析**辅助（scoped 到发信方 owner 的在线会话），语义：

```
resolveOwnerSessionByShort(ctx, ownerKey, requested) -> (fullName, keyID, status)
  status ∈ { unique-hit, no-match, ambiguous }
```

规则：
1. 先按**精确全名**匹配（`requested` 本身就是注册全名 → 直接命中，兼容老用法不破坏）。
2. 否则把 owner 在线会话逐个提取短名，与 `requested` 比较：
   - 恰好 1 个匹配 → 命中，返回其全名 + keyID。
   - 0 个匹配 → no-match（回原来的"不在线/不在你账号下"错误）。
   - ≥2 个匹配（**撞名**）→ ambiguous，**回提示**让发信方用全名指定，提示里列出候选全名（如"存在多个 dev55：zzc-dev55-2642、zzc-dev55-9xxx，请用全名"）。

接入两处：
- **会话→会话**：`routeSessionSelectorEnvelope`（hub.go:2516，`#` 分支）解析出短名后，先用发信方 `c` 的 owner 做短名解析，命中则 `env.To = sessionAddress(fullName, keyID)`；ambiguous/no-match 回 `errorEnvelope` 给发信会话。
- **飞书→会话**：`RouteEnvelope`/`resolveOwnerSessionKey` 调用点（hub.go:2255 一带），同样先短名解析再路由；ambiguous 回飞书提示（复用现有 "请用 #会话名 内容 指定" 风格，但列全名候选）。

## 边界与不破坏

- **兼容**：已有"打全名"用法必须继续可用（精确匹配优先）。
- **owner 隔离**：只在**发信方自己 owner 名下**解析，绝不跨 owner 命中（安全，防止把消息发到别人的会话）。
- **离线**：短名只匹配**在线**会话（沿用 `sessionOnline`/`onlineSessionsForOwner` 的 active 过滤）。
- `@成员#会话名` 路径（`parseMemberMention`/`resolveMemberSession`）如同样用精确名，可一并支持短名（次要，评估工作量，非必须本次做——**做的话同样撞名才提示**）。
- 别动 `deriveRegisteredSessionName` 的握手注册逻辑（命名规范 v1.0 的地基）。

## 验收判据（manager 复测）

- [ ] 飞书发 `#manager 内容` → 命中 `zzc-manager-2642`，投递成功。
- [ ] 会话 A 发 `#dev55`/`#developer` 短名 → 命中注册全名，dev55 收到。
- [ ] 构造同短名两会话 → 短名寻址回**候选全名提示**，不误投。
- [ ] 打**全名**仍正常（零回归）。
- [ ] 跨 owner 不命中（安全）。
- [ ] `go build ./... && go test ./...` 全绿；新增短名解析单测（unique/no-match/ambiguous 三态 + owner 隔离）。

## 交付

同任务①：每模块独立 commit + 说明 → send.py 回报 manager → manager 亲跑 go test + 探针 + 真机复测 → 编译部署 → 验收。分支别 push，禁碰生产。
