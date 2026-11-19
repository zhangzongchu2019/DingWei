# MCP Gateway Codex Compatibility Baseline

## Purpose

记录现有网关与Codex的协议边界：基线只接受2026-07-28无状态封套、精确回调登记和外部CIMD，Codex0.167.0原生初始化尚不能通过。本文绑定f2a5db5f现状，不把后续兼容实现当作已发布能力，不改变身份与业务授权。

## Requirements

### Requirement: MCB-001 既有封套与身份边界
网关 MUST 独立鉴权2026封套每请求，按现有tenant、scope、audience、AG和Origin处理四通讯工具；基线不接受Codex的2025初始化。

#### Scenario: MCB-001-S01 现有与Codex请求
- **WHEN** 现有2026客户端提供合法头与_meta，或Codex0.167.0发送initialize2025
- **THEN** 前者按权限成功，后者在基线上拒收；不得据OAuth成功声称原生互通

### Requirement: MCC-001 双协议通讯编解码
网关 MUST 并存2025-06-18的initialize、notifications/initialized、tools/list、tools/call、ping，与既有2026封套按版本分流，独立严格解码；不全局放宽未知字段。2025通知成功返回202空体，不建立协议授权会话。标准initialize的未知非空版本提议 MUST 协商到支持的06-18，MUST NOT仅因提议不同拒绝；允许已知clientInfo可选description/websiteUrl/icons但不据此授权。后续未支持版本操作仍拒绝。

#### Scenario: MCC-001-S01 2025与2026同时成功
- **WHEN** 实际Codex0.167.0使用2025初始化/通知/四工具/心跳，既有2026客户端使用合法封套
- **THEN** 两者各按相应结构成功，2026原有负测保持；撤2025入口则initialize目标红

#### Scenario: MCC-001-S02 参数与版本拒绝
- **WHEN** 未知顶层/方法参数、未支持的操作版本或不合法通知id到达，或标准initialize提出不支持的非空版本
- **THEN** 前三类拒绝且无工具副作用；initialize按MAF-002协商06-18，不以宽松JSON解析接受未知字段

### Requirement: MCC-002 每请求最小权限现核
两codec MUST 每次调用共同Authenticate/Seats服务，保持tenant、scope、audience、AG、Origin、PKCE、过期和吊销校验，不以握手缓存授权。

#### Scenario: MCC-002-S01 跨租户与握手后吊销
- **WHEN** 外组织扫码，或初始化后token过期/吊销/丢权限，或非法Origin调用
- **THEN** 无越权签发/执行；放宽tenant时对应组织外登录负测红

### Requirement: MCC-003 IP回环任意端口登记匹配
通用外部 CIMD MUST 对登记的 `http://localhost`、`http://127.0.0.1` 或 `http://[::1]` 回调，仅忽略有效端口并精确匹配 scheme、主机、path、query；localhost 主机大小写不敏感但必须精确相等。HTTPS及非回环规则保持既有精确匹配。Codex身份localhost仍由ZC-003拒绝；token MUST 精确绑定本次实际redirect。

#### Scenario: MCC-003-S01 随机端口登录与拒绝边界
- **WHEN** 通用CIMD登记无端口HTTP回环回调并使用动态端口，或请求错主机、错path、错scheme/query；Codex专用身份请求localhost
- **THEN** 仅登记回环及精确组件匹配的通用客户端回调接受，Codex localhost与其它错误均拒绝

### Requirement: MCC-004 自有静态CIMD与外部取件
网关 MUST 在本域/mcp/clients/codex.json公开生成只读CIMD，client_id绑定配置PublicOrigin，登记两IP回环无端口/callback。仅此精确自有ID本地解析；其他ID仍走原HTTPS/公有IP/无跳转resolver，不新增任意私网例外。

#### Scenario: MCC-004-S01 自有与外部边界
- **WHEN** 读取自有文档/精确ID授权，或提交近似URL/外域/私网CIMD
- **THEN** 自有文档可用且不受请求Host污染，其他ID保持原取件校验，未授权不得读业务

### Requirement: MCC-005 客户端最低版本与路径清单
文档 MUST 要求Codex至少0.167.0，提供显式本域oauth-client-id的add/login主路径说明，禁止配置oauth-resource；记录现网/预发新增静态路由，不部署。关闭态不登记新路径。

#### Scenario: MCC-005-S01 PM接入与关闭态
- **WHEN** PM按文档使用新add/login，或Hub关闭r53
- **THEN** 配置可供0.167.0使用，关闭态没有新文档路径；不承诺0.145 issuer兼容

### Requirement: MCC-006 原生证据与变异
交件 MUST 附真实0.167.0本地add/login/列工具/list_online成功原文、四刀目标红、2026/相关包/canonical/strict结果，禁用真实凭据且不碰节点。

