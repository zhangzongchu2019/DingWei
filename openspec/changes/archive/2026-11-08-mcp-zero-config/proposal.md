## Why

产品经理的目标客户端已裁定为 Codex 桌面版，接入时应只填写服务器地址。发布基线仍要求显式填写本域 client_id，因为现网出口获取默认外部元数据得到 403。本变更先测官方钉版客户端的发现行为，再用可审计的本地副本消除外部取件前提，保持原有登录、租户、资源和逐请求鉴权边界，不执行部署。

需求来源：工蜂 `review/opus50tc2k/rulings` 的 `2412f2e1`，rulings ㊿-补18；派单 S-MCP-ZERO-CONFIG。基线发布提交 `68505cd3`。

## What Changes

- 精确默认外部 client_id 采用固定本地原文副本并施加安全交集；不新增动态注册。
- 保留显式本域 client_id 及其他外部 CIMD 原有策略。
- 附 URL-only 原生探针、注册端点有无对照、守卫变异和回归证据。

## Capabilities

### Modified Capabilities

- `mcpgw-codex-compat`：增加 URL-only 默认身份本地解析及实测证据。

## Impact

仅 Hub OAuth 客户端解析、测试和说明；不改数据库、版本号，不部署。

### 待确认假设

- 无 Codex 桌面应用的 Linux 工作区不能验证桌面实际请求。CLI 结果单列，桌面标记“未测”；按补20支持受控安全段，但仍不宣称桌面实测通过。

### 待 PM 决策

- 补19、21的自动短名长度、唯一默认目标及多目标选择需组长裁定，详见 DEFAULT-CONSENT-PROPOSAL.md；本单只出方案。桌面实测由具备该客户端的复核环境补齐，本件不据 CLI 宣称桌面验收通过。
