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
