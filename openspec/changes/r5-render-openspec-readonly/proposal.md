# Proposal

## Why

现有 Console 只会读取数据库旧 Spec，不能把仓库 OpenSpec 的固定版本直接呈现给研发成员。全面切换要求以 Git 内容为唯一依据，因此必须补齐继承仓库权限的只读浏览，并在内容进入页面前校验仓库坐标、路径和资源边界，防止仓库文本、恶意链接或远程图片绕过授权与浏览器安全约束。

## What Changes

- 在 Console 中读取经授权仓库、完整 commit SHA 下的 OpenSpec 文档，并持续显示仓库/版本/路径；分支仅用于选取版本，显示结果必须钉住 commit。
- 只读呈现主规格、context、进行中与归档变更的 proposal/design/specs/tasks；保留 tasks 勾选原状，不允许 Console 写回，不由勾选或 archive 判定完成。
- **BREAKING**（仅新 OpenSpec 阅读区）：收紧 HTML、链接、图片和路径规则；不自动加载远程图片，不以现有公共渲染器直接输出作为新安全契约。
- 补 `console-spec-reading` 代码最小基线/context 与增量；不实现 R6～R10、能力时间线、Git 写入或新的旧格式编辑器。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `console-spec-reading`：扩展 CSR-001 到不可变仓库坐标读取，对新阅读区替换 CSR-002 的安全策略；新增 CSR-003～007 的权限、路径、失败边界和只读语义。最小基线本轮补齐，增量仍是未来行为。

## Impact

涉及 Console 新阅读区、服务端工蜂只读适配与授权、Markdown 输出及文件/tree 读取；不将 OpenSpec CLI 引入 Hub 构建链，不把源码送入文件服务或消息正文。依赖 prodops 提供工蜂 API 事实与用户/Agent 权限映射。规格审过之前不编写产品代码，不把待确认接口当成已可用接口。

## 来源

唯一需求来源为委员会决议：[OPENSPEC-SWITCH-DECISION-20261031.md](https://git.code.tencent.com/wego-ai/dingwei/blob/c2d9c9fdd0416828739f7042d2ceb9bc71f603c6/docs/decisions/OPENSPEC-SWITCH-DECISION-20261031.md) 第 4～6、10～15、17、19～22 条。
就绪范围：[就绪计划 R5](https://git.code.tencent.com/wego-ai/dingwei/blob/c2d9c9fdd0416828739f7042d2ceb9bc71f603c6/docs/decisions/OPENSPEC-SWITCH-READINESS-PLAN-20261101.md)。来源 commit c2d9c9fdd0416828739f7042d2ceb9bc71f603c6；实现基线 a8bb5e5847884b62ba01f6f48fbce3772e2d8b04。

## 待确认假设

以下是对 prodops 的接口需求，不是已获授权/已存在的 API；需回写已核文档链接、能力、限制和凭据管理位置，不发送凭据值。

- R5-A01 仓库发现/标识：受控工蜂实例、稳定仓库 ID 与规范 namespace/path 的对应查询；只列当前主体可见仓库。需要确认重命名、迁移、删除的行为，不接受用户提交任意 host/API URL。
- R5-A02 身份/权限：Console principal 与工蜂用户的可信映射；查询该用户对仓库的当前 read 权限（含继承、组、撤权、停用）；后续 Agent 读取也必须能验证其独立仓库权限。服务账号读到内容不等于用户获权；尚未证实可逐主体可靠核权时不能开放该读取。
- R5-A03 版本接口：把受控仓库的 ref/tag 解析成完整 commit SHA，并验证 commit 属于该仓库、可读取；查询响应必须回显实际 commit，不以可移动分支名作缓存身份。
- R5-A04 内容接口：指定 commit 的 tree 列举/分页、路径类型与 blob 标识、原始 UTF-8 文件读取；必须可识别 symlink/gitlink，不能自动跨 submodule。需确认 API 编码/错误/限流/大小约束与重定向策略。
- R5-A05 运维约束：工蜂访问凭据由谁持有、最小只读 scope、secret 管理位置、轮换/撤销与审计方式；授权检查不可用时拒绝，不跨用户使用缓存。需确定超时、并发、正文/tree 大小与缓存清理上限。
- R5-A06 现有 Markdown 规则可被新阅读区单独收紧；不假设公共渲染器能无改动满足仓库相对链接与远程图片限制。是否展示附件不在本轮范围。

## 待 PM 决策

- R5-PM01 图片体验：选项 A 首期全部显示替代文本，禁止图片网络加载；B 允许同 commit 且独立授权校验的仓库图片。建议 A，最小就绪范围更易证明安全；远程图片自动加载两案均禁止。
- R5-PM02 浏览边界：选项 A 首期仅 `openspec/` 内 Markdown 文件；B 放开仓库其他文档。建议 A；外部来源链接仍可按链接安全策略人工打开，不能成为平台任意抓取入口。
- R5-PM03 内容/tree/时间限额：选项 A 按 prodops 实测给出有界默认值并在界面说明；B 无上限。建议 A，且单份合法 70 KiB 文档可成功读取，不能复现 64 KiB 消息截断。精确数值与配置名待 API 实测回写后冻结。
- R5-PM04 权限映射：选项 A 可信用户身份映射后逐主体核权；B 仅凭平台项目成员关系使用服务账号内容。建议 A；B 不满足继承仓库权限，不能作为默认兜底。
