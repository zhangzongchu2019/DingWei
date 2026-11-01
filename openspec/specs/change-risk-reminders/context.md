# 风险提醒上下文

## 代码位置

- 基线 dfc95fd8b6b2a063edd77f22bd38ecc63b729213，仅核源码，不代替现网验证。
- `internal/model/model.go` 的 Risk：通用风险内容/状态/关联任务，尚不是仓库七类判定和双端提醒。
- `internal/m8/hub.go`、`console_platform.go`、`cmd/workpulse/console_routes.go`：可信主体与 Console 路由边界；未发现 R1 的规则纯函数或 MR 评论适配。
- `internal/specconfirm/confirmation.go` 的 Coordinate/Authority 与 `policy.go` 的 SnapshotReader：已有提供方中立的固定坐标/可信授权边界，可参考而非把快照读取器冒称 diff/评论 API。
- 新代码落点与规则文件 schema 待实施设计核实；本单不添加产品文件或 `risk-paths.yaml` 运行配置。

## 环境与配置

- OpenSpec 1.16.0；Node >=22.12；`OPENSPEC_TELEMETRY=0`，不进入 Hub 构建链。
- 各仓规则位置 `openspec/risk-paths.yaml`；平台默认表见变更 design。规则版本包含平台表、仓库文件 blob 与匹配语义版本，实际字段待核。
- dingwei 使用 GitHub、业务仓使用工蜂；只读 diff/标签权限与写 MR 评论权限分别核实，secret 仅记管理位置，不读写值。

## 依赖

- 上游：可信仓库托管提供方 diff/标签/评论/合并时刻与逐主体权限、变更负责人映射；提供方接口能力不能从 R7 模拟测试推定。
- 同级：[R1](../../changes/r1-identify-high-risk-changes/proposal.md)、[R8](../../changes/r8-evidence-driven-change-lifecycle/proposal.md)。
- R1-PM01：业主2026-11-01T04:0xZ经workpulse批准A，由组长转达；同commit标签/规则变化更新原提醒并追加修订，命中集合变化重置该修订知悉。实际评论更新/查回/权限及规则接口仍待核，R1／R8实现排r45另派。
- 下游：R9 处理 PM 决策请求；风险已知悉不作为 R8 委员会确认或 PM 回应。

## 负责组

- coder56solzzc.zzc 单写；opus50tc2k.zzc 审阅与冻结规则；prodops 核提供方接口/权限；togpt60lunazzc 执行独立验收。

## 外部资料

- 决议、草案、月计划、QA 风险专项固定链接见 R1 proposal 来源；本能力不上传源码至文件服务。
