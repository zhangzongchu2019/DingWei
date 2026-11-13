## ADDED Requirements

### Requirement: R59I-001 冻结父关系与最小冲突合并
r59集成 MUST 从c0fecb12建立新分支，以no-ff合并e0de23713606d3e89f1bf578d268550e70d4786d且保持第二父逐字一致。renderer MUST 同时保留busyWait原输入norm与等待文案、View未知条目时间/隐藏与visibleStageText；SHALL 记录合并blob及与组长试合的可核比对，不伪称缺失对象已比对。

#### Scenario: R59I-S001 两处交叉冲突
- **WHEN** 两冻结tip合并只在renderer的norm与stageText处冲突
- **THEN** 解法保留两链语义，目标浏览器20项与26项均通过，并记录准确merge父关系

### Requirement: R59I-002 版本位独立提交
集成 SHALL 在merge之后独立普通提交将VERSION、internal/version/version.go、internal/version/helper_0233_test.go与helper __init__.py同步为0.2.3.39；MUST 不把版本号写入页面源码，不构建发布件或部署。

#### Scenario: R59I-S002 版本一致性
- **WHEN** 合并完成后抬版本位
- **THEN** 版本提交只修改同r58 c5f0c101的四个文件，根/Go/helper钉值一致

### Requirement: R59I-003 统一隔离门与可追溯交件
最终产品树 MUST 在env -i、Go1.22.2、Node22、lock venv、HUB_TEST_PG_STRICT=1下通过两种PATH完整run-build-tests（含无codex/claude）、internal/m8全包及cmd/workpulse/internal/version/mcpgw、无选格全量浏览器runner、OpenSpec strict与diff检查。SHALL 记录每profile expected/actual、Go PASS/FAIL/SKIP及跳过名、浏览器aggregate_rc、执行环境与完整日志hash；推送后核远端同值。可选截图输入 SHALL 仅登记环境键名，不设置该变量或绕过覆盖自检。

#### Scenario: R59I-S003 全部读数完成
- **WHEN** 全部指定门结束且产品树在验证期间未改
- **THEN** docs/reviews/r59-integration包含REPORT/raw/SHA256SUMS、父关系与真实读数；未跑/未完成或跳过不得冒作通过
