# 旧 Console Spec 最小基线

## Purpose

本变更把旧 Console Spec 的基线从零散代码改为可追溯的最小行为记录，目的是为冻结切换提供对照且避免把旧需求当成当前事实。涉及 Console 页面、鉴权和规格存储模块，不影响现行部署与任务执行。

本文件描述 a8bb5e5847884b62ba01f6f48fbce3772e2d8b04 的代码行为；未独立验证现网，R11 增量尚未实施。

## Requirements

### Requirement: LCS-001 旧规格写入控制
系统 SHALL 按现有 Console 身份、编辑 capability 或 Agent 专属凭据校验旧规格的新建与修订权限，并在修订时校验预期版本。此基线仍允许获得权限的调用者写入。

#### Scenario: LCS-S001 合法编辑者追加修订
- **WHEN** 当前编辑者提交合法正文、验收项、来源锚和匹配的预期修订号
- **THEN** 系统生成新修订及正文摘要，并保留旧修订

#### Scenario: LCS-S002 无 Agent 身份直接写入
- **WHEN** 未携带有效 Agent 专属凭据的调用者请求 Agent 直写入口
- **THEN** 系统拒绝请求且不创建规格

### Requirement: LCS-002 旧规格读取与入口
Console SHALL 提供规格工作区入口与按 principal 读取的列表，默认隐藏已归档规格，并按规格编号展示正文和修订信息。现有共享链接另有独立读取语义，不等同于仓库权限。

#### Scenario: LCS-S003 列表过滤
- **WHEN** 已登录用户请求自己的旧规格列表且未指定包含已归档项
- **THEN** 返回该 principal 可列出的未归档规格，不混入他人的私有列表

### Requirement: LCS-003 旧格式生成与审结门
系统 SHALL 在现行旧规格编辑、review 和 final 流程中检查验收项、来源和适用的模板、生成审阅及审结条件；条件不足时拒绝定稿并显示缺项。

#### Scenario: LCS-S004 未满足审结条件
- **WHEN** 当前编辑者尝试把不满足生成审阅或逐项验收条件的旧规格定稿
- **THEN** 系统拒绝定稿，规格不进入 final
