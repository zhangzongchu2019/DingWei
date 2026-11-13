# release-quality-gates Specification

## Purpose
本变更把已有三个质量闸从发布入口未调用改为每次统一测试均执行且非零立即失败，目的是阻止新增建库调用、未经登记的直接依赖和缺少前置负测的真实租户schema进入发布。涉及检查脚本、例外登记与迁移校验，不改变外部产品协议。

### ⑤ 约束与突破（SpecDoc §4.7.1）

| 栏 | 本次登记 |
| --- | --- |
| Violation | 违反§4.7.1a默认“依赖不增”：r53提交97671b3a引入github.com/mozillazg/go-pinyin v0.21.0，直接依赖8→9。 |
| Why Needed | internal/feishulogin/identity.go在飞书未提供英文名时，把中文姓名转为无声调拼音并只保留小写ASCII，满足r53已复核的身份显示名契约。本单仅补登记，不改变实现。 |
| Simpler Alternative Rejected Because | 标准库unicode只识别字符类别而无汉字读音字典；直接丢弃汉字会把有效中文名变成空值并拒绝登录；自带拼音表等于复制维护字典且增大代码面。r53 C实现已通过复核，替换算法违反本次不换实现的范围。 |

许可证：MIT，模块LICENSE版权2016 mozillazg；保留上游版权/许可文本，发行时遵守MIT notice条件。模块版本v0.21.0的go.mod仅含module与go1.11、没有require。
传递依赖清单：**空（0项第三方模块）**；产品源仅import标准库regexp、strings，字典随模块自带。许可证与依赖依据为本机Go模块缓存github.com/mozillazg/go-pinyin@v0.21.0/{LICENSE,go.mod,pinyin.go}及go.sum校验；不把主模块其它已有indirect项算作该依赖引入。
直接依赖集合与sha256严格按dependency-ratchet.sh相同排序/换行算法生成；基线文件登记9项，未改go.mod/go.sum。

未经执行的用例不得声称通过；保留边界闸原有债而不声称全部归零。

## Requirements

### Requirement: GG-001 三闸逐项可复核
发布测试入口 MUST 执行三个闸并传播任一非零退出；例外变更 SHALL 列明路径和原因，不用改基线数字隐去增量。

#### Scenario: GG-001-S01 未登记建库调用
- **WHEN** 一次性树中在非例外路径新增一处建库调用
- **THEN** 统一测试入口非零，撤销变异后恢复为零

### Requirement: GG-002 schema触发负测
租户闸 SHALL 只把真实DDL租户列当触发条件，请求或JSON字段不得冒充schema；每个命中领域 MUST 同时提供四项乙组负测。

#### Scenario: GG-002-S01 请求字段与真实DDL分开
- **WHEN** 只有请求字段，或新增含租户列的表
- **THEN** 前者不触发、后者缺任一领域标记即非零

### Requirement: GG-003 两处拒绝最小审计
extseat跨tenant签发拒绝和store元数据前置拒绝 SHALL 在既有审计表记源主体、派生租户、操作、deny判定及服务时刻；不得记录调用方tenant、目标、令牌或正文。审计失败 MUST 仍保持原拒绝响应。

#### Scenario: GG-003-S01 不解析拒绝目标
- **WHEN** 调用方不满足签发tenant或入站元数据前置条件
- **THEN** 资源解析spy为0；存在与不存在目标返回相同拒绝并产生同形最小审计，审计失败也不放行

#### Scenario: GG-003-S02 删除审计必须被负测发现
- **WHEN** 一次性树删去其中任一审计写入
- **THEN** 该域最小审计负测转红

### Requirement: R58I-001 r58冻结输入组合验证
r58集成 MUST 从153bac4d按派单顺序保留五项原tip各一个no-ff merge，SHALL 实测并记录helper库存与expected/actual。最终tip MUST 通过全仓Go、hosted race三轮、canonical双profile、全量浏览器和strict，并分别撤防根绑定与Claude归属以证明判据有效。不得改变版本位或部署。

#### Scenario: R58I-S001 最终组合交审
- **WHEN** 五项合并且计数钉值已核
- **THEN** 所有门在同一最终tip执行，交件包含五个merge、tip/tree、读数、跳过原因及快进推送后远端一致证明

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
