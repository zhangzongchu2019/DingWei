## ADDED Requirements

### Requirement: CSR-008 平台管理员专属绑定管理页
R5 Console MUST 只向平台管理员提供 GET /console/openspec-bindings 页面及平台管理区入口。页面和现有绑定 API 的权限 MUST 继续由 RequireConsolePrincipal 与 RequireConsolePlatformAdmin 共同保护；隐藏导航不得替代服务端授权。页面 MUST 复用现有绑定 API，不改变其路由、权限、校验或存储语义，也不显示或回显任何 GitHub 凭据。

#### Scenario: CSR-S016 非管理员不能进入绑定管理
- **WHEN** 普通 Console principal 直接访问绑定页、尝试读取绑定 API 或提交 PUT/DELETE
- **THEN** 页面/API 均拒绝且不泄露绑定详情；平台管理员可进入页面

#### Scenario: CSR-S017 管理员创建并回读绑定
- **WHEN** 平台管理员提交有效 principal、GitHub login 与数字 ID
- **THEN** 页面以现有 PUT 创建/更新，并用现有 GET 回读显示对应单条绑定；不调用 GitHub 外部查询

#### Scenario: CSR-S018 管理员查看并删除绑定
- **WHEN** 平台管理员输入 principal 并查询现有映射，随后确认删除
- **THEN** 页面用现有 GET 显示该条或空状态，用现有 DELETE 删除；删除成功后回读为空，保留现有审计语义

### Requirement: CSR-009 表单遵循既有校验与同源请求边界
绑定页 MUST 提交现有 Binding 的 principal、provider、instance、account_id、login 字段，并遵守 Binding.Valid() 规则。provider/instance 固定为 github/github.com。PUT/DELETE MUST 用同源 fetch、浏览器 Origin、Content-Type application/json、无 query string；服务端校验仍为最终判定。页面 MUST 将错误安全地作为文本显示。

#### Scenario: CSR-S019 非法字段显示服务端错误
- **WHEN** 任一字段不符合既有 Binding.Valid()，或 JSON 含未知字段
- **THEN** API 拒绝变更；页面显示现有安全错误信息，不清空/覆盖旧绑定，不把错误当成功

#### Scenario: CSR-S020 重复创建沿用 upsert 覆盖
- **WHEN** 对同 principal/provider/instance 再次提交有效但不同的 account_id/login
- **THEN** 按现有 PUT upsert 替换映射，updated_by/updated_at 更新并新增 operation=set 审计；页面在提交前显示将被替换的当前值，按已批准的页面确认交互继续，不把该操作显示为冲突

#### Scenario: CSR-S021 页面不接受凭据或暴露列表
- **WHEN** 管理员使用表单创建/查询/删除映射，或普通用户直接请求页面/API
- **THEN** 请求字段中没有 PAT/token；页面只查询明确 principal 的单条绑定，不展示全量 principal 清单，不自动发 GitHub 查询。若 PM 决定全量列表，先另行批准 API 与规格
