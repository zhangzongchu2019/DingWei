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

## 首轮复核返工（01a1bf0d）

- ZC-003：保持已有 login kind，三类键统一先加用途前缀再哈希：state:、started:、selection:。GET 回调只查询和消费 state:；POST 选择只查询和消费 selection:。不新增 kind，不改旧迁移 CHECK。即使请求 state 等于另一类型的完整明文键，也无法查到另一域；400 不得消费其他类型记录。
- ZC-003：保留有效期和浏览器绑定；伪造 started 回调后同 attempt 的第三次启动仍拒绝，真实 state 和选择票据仍能按原语义消费一次。撤掉 state 写入和读取前缀的同一刀必须让两格 GET 负例分别在启动守卫和选择记录未消费断言转红。
- ZC-003：合并 r56.2 d70016f3，连接页与网关使用 Referrer-Policy: same-origin，保留同源表单 POST 的浏览器 Origin；跨源飞书跳转不携带 referrer。ZERO-CONSENT 不抬版本，沿用 0.2.3.33。新增 Chromium 真表单回归连接页→飞书替身→选择页→POST→授权码换票，单目标无编辑输入直接完成；真实本地 Hub/临时 PG，不模拟 Origin 请求头。

### 真浏览器发现的 CSP 跳转链阻断

Chromium 会对表单 POST 后的跳转链检查原表单页面的 form-action。原 self 策略阻止连接页303飞书，以及选择POST后302回客户端。保持 default-src none、base-uri none、frame-ancestors none；连接页仅加入服务端OAuth适配器生成的飞书授权origin，网关再加入已校验且持久化的attempt客户端回调origin；选择页仅加入该回调origin。CSP不读取请求Host或未校验redirect，剥掉path/query，拒userinfo、非http(s)、空host、通配符和CSP分隔符，避免策略注入；仍保留Origin/CSRF/浏览器绑定与全部OAuth校验。来源为可信配置／已有校验结果，不扩大OAuth登记允许集合。两条CSP撤除变异由真Chromium分别命中连接跳转与选择完成跳转阻断。

## r56.2 新 tip 合并裁定（01a1bf6a）

本段覆盖上文首轮 CSP 连接页方案：合并 d5a6fbbca6cda0c0627342e8905372f052638b2a，保留 r56.2 的 NewHandler → authorizeOriginOf → connectPageCSPFor，连接页仅 self 与配置飞书 HTTPS 精确源，不包含 attempt 的回调／回环源；网关不再追加连接页 CSP。选择页继续只加本次已校验、持久化 redirect_uri 的源。defaults／自动命名／用途隔离保持原逻辑。

隔离飞书替身改为 HTTPS（与 r56.2 源校验一致），落地确认页面后由真实 Chromium 点击身份确认链接，模拟上游身份确认的独立页面导航，不改 Origin／响应头或拦截导航。连接页撤除变异改为撤 NewHandler 内 authorizeOriginOf；选择页撤除仍移除 a.Redirect。
