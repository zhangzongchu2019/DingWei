## Context

依据㊿-补29，基线main fce2ede85150747cfa7ccdf728f060d1e498304a。授权开始仍逐字绑定client、redirect、PKCE、resource、scopes和浏览器。

## Goals / Non-Goals

减少接入输入，维持两成员AG和四通讯scope。不部署、不扩大普通Agent名称、不加DCR、不改已发布迁移。

## Decisions

- ZC-001：新增DW_MCP_CONSENT_TARGETS_JSON，JSON对象，key为tenant，value为普通目标全名数组，最多64项、拒重复和非法名称。缺席/空集合保持原手填两项；手填仍限8，关闭功能只停新增长名。外部内部解析与PG可读写1～20。
- ZC-002：可信user_info后生成名字：英文清洗小写→中文拼音首字母→pm，名字截8位，加UTC yyMMddHHmmss。复用已钉go-pinyin v0.21.0，不加依赖。state一次性消费，同attempt不会再次生成；多选期间名字保存于加密state。
- ZC-003：恰一配置目标现核有效则授权页只有同意按钮，无可编辑输入。多配置目标先同意去飞书；可信profile后，才查询该主体最近一次成功签发目标，并在现核有效的允许集合中单选。没有有效目标拒绝。选择表单用不可猜的一次性selection票据，网关将其绑定原attempt和浏览器cookie；回调POST不收主体/短名，自身仍现核目标。
- ZC-003：最近目标只作偏好，取既有PG主体/席位/AG关联，不按显示名合并；先验租户再查。签发前现核存在、用户和Agent active、非external、仍在允许集合。原extseat事务及AG成员检查继续保留。
- ZC-004：0108新增CHECK；旧数据不变。存在长名行不得恢复旧CHECK；回旧二进制前先封口所有长名席位、处理确定未执行投递并保留墓碑/ACK。关配置不作数据回写，必须前向修复。

## Risks / Trade-offs

多目标需要飞书回调后再做一次单选，以获得可信主体的最近偏好，不能在未认证时用自报名字查询。配置由启动时读取，变更通过新配置启动生效；本单不提供在线修改接口。官方桌面未测，不把CLI验证当桌面验收。

## Migration Plan

先新PG迁移，再新代码；现网集合仍缺席，默认关闭。详见docs/reviews/mcp-zero-consent/RELEASE-NOTES.md。
