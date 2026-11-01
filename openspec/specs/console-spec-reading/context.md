# Console 规格读取上下文

## 代码位置
- 基线 a8bb5e5847884b62ba01f6f48fbce3772e2d8b04，来自源码，不是现网验收。
- `cmd/workpulse/console_routes.go`、`internal/m8/console_specs.go`：现有列表、详情、共享读取及鉴权路由。
- `internal/m8/console_specs_page.go`：renderSpec 调用 DwMdCore，当前还有编辑组件，不能直接视作新只读页面。
- `view-service/md-render-core.js`：公共 Markdown 转义、链接及自有文件图片规则；`view-service/md-render-core-sync.py` 与相关 parity/图片回归用于约束副本同步。实施前核其分发关系，避免新策略改变其它页面。
- `internal/m8/console_specs_assets.go`：现有 EasyMDE 静态资源，不是 Git 内容读取器。
- 本锚未发现实现 OpenSpec 仓库读取的产品路径；不假设已经存在工蜂 adapter 或用户身份映射。

## 环境与配置
- OpenSpec 1.16.0；Node >=22.12；`OPENSPEC_TELEMETRY=0`。仅执行席/CI 工具，不并入 Hub 构建链。
- 仓库配置 `openspec/config.yaml`；真实规格从仓库指定不可变 commit 读取，不读取会话记忆作事实。
- 工蜂 API 部署域名、凭据管理位置、权限映射、限额配置名称由 prodops 核实，见 R5 proposal 的接口清单；未设定或提供任何凭据值。

## 依赖
- 上游：Console 登录/身份映射、工蜂有效读取权限、固定 commit 文件/tree API；prodops 提供接口事实。
- 下游：后续 R6 坐标派审、R7 确认版本、R8 状态、R10 任务派单。本轮只定义 R5 浏览边界，不实现这些流程。
- [R5 proposal](../../changes/r5-render-openspec-readonly/proposal.md)；[旧 Spec 基线](../legacy-console-spec/spec.md)。

## 负责组
- Console 开发 coder56solzzc.zzc 单写；平台架构组审阅；prodops 核 API、权限与执行环境。

## 外部资料
- 固定版本决议、就绪计划链接见 R5 proposal「来源」；不使用 beta Stores 或会话之间转传源码。
