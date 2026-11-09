# 产品经理用 Codex 接入 DingWei

本说明为 S-MCP-ZERO-CONFIG 待发布草稿。目标客户端是 Codex 桌面版；最低验证的原生组件为官方 0.167.0。管理员通知兼容网关正式开放后再接入。本次没有部署，尚未在桌面版验证，不能视作现网已可用的承诺。

## 桌面版接入目标

按[官方当前设置说明](https://developers.openai.com/codex/mcp/)，可直接从设置里的 MCP 服务器页新增服务器，选择 HTTP 传输；此路径不经过对话 Agent 执行命令。显示名需要填写时使用 DingWei，服务器地址填写：

```text
https://dw123.cmjrt.com/mcp
```

不填写 client_id、客户端密钥或额外资源参数。按客户端引导打开授权页面，填写接入短名及要协作的目标 Agent 全名，用本组织飞书身份完成授权。授权完成后，让 Codex 调用 DingWei 的 `list_online`；能返回清单（可能为空）才表示调用成功。

本机未安装 Codex 桌面版，以上桌面操作是待实测的接入目标。已通过的是官方 0.167.0 CLI 的 URL-only 原生流程。按㊿-补20，两个精确 Codex client_id 支持 `/callback` 或 `/callback/<1～64 位安全字符单段>`，空尾斜杠仍拒。按钮名称及自动填显示名行为尚需实际桌面确认。当前授权页仍有短名和目标输入；免输入方案只交审，尚未实现，不把“飞书点同意”作为已发布功能承诺。

## 管理员复现及既有配置

0.167.0 CLI 只配 URL 即自动开始首次登录：

```sh
codex mcp add dingwei --url https://dw123.cmjrt.com/mcp
```

`codex mcp login dingwei` 只用于重新授权。不要加 `--oauth-resource` 或手写 `oauth_resource`；资源由网关发现。网关对默认外部 client_id 使用固定本地元数据副本，不依赖现网出口访问 chatgpt.com。

已配置的显式本域 client_id 仍可继续使用；管理员需要复现旧路径时：

```sh
codex mcp add dingwei --url https://dw123.cmjrt.com/mcp --oauth-client-id https://dw123.cmjrt.com/mcp/clients/codex.json
```

不需要固定本地回调端口。仅接受已登记的回环 IP、http 和受控回调路径；不要手改为 localhost。预发使用管理员提供的完整地址，不混用环境。

## 使用与问题处理

DingWei 提供查看在线清单、发送消息、等待收件、确认收到四个通讯工具。可协作目标由平台编组和账号权限决定。

- 飞书登录失败：确认属于本组织，使用本次新授权链接；不要转发链接或凭据。
- `invalid_request`：检查是否额外配置了资源参数。
- `invalid_client`：交管理员核客户端版本及实际回调形态；不要发送 token 或凭据文件。
- 工具目录为空或初始化错误：核客户端至少 0.167.0、网关已发布兼容版本；0.145.0 不提供兜底。
- 在线清单为空：可能没有授权目标在线；与工具目录为空不同。

本文不是部署、开放来源或变更现网的授权。
