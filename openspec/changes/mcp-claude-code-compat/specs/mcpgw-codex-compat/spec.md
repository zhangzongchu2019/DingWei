# MCP Gateway Codex Compatibility Delta

## MODIFIED Requirements

### Requirement: MCC-003 IP回环任意端口登记匹配
通用外部 CIMD MUST 对登记的 `http://localhost`、`http://127.0.0.1` 或 `http://[::1]` 回调，仅忽略有效端口并精确匹配 scheme、主机、path、query；localhost 主机大小写不敏感但必须精确相等。HTTPS及非回环规则保持既有精确匹配。Codex身份localhost仍由ZC-003拒绝；token MUST 精确绑定本次实际redirect。

#### Scenario: MCC-003-S01 随机端口登录与拒绝边界
- **WHEN** 通用CIMD登记无端口HTTP回环回调并使用动态端口，或请求错主机、错path、错scheme/query；Codex专用身份请求localhost
- **THEN** 仅登记回环及精确组件匹配的通用客户端回调接受，Codex localhost与其它错误均拒绝

### Requirement: ZC-003 身份和回调精确边界
网关 MUST 保留显式本域 codex.json 及其他外部 CIMD 的既有策略，仅默认身份完全相等时本地解析。两个精确本地 Codex 身份 MUST 使用已登记回环IP与http回调，端口可变；按㊿-补20接受 `/callback` 或 `/callback/` 后跟 1～64 位 A-Za-z0-9_- 单段，query保持登记值。Codex专用身份 MUST 拒绝localhost、错IP、空尾斜杠、编码／多段路径、userinfo、fragment与无效端口。其它通用外部CIMD的localhost规则按MCC-003执行。原URI保持授权／换票逐字绑定。

#### Scenario: ZC-003-S01 正负边界
- **WHEN** 两条有效本地Codex身份采用合法随机IP回环端口，或提交localhost/近似身份及错误回调
- **THEN** 仅有效身份和登记回调成功；Codex localhost拒绝，普通Claude CIMD可按MCC-003接受精确localhost动态端口