#### Scenario: MCC-006-S01 完整本地交审
- **WHEN** 提交中间/最终坐标
- **THEN** 按该阶段实际完成项附命令/UTC/rc/SHA，原生失败不算成功，未部署且独立复核

### Requirement: ZC-001 先实测默认客户端行为
交件 MUST 用官方 Codex 0.167.0 只配置服务器地址，分别测授权服务器元数据有无 registration_endpoint 的 client_id、redirect_uri、DCR 请求，原文记录结果。桌面不可运行时 MUST 明记未测和原因，不由 CLI 推导桌面。

#### Scenario: ZC-001-S01 两种发现元数据
- **WHEN** 新的隔离配置仅添加 MCP URL，分别发现有／无注册端点的服务器
- **THEN** 记录授权参数、HTTP 请求和二进制 SHA，不使用真实账号、不启动推理

### Requirement: ZC-002 固定原文与有效策略
网关 MUST 对精确默认外部 client_id 使用固定的本地原文副本，附实取时刻及 SHA256，禁止在线获取或坏件后网络兜底；有效策略 MUST 为原文与公开 none、IP 回环回调、authorization_code/code 的交集，不自动接纳 localhost 或 refresh_token。

#### Scenario: ZC-002-S01 正常副本与坏件
- **WHEN** 默认身份请求登录，或测试注入坏件、非回环 HTTPS、非公开身份与缺失支持项
- **THEN** 正常副本无需 resolver 即进入登录；坏件 fail closed，不访问外网

### Requirement: ZC-003 身份和回调精确边界
网关 MUST 保留显式本域 codex.json 及其他外部 CIMD 的既有策略，仅默认身份完全相等时本地解析。两个精确本地 Codex 身份 MUST 使用已登记回环IP与http回调，端口可变；按㊿-补20接受 `/callback` 或 `/callback/` 后跟 1～64 位 A-Za-z0-9_- 单段，query保持登记值。Codex专用身份 MUST 拒绝localhost、错IP、空尾斜杠、编码／多段路径、userinfo、fragment与无效端口。其它通用外部CIMD的localhost规则按MCC-003执行。原URI保持授权／换票逐字绑定。

#### Scenario: ZC-003-S01 正负边界
- **WHEN** 两条有效本地Codex身份采用合法随机IP回环端口，或提交localhost/近似身份及错误回调
- **THEN** 仅有效身份和登记回调成功；Codex localhost拒绝，普通Claude CIMD可按MCC-003接受精确localhost动态端口

### Requirement: ZC-004 不放宽登录和请求鉴权
网关 MUST 保持 PKCE S256、固定 scopes、tenant、audience、token 对 client_id/真实 redirect/resource/verifier 的绑定，以及逐工具请求现核身份；默认身份路径不得获得鉴权例外。

#### Scenario: ZC-004-S01 拒绝越权
- **WHEN** 使用默认身份提交错误授权参数、兑换绑定、组织身份，或握手后身份被撤权
- **THEN** 拒绝签发或调用，且无业务副作用；各守卫单刀变异红在对应断言

### Requirement: ZC-005 原生验证与交审
交件 MUST 附官方 0.167.0 URL-only add 自动登录、四工具和 list_online 的隔离原生 E2E 原文、守卫目标红、m8 全包、mcpgw、canonical、strict 结果；不改版本、不部署、不碰线上席位。

#### Scenario: ZC-005-S01 本地完整证据
- **WHEN** 提交分支坐标供独立复核
- **THEN** 命令、UTC、返回码、SHA 与原文可复跑，模拟飞书与桌面未测限制明确，不自验收

### Requirement: ZC-006 原生回调安全段与专用身份
网关 MUST 仅对默认副本和本域 codex.json 两种精确 client_id 开启补20的回调安全段，不为其他客户端开放路径扩展。实现 MUST 附两身份正负例、换票逐字绑定，以及启用路径、长度上限、字符集、单段四刀目标红；不将 CLI 结果当桌面实测。

#### Scenario: ZC-006-S01 安全段边界与换票
- **WHEN** 两种身份使用 1、12、64 位安全段，或空段、65 位、点、斜杠、编码、另一合法段换票及外部身份提交扩展路径
- **THEN** 仅授权中的合法原 URI 可兑换；其他拒绝，四刀各红在相应判据

### Requirement: ZC-007 免输入授权只出影响方案
交件 MUST 依补19、21说明不经 Agent 执行命令的桌面设置入口及证据等级，提出可信飞书姓名加 UTC 时间的自动短名和默认目标方案，列校验、PG CHECK、地址解析、展示、权限与旧二进制回退影响；未裁定前 MUST 不改表单、名字或目标权限。

