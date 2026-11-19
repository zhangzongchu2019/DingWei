## ADDED Requirements

### Requirement: MAF-001 每请求鉴权先于协议
/mcp MUST 在HTTP方法、Origin、媒体头、版本选择及JSON解码前独立鉴权。缺失/无效/过期/吊销bearer SHALL 返回401与既有resource_metadata和scope的WWW-Authenticate，未认证时MUST NOT读取body或执行业务。其它路径不改变。

#### Scenario: MAF-S001 三版本未鉴权发现授权
- **WHEN** 03-26、06-18、11-25客户端未带bearer发送initialize/tools/list/ping，版本头有或无
- **THEN** 均401且WWW-Authenticate逐字保持，业务调用为0，不返回HeaderMismatch或资源

#### Scenario: MAF-S002 方法头与坏body不抢鉴权
- **WHEN** 未认证/mcp含未知方法、重复版本头、错误Origin/媒体类型或不可读body
- **THEN** 401挑战且body读取次数0；已认证同类请求仍受原方法/Origin/媒体校验

### Requirement: MAF-002 初始化协商到实际支持版本
标准initialize SHALL 接受非空版本提议，若不支持则返回完整支持的2025-06-18，不仅因提议版本不同报错。服务端MUST NOT宣称未实现的03 batch或11 tasks能力。已知clientInfo可选展示字段仅解码不用作授权。

#### Scenario: MAF-S003 协商后实际工具链
- **WHEN** 已认证客户端提议03-26、06-18、11-25或未知版本
- **THEN** initialize成功返回06-18，只宣告tools；按返回版本发送通知、ping、list和call均成功，后续吊销则401

### Requirement: MAF-003 缺头默认与私有兼容
无版本头且无其它版本依据时MUST 按2025-03-26识别，已认证不支持操作返回400/-32022；initialize仍按params协商。私有2026封套MUST保留头/meta精确检查，不缓存身份，不新增工具或资源权限。

#### Scenario: MAF-S004 缺头操作明确不支持
- **WHEN** 已认证标准tools/list或ping缺版本头
- **THEN** 返回400/-32022而非私有HeaderMismatch，无业务副作用；合法06与私有2026请求保持原形状

### Requirement: MAF-004 证据与删保护
交件 MUST 包含固定源canonical、mcpgw/extseat/m8三遍真PG strict、逐名SKIP、原始失败和对应删保护业务红，不以编译错误或panic替代。

#### Scenario: MAF-S005 版本检查抢在鉴权前
- **WHEN** 将版本校验恢复到鉴权入口之前
- **THEN** 未认证三版本401格转业务红；还原源后基线恢复绿
