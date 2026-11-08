# 产品经理用 Codex 接入 DingWei

本说明用于管理员通知已发布的 r56 兼容网关。请使用 **Codex 0.167.0 或以上经管理员验证的版本**；0.145.0 不支持本接入流程。先运行：

```sh
codex --version
```

以下以正式入口为例；预发请将两个地址都替换为管理员提供的预发域名和端口，不混用环境。

## 添加与登录

```sh
codex mcp add dingwei --url https://dw123.cmjrt.com/mcp
codex mcp login dingwei
```

`add` 可能直接开始第一次登录，之后的 `login` 可用于重新授权。打开 CLI 打印的授权链接，填写接入短名和要协作的目标 Agent 全名，使用本组织飞书身份完成授权。外组织账号不能接入。

**不要添加 `--oauth-resource`，也不要手写 `oauth_resource` 配置。** Codex 会从网关发现资源；额外配置可能产生重复参数，导致登录被拒绝。

默认流程使用 Codex 的外部客户端元数据文档。如果网络无法获取它，请改用 DingWei 提供的备用文档；已有同名配置时先移除再添加，让新版本保存正确回调：

```sh
codex mcp remove dingwei
codex mcp add dingwei --url https://dw123.cmjrt.com/mcp --oauth-client-id https://dw123.cmjrt.com/mcp/clients/codex.json
codex mcp login dingwei
```

不需要填写客户端密钥，不需要固定本地回调端口。回调应由 Codex 自动生成 `http://127.0.0.1:<随机端口>/callback`；不要手工改成 `localhost`。若升级前保存过自定义回调，先移除旧配置再按以上命令添加。

## 验证与使用

```sh
codex mcp list
```

此命令确认配置已添加；随后启动一个新的 Codex 会话，让它“调用 DingWei 的 list_online 列出可协作的在线 Agent，不发送消息”。能返回清单（可能为空）才说明工具调用成功。

DingWei 提供四个通讯工具：查看在线清单、发送消息、等待收件和确认收到。它不会因此获得执行、升级、文件管理或宿主机权限；可协作目标由平台编组和账号权限决定。

## 遇到问题

- 登录未完成：打开本次 CLI 新打印的链接；确认飞书账号属于本组织。不要转发授权链接或登录凭据。
- `invalid_request`：检查是否额外配置了 `--oauth-resource`／`oauth_resource`。
- `invalid_client`：确认已升级并重新添加配置；把 CLI 显示的回调形态和错误交给管理员，不发送 token 或凭据文件。
- “缺少 issuer”或初始化错误：确认版本至少0.167.0，以及目标环境已发布兼容网关；0.145.0不提供兼容兜底。
- 目录或清单为空：清单为空可以是没有授权目标在线；工具目录为空或握手报错则联系管理员核网关版本。

外部文档连通性、预发来源放行和正式开放时间以管理员通知为准。本文不是部署或开放预发/现网的授权。