#### Scenario: ZC-007-S01 方案与未测边界
- **WHEN** 提交默认授权建议供组长裁定
- **THEN** 说明官方当前文档与本机未测差别，名字来自可信 profile，目标受控且不扩权，长度及默认目标待定，本单不提前实现

### Requirement: ZC-001 外部短名与关闭态兼容
系统 MUST 仅把外部短名校验与新PG CHECK放宽为1～20位小写字母数字，普通Agent与用户简称保持原限。DW_MCP_CONSENT_TARGETS_JSON MUST 为租户到最多64个唯一普通目标全名的映射；缺席或空集合 MUST 保留旧两项手填入口，关闭态不得新增长名。

#### Scenario: ZC-001-A 长名与旧名
- **WHEN** 升级后读旧1～8位行或写20位外部短名
- **THEN** 均成功，21位及非法字符失败

#### Scenario: ZC-001-B 关闭态
- **WHEN** 本租户配置缺席或为空
- **THEN** 显示旧两项手填且八位限制，其他租户配置不得开启本租户

### Requirement: ZC-002 可信自动命名
系统 MUST 仅在可信飞书profile通过租户与主体验证后，用英文名小写去非字母、否则中文名拼音首字母、否则pm前缀，名字部分截8位并加UTC yyMMddHHmmss。同OAuth attempt MUST 仅生成一次，多选重试不换名；同秒同名靠不可复用AG序号区分，主体仍按provider/tenant/issuer/subject区分。

#### Scenario: ZC-002-A 固定生成
- **WHEN** 输入多音字、空名、符号或超长可信姓名
- **THEN** 按固定拼音/回退/截断与UTC生成，不采客户端输入

#### Scenario: ZC-002-B 重放
- **WHEN** 同一回调或选择票据并发重放
- **THEN** 仅一次签发，不能再次生成名字或主体

### Requirement: ZC-003 受控目标与一次授权
系统 MUST 对单一有效配置目标隐藏两项输入；多配置目标在可信profile后显示一次单选，仅列仍有效允许目标，并预选该主体最近成功且仍有效的选择。无有效目标 MUST 拒绝；签发前 MUST 再核目标存在、active、非外部、仍在允许集合，不回落任意目标。selection票据 MUST 一次性、过期有效、绑定原attempt与浏览器；四scope和两成员AG不变。

#### Scenario: ZC-003-A 单目标
- **WHEN** 仅一个配置目标且现核有效
- **THEN** 页面仅同意并进入飞书，可信profile后自动签发

#### Scenario: ZC-003-B 多目标与撤销
- **WHEN** 多目标完成飞书认证后选择或目标被撤销
- **THEN** 仅允许现有效集合内单选，撤销、篡改、过期、错误浏览器或重放均拒绝

### Requirement: ZC-004 长名回退边界
发布说明 MUST 要求回旧二进制前封口全部长名外部席位、处理确定未执行投递并保留墓碑与ACK。功能关闭 MUST 不改变已有长名；存在长名行 MUST 不恢复旧CHECK，只能前向修复。

#### Scenario: ZC-004-A 禁止旧约束
- **WHEN** 已存在长名行并试图恢复旧CHECK
- **THEN** PG拒绝，旧行保持原样

#### Scenario: ZC-004-B 安全前向状态
- **WHEN** 关闭新默认配置并继续读已有长名
- **THEN** 名称和投递确认不被截短或删除

### Requirement: ZC-005 OAuth login 用途隔离与浏览器表单
网关 MUST 在哈希前对 state、started、selection 各加不同用途前缀。GET state 不得消费启动标记或选择记录，POST selection 不得接受真实 state。连接与选择页 MUST 在真实浏览器保留同源 POST 的可信 Origin，单/多目标都能换票；保持来源守卫。

#### Scenario: ZC-005-A 类型互认拒绝
- **WHEN** 同浏览器以 started:attempt 或 selection:token 作 GET state，或用真实 state 作 POST selection
- **THEN** 均400且对应其他用途记录不被消费；started伪造后第三次启动仍400，正常回调与选择仍一次成功

#### Scenario: ZC-005-B 真实浏览器授权
- **WHEN** Chromium 访问隔离真Hub与飞书替身，执行单目标或多目标表单授权
- **THEN** 单目标无编辑输入，多目标能选择并提交，无 origin_rejected，均成功签发和换票

#### Scenario: ZC-005-C 表单跳转链与策略注入
- **WHEN** 表单303前往配置飞书或302回已校验客户端，或构造含CSP分隔符的来源
- **THEN** 仅可信origin出现在form-action，跳转链成功；非法来源不进入策略，保留default-src与frame-ancestors限制
