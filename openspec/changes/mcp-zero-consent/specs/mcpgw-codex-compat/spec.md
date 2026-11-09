## ADDED Requirements

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
