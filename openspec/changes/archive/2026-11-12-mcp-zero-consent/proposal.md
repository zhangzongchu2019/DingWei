## Why

业主要求产品经理只添加一个 MCP 地址并在飞书授权，不应反复输入短名和目标。现有接入页要求两项手填且外部短名仅八位，无法承载业主指定的姓名加十二位时间格式。本变更按组长裁定提供默认关闭的租户允许集合、可信身份自动命名及单目标免输入授权，同时保留旧入口和最小两成员编组权限。

来源：wego-ai/dingwei，review/opus50tc2k/rulings，5641584ae1eeb83e199f80608019e3b89e855758，docs/rulings/S-TEAM-MIGRATE-HOSTED-20261017.md，㊿-补21、补29；2026-11-09 改派本实际线程。

## What Changes

- 外部短名放宽到20；新增PG迁移，不改普通名称规则。
- 增加按租户的服务器允许目标配置及可信profile自动命名。
- 多目标在可信身份取得后一次单选、预选仍有效的最近目标；签发前现核。
- 成文长名回退前提与发布验证。

## Capabilities

### Modified Capabilities

- `mcpgw-codex-compat`: 默认授权与外部短名兼容。

## Impact

涉及feishulogin、extshared、extwire、mcpgw、workpulse装配及身份PG迁移。不部署、不改版本、不改子Agent分支。

## 待确认假设

无。多目标选择安排在可信profile之后，以免从浏览器自报主体查询最近目标。

## 待 PM 决策

现网允许集合初始值须组长另定并经业主确认；本变更不写现网配置。
