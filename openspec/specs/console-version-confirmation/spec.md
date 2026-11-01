# R7 最小基线

## Purpose

本变更把旧数据库规格审阅的版本边界从隐含实现改为可核查的最小基线，目的是避免把旧修订号、正文摘要与 Agent 请求误认为 Git 不可变版本治理已存在。涉及 Console 与旧审阅存储模块，不影响现有发布及部署流程。

此基线依据 d1a7a686 源码；新 Git 能力仅在对应 changes 中提议，未实现、未验收。

## Requirements

### Requirement: R7-B001 旧记录不代表 Git 版本确认或审阅
平台 SHALL 保持旧数据库 spec_id、revision、content_sha256 与 Git 仓库/变更/commit 语义分离，不得把旧审阅或正文摘要当作新 OpenSpec commit 的确认或审阅证据。

#### Scenario: R7-B001-S01 旧记录按旧语义读取
- **WHEN** 读取一个只有 spec_id、revision 和正文摘要的旧 Agent 审阅记录
- **THEN** 其来源为旧 Spec 修订，不具有 Git 仓库/commit 的有效确认或审阅资格

#### Scenario: R7-B001-S02 不能用旧通过记录替代新证据
- **WHEN** 同名 OpenSpec 变更需要固定 commit 的记录而只有上述旧通过记录
- **THEN** 不得把旧记录解释为该 commit 已获确认或已审阅
