# 补19、补21：免命令入口与默认授权方案（仅方案，未实现）

依据 `review/opus50tc2k/rulings` 的 `c9581bdb`，㊿-补19、补21。这里不修改表单、短名校验、迁移或授权策略；待组长裁定后实现。回调补20是独立已授权代码变更。

## 不经过 Agent 执行命令的桌面入口

已查官方 [MCP 配置说明](https://developers.openai.com/codex/mcp/)（2026-11-09 访问，实际重定向至 [当前官方页面](https://learn.chatgpt.com/docs/extend/mcp?surface=cli)）。官方描述的直接入口是设置中的 MCP 服务器页，新增服务器，选 HTTP 传输并填写 URL，保存／重启连接，然后在服务器列表触发认证。需要服务器显示名时固定为 DingWei；不是 OAuth client_id。无需让对话里的 Agent 运行 add/login 命令，也不依赖安装 CLI。

证据等级：这是当前官方说明，页面使用“ChatGPT 桌面应用”的名称；不是本机 Codex 桌面 0.167.0 的实测截图。当前机器只有 CLI，桌面仍**未测**，旧版本具体按钮名称和是否自动填显示名不能确认。不得将现有十余次 Agent 命令批准流程描述为“桌面设置”。最终 PM 一页说明按业主实际界面由组长写，本件只提供官方直接入口和待验证差异。

## 建议的无输入授权流程

1. 原生 OAuth authorize 继续先验证精确 client_id、回调、PKCE S256、resource/scopes，持久化 attempt 和绑定浏览器 cookie。
2. 网关接入页不再让 PM 填短名或目标全名。显示默认协作目标、四通讯权限及自动命名规则，只保留同意并进入飞书的按钮；CSRF、一次性 state、browser 绑定和过期不删。
3. 飞书返回服务器获取的 user_info，先验证组织身份，再生成短名，按服务器配置的最小目标策略选目标，现核目标有效／非外部席位，沿用事务签发。名字和目标不从 OAuth query、表单隐藏值、客户端参数、环境自报或回调 query 取信。
4. 返回授权码，原生客户端使用本次实际 redirect_uri 和 verifier 兑换；回调、换票、每请求 Authenticate 和 AG 栅栏不变。

现有 `feishulogin.Start` 在去飞书前就要求有效短名和目标，因此不能只把 HTML 输入改成 hidden。建议新增显式“使用受控默认值”意图，绑定 OAuth attempt／已配置目标策略版本；在已验证飞书 profile 后补全真实签发意图。保留手填旧入口用于兼容，不让缺省值落入任意目标或空名字。该接口尚未实现，须单独规格与正负例。

## 自动短名规则与兼容建议

遵循业主规则：飞书英文名转小写并去非字母；没有有效英文时取中文姓名拼音首字母；仍无则用固定 `pm` 前缀；拼接服务器 UTC `yyMMddHHmmss`。例：`alice261109023501`。建议名字部分最多 8 个字母，总短名上限 20；这是待裁定的长度选择，不是业主已定的额外限制。

仅使用服务器已验证的飞书原始 profile。现有 `Principal.DisplayName` 在中文回退时是完整拼音，已丢失每字边界，不能把它当作“首字母”使用；生成器应在可信 user_info 阶段取原始中文姓名。英文名清洗为空时继续中文回退；对多音字、空名、符号、超长及同秒登录给固定测试。

同一 OAuth attempt 固定一次生成的名字与策略选择；重试不改变时间或另建主体。主体唯一性仍按 provider/tenant/issuer/subject，绝不按显示名合并。同秒同名不需要随机改写业主的命名后缀：现有外部名还包含不可复用的全局 AG 序号，不同新席位可区分。是否每次重新登录新建外部席位继续遵循既有事务语义，不借本单改动。

| 层 | 当前限制／代码 | 采用建议后的影响与处理 |
|---|---|---|
| 共享校验和外部名拆分 | `internal/extshared/extshared.go`：短名 1～8，AgentName / SplitAgentName 共用 | 只放宽外部短名至 20；1～8 旧名继续有效，不扩大普通 Agent／用户名称规则 |
| 飞书开始及授权页 | `internal/feishulogin/handler.go`、`connect.go`：先验名字／目标，HTML 8 位 | 增加受控默认意图，完成可信 profile 后生成；兼容旧 POST，服务器做最终验证；不只改前端 maxlength |
| 席位事务 | `internal/extseat/service.go`：再次 ValidShortName，写 np_agent/np_extseat、AG、token | 必须同步新校验；身份、tenant、目标非 external、同事务及单次回调不变 |
| PG 约束 | `internal/store/migrate/migrations/0105_extseat.sql`：np_extseat.short_name CHECK 1～8 | 不能编辑已发布迁移；新加迁移调整该 CHECK，旧行原样可读，不 DROP 表、不删身份或 ACK；与代码发布顺序绑定 |
| 主身份和地址 | `identity/model.go` 普通 Agent 2～32、用户简称 2～16；`identity/service.go:ResolveFullName` 另有 external 受控分支 | 保持普通规则。20 位短名＋下划线＋最大 20 位 AG码＝41，连用户简称和点最长 58；外部专用解析需覆盖，不能顺手放宽普通名称或让普通 origin 伪装外部 |
| Console／View | `internal/m8/console_auth.go`、`tempgroup/console.go`、`newplat_view.go` | 检查外部名称展示、临时组、搜索、订阅／票据、地址往返及页面窄屏；该发布基线 Console 未检出外部短名 8 位表单；不能把自备 Agent／用户简称输入当外部短名批量放宽 |
| 通讯封套与 helper | `extseat/inbox.go`／outbox、extwire、Hub 路由、helper 名称展示 | 核完整外部全名 58 字符能往返／回信，保持 by-ID 授权与 AG；逐个边界验证，不截短后寻址，不扩大 user_id 简称 |
| 旧二进制回退 | 旧 regex 和旧 PG CHECK 均只识别 8 位外部短名 | feature-off 只停新增长名，不自动使已有长名兼容旧版。回退前封口长名席位、逐封处理确定未执行投递，保留墓碑/ACK；不可恢复旧 CHECK 直到长名存量有明确去向，需发布单专项验证 |

全量影响中的重要纠正：本发布基线 Console 未检出外部短名 8 位输入；其自备 Agent／用户简称与外部短名不是同一校验，不能照标签批量改所有数字 8。实际 PG CHECK 也必须纳入，单改 extshared/HTML 会在事务写入时失败。本件没有改这些位置。

## 目标 Agent 的默认选择与权限影响

推荐由管理员配置允许协作的默认目标，按组织／已验证主体作用域选择，不从公开在线清单猜目标、不默认接到任意在线 Agent。一个有效且已批准目标时无需输入；多个时可复用该主体最近一次仍在允许集合内的选择；没有可用唯一默认时给明确提示或受控选择，不能悄悄扩大范围。管理员可为 PM 预配置唯一目标，满足“一次加好、飞书同意”。

当前手填目标会创建双成员临时 AG，新默认方案仍只建同样两人，scope 仍四项，不加入全组织、不提供宿主执行权。缺省策略影响“与谁建立协作”，因此目标／允许集合必须由组长确定；不能把“减少输入”解释为同租户全部可达或任意组长可达。选择后到签发之间再次现核目标存在、active、允许集合／策略版本，撤权与缺失均 fail closed。

待裁定项：① 外部短名上限建议 20、名字部分上限 8，是否采用；② 每类 PM 的唯一默认目标及允许集合来源；③ 多目标时要求配置唯一默认，还是显示一次受控选择；④ 新长名存量与旧二进制回退边界。四项明确后再实现、迁移和验证，当前代码仍保留原两项表单输入。
