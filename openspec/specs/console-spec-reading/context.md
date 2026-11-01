# Console 规格读取上下文

## 代码位置
- 基线 a8bb5e5847884b62ba01f6f48fbce3772e2d8b04，来自源码，不是现网验收。
- `cmd/workpulse/console_routes.go`、`internal/m8/console_specs.go`：现有列表、详情、共享读取及鉴权路由。
- `internal/m8/console_specs_page.go`：renderSpec 调用 DwMdCore，当前还有编辑组件，不能直接视作新只读页面。
- `view-service/md-render-core.js`：公共 Markdown 转义、链接及自有文件图片规则；`view-service/md-render-core-sync.py` 与相关 parity/图片回归用于约束副本同步。实施前核其分发关系，避免新策略改变其它页面。
- `internal/m8/console_specs_assets.go`：现有 EasyMDE 静态资源，不是 Git 内容读取器。
- 本锚未发现实现 OpenSpec 仓库读取的产品路径；不假设已经存在仓库托管提供方 adapter 或用户身份映射。

## 环境与配置
- OpenSpec 1.16.0；Node >=22.12；`OPENSPEC_TELEMETRY=0`。仅执行席/CI 工具，不并入 Hub 构建链。
- 仓库配置 `openspec/config.yaml`；真实规格从仓库指定不可变 commit 读取，不读取会话记忆作事实。
- 仓库托管提供方 API 部署域名、凭据管理位置、权限映射、限额配置名称由 prodops 核实，见 R5 proposal 的接口清单；未设定或提供任何凭据值。

## 依赖
- 上游：Console 登录/身份映射、仓库托管提供方有效读取权限、固定 commit 文件/tree API；prodops 提供接口事实。
- 下游：后续 R6 坐标派审、R7 确认版本、R8 状态、R10 任务派单。本轮只定义 R5 浏览边界，不实现这些流程。
- [R5 proposal](../../changes/r5-render-openspec-readonly/proposal.md)；[旧 Spec 基线](../legacy-console-spec/spec.md)。

## 负责组
- Console 开发 coder56solzzc.zzc 单写；平台架构组审阅；prodops 核 API、权限与执行环境。

## 外部资料
- 固定版本决议、就绪计划链接见 R5 proposal「来源」；不使用 beta Stores 或会话之间转传源码。

## R5／R6／R10 共用仓库托管提供方接口（规格契约）

- 逻辑适配接口包括：受控提供方/实例及仓库发现、Console/Agent 主体映射与逐主体 read 核验、ref→完整 commit 解析/归属可读验证、指定 commit 的 tree/blob/类型/分页读取，以及同 commit 来源链接生成；具体 endpoint、字段、限流、错误与 secret 管理位置按提供方核实，不写死工蜂。
- 坐标、缓存和审计身份包含提供方/实例、稳定仓库 ID、完整 commit、路径；更换提供方或同名仓库不能继承旧权限、缓存、审阅或确认，须重新核实映射及对象来源，保留旧事实坐标。
- R6 坐标派审与后续 R10 任务坐标读取必须沿用此接口及独立主体核权，不经聊天或文件服务补源码。当前尚无独立 R10 OpenSpec 变更，不新建任务管理需求；本段是其共享读取约束。
- 首月 Console 用户绑定由管理员维护；按提供方核权不可用仍失败关闭。工蜂转 GitHub 尚未决定；本轮不迁移、不改任何实现。

## R5 T-03～T-06 实现上下文（2026-11-01）

来源事实：review/prodops/r5-facts e8b14b69，实测 08:37–08:43 UTC；细项见 R5 proposal R5-A01～06。GitHub 按数字仓库 ID 核响应身份，ref 固定完整 SHA；tree recursive=1 截断逐级回退；symlink/submodule 不跟随，tree.size 先核 2 MiB，逐次读取限速头。

实现位置：internal/specread、internal/store/openspec_read_binding.go 与迁移 0070、internal/m8/console_openspec_read*、cmd/workpulse/console_routes.go。读取 API /api/console/openspec-read；管理员 GET/PUT/DELETE /api/console/openspec-bindings。绑定不授予仓库权限，每次独立核权，无内容/权限缓存。新只读页面不改公共 Markdown 输出。

配置：DW_OPENSPEC_READ_ENABLED 默认关闭；DW_OPENSPEC_GITHUB_CREDENTIAL_FILE 默认未配，服务端受限凭据文件路径；DW_OPENSPEC_GITHUB_REPOSITORY_IDS 受控数字 ID 列表。缺任一即服务不可用且无入口。单文件 2 MiB，API 10 秒/整次 15 秒/并发 4，单响应 8 MiB/tree 10,000 项/截断回退最多 256 子树请求。真实凭据、绑定均未创建；scope、工蜂令牌和 Console/GitHub 绑定仍属真实验收缺口，T-07/T-08 另派。
