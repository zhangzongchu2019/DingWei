# Proposal

## Why

R5 的 GitHub 身份绑定目前只有管理员 API，没有 Console 管理页面。绑定配置需要管理员手动构造请求，容易填错 Console principal、GitHub 登录名或数字 ID，也难以在变更前核对现有映射与审计结果。业主已批准在 r48 增加一个最小绑定管理页；先以 OpenSpec 明确页面入口、复用接口、权限边界、现有覆盖行为和待决交互，避免实现时扩展后端授权或凭据范围。

## What Changes

- 新增一个仅平台管理员可访问的 Console 绑定管理页，入口放在 Console 平台管理区；非管理员直接访问页面也必须被拒绝。
- 页面以 Console principal、GitHub 登录名和数字 ID 作为表单字段；provider 固定 github、instance 固定 github.com。
- 页面调用现有 GET/PUT/DELETE /api/console/openspec-bindings 完成单条绑定的查看、创建/更新、删除；保留现有路由权限、校验、审计和错误语义。
- 首期不增加绑定列表 API；按 principal 查询单条。不做服务器端 GitHub 查询、不加入凭据输入或回显、不改变 R5 read 授权方式。
- 保存前显示目标 principal 和当前映射；现有 PUT 是 upsert 覆盖，页面先显示旧值并在管理员确认后提交覆盖，不把重复建模成冲突。

## Capabilities

### Modified Capabilities

- console-spec-reading：新增受平台管理员保护的绑定管理页及对现有绑定接口的前端调用约束。

### New Capabilities

无。

## Impact

只新增 Console 静态管理页面及其页面入口/导航；页面通过现有管理员 API 读取、写入、删除。现有 API 的授权包装、请求字段、校验、存储与审计语义维持不变。页面不得读取、展示、提交或回显 GitHub 凭据，也不通过服务端出网查询账号 ID。产品实现和验收不在本提案内。

## 来源

需求来源：[R5 绑定落地方式裁定](https://git.code.tencent.com/wego-ai/dingwei/blob/cf116475ebc6d40c977269be55d30d237d8b563d/docs/rulings/S-TEAM-MIGRATE-HOSTED-20261017.md)，第㉘条，Hub 收信时间 2026-11-02T01:32:07Z。业主批准“r48 加一个最小绑定管理页，管理员在 Console 填好点一下即建”；该条同时限定复用现有 GET/PUT/DELETE、不改接口语义与权限，要求按 OpenSpec 先出小提案再实现。三项交互定值来源：[业主 rulings ㉙](https://git.code.tencent.com/wego-ai/dingwei/blob/b253543a/docs/rulings/S-TEAM-MIGRATE-HOSTED-20261017.md)，第㉙条（2026-11-02）。

## 现状勘察

### 页面与路由

- 基线 c248506cf120c3af2a06eeaeeae3038f596537b1。
- cmd/workpulse/console_routes.go 注册三个现有 API：GET、PUT、DELETE /api/console/openspec-bindings；每条均先 RequireConsolePrincipal，再 RequireConsolePlatformAdmin。现有 GET /console/openspec 只要求 Console principal，不可直接用作管理员绑定页。
- internal/m8/console_openspec_read.go 的 HandleOpenSpecBinding 处理单条绑定。新页面入口需单独受 Console principal 与平台管理员双重保护；导航入口位于 Console 平台管理区，页面/API 均不能只靠隐藏链接保护。
- 现有页面采用同源路径调用浏览器 fetch。页面 PUT/DELETE 显式设置 Content-Type: application/json、无查询串；浏览器对非安全方法自动带 Origin，使用同源 URL 后 Origin host 与 r.Host 匹配。不能省略 JSON 头，也不应手工伪造 Origin。

### 查询与列表能力

现有 GET 只接受 principal、provider、instance 三个查询参数，并调用 SpecReadBinding 查询一条映射；没有查询全部绑定的 handler、store 方法或 API。无记录返回 JSON null。按业主定值，首期输入 Console principal 后查询并显示该条映射，不新增全量列表接口。

### 表单字段与既有 Valid 规则

PUT/DELETE handler 将 JSON 解码为 Binding，拒绝未知字段，最大体积 4096 字节，并要求 Binding.Valid()。页面表单仅包含以下五个 API 字段；不改变服务端校验：

| API 字段 | 页面值 | 现有校验 |
| --- | --- | --- |
| principal | Console 账号/principal | 非空，长度不超过 200 |
| provider | 固定值 github | 必须精确等于 github |
| instance | 固定值 github.com | 必须精确等于 github.com |
| account_id | GitHub 数字用户 ID | 正整数十进制字符串，1～20 位，不允许前导零 |
| login | GitHub 登录名 | 1～39 位；首字符字母或数字；后续仅字母、数字、连字符 |

PUT 重复使用同一 principal/provider/instance 时，SQLite 唯一键 upsert 更新 account_id、login、updated_by、updated_at，并在同事务追加 operation=set 的审计行；这是覆盖更新，不是冲突。DELETE 先将既有映射复制为 operation=delete 审计行，再删除。若记录不存在，DELETE 仍可成功但 SELECT 无行可供写审计，页面不得显示“已删除一条既有映射”。GET 当前无审计写入。

### 身份 ID 查询建议

按业主定值，本页面由管理员手填 GitHub login 与数字 account_id，页面提示从 GitHub 公共用户资料/API 查询并核对，不由 Hub、浏览器页面脚本或服务端 API 自动出网。管理员可在自己的浏览器或受控终端读取 https://api.github.com/users/{login} 的 JSON id/login 字段并核对二者对应；最终仍由服务端 Binding.Valid() 校验格式。

### 待确认假设

- 假设 Console 平台管理区能够放置管理员专属入口；若现有导航不能按管理员身份区分，页面路由本身仍须强制鉴权，导航隐藏不得替代授权。
- 假设 Console principal 是管理员可从现有平台管理资料中复制的稳定标识；页面不新增 principal 枚举 API。
- 不假设存在绑定全量列表、GitHub ID 自动查询、GitHub 凭据输入或服务端审计查询页面。

### PM 决策（已定）

1. **PM-R5-BIND-01：已定（rulings ㉙）。** 首期不增加全量绑定列表；按 principal 查询单条映射。理由：现有 GET 只支持按 principal/provider/instance 查单条，避免新增后端枚举接口。
2. **PM-R5-BIND-02：已定（rulings ㉙）。** 管理员手填 GitHub login 与数字 ID，页面提示从 GitHub 公共用户资料/API 查询并核对；服务端不出网。理由：不引入外部请求、速率依赖或新的服务端网络依赖。
3. **PM-R5-BIND-03：已定（rulings ㉙）。** 后端仍按现有 PUT upsert 覆盖并记录审计；页面先显示旧值，管理员明确确认后才提交覆盖。理由：保留既有 API 语义，同时降低误覆盖风险。

历史选项记录（保留供决策追溯）：PM-R5-BIND-01 曾比较单条查询与新增全量只读 API；PM-R5-BIND-02 曾比较管理员手填与页面自动查询；PM-R5-BIND-03 曾比较确认后覆盖与提交即覆盖。业主定值分别为上列 A、A、A，记录依据为 rulings ㉙。
