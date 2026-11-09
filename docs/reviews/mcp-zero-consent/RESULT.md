# 首轮返工入口

最新返工见 [rework-01a1bf0d/RESULT.md](rework-01a1bf0d/RESULT.md)，冻结源10e6c2213，已合入r56.2并补键分域与真实浏览器。下方原报告及原SHA清单保留为c85b4e93历史读数。

# S-MCP-ZERO-CONSENT 作者交审结果

执行会话：01a1be21-a016-7ff1-bf0f-a48b78f9d3d6；唯一写锁 `/tmp/coder56-mcp-zeroconsent.lock`，取得于2026-11-09T06:06:49.388462+00:00。基线严格为main fce2ede85150747cfa7ccdf728f060d1e498304a；依据rulings ㊿-补21、补29＠5641584ae1eeb83e199f80608019e3b89e855758及本次改派令。**作者自测门通过，交组长独立复核；不是部署或桌面验收通过。**

## 行为与权限

- 可选服务器配置 `DW_MCP_CONSENT_TARGETS_JSON` 按租户给普通目标允许集合。配置缺席/为空保留原两项手填；配置非法、重复（含大小写别名）或超过64项拒绝，不从在线清单推断目标。本单不写现网集合初始值。
- 单配置目标现核有效时，没有可编辑输入框，只有同意并进入飞书按钮，并显示协作目标与四项通讯权限。多个配置目标时，先完成飞书身份验证，再显示一次受控单选，仅列现核有效的允许目标；预选同一可信主体最近一次成功且仍在集合内的选择，不按显示名猜主体。没有有效目标就拒绝。
- 原OAuth attempt、浏览器cookie、CSRF、PKCE、client_id/redirect/resource/scopes绑定保持。不同页签可拿到不同CSRF，但同一外层attempt只有一个Start可成功关联。选择票据一次性、受原期限约束、与浏览器绑定；签发前再现核目标存在、active、非external且仍在集合内，原AG事务继续锁目标和属主。只建原双成员AG、四通讯scope。
- 仅可信飞书profile后自动命名：英文去非ASCII字母并小写→中文拼音首字母→pm，名字截8位，加UTC yyMMddHHmmss。同attempt仅生成一次；多目标选择保留这次名字。同秒同名不加随机后缀，靠不可复用全局AG序号区分；主体按provider/tenant/issuer/subject，不按姓名合并。
- 外部校验放宽到20；新增PG `0108_extseat_short_name.sql`。0105/0107等已发布迁移、普通Agent/用户简称及VERSION未改；原手填入口继续限8，使功能关闭只停止新增长名。最长41位外部Agent名、58位全名有真实PG/路由/在线清单覆盖。

多目标放在飞书回调后选择，是为了先获得可信主体再查询最近目标。允许集合仅在启动配置中读取；没有新增在线变更接口。详见LENGTH-AUDIT.md、DEPENDENCY.md与RELEASE-NOTES.md。拼音复用缓存中既有go-pinyin v0.21.0（MIT），未加依赖，GOPROXY=off编译通过。

## 实跑门与原文

官方go1.22.2，普通用户uid1001。Go门使用隔离HOME/TMPDIR与真实临时PG，HUB_TEST_PG_STRICT=1、GOPROXY=off；canonical使用锁定venv，构造干净环境剥离SH_、DINGWEI_和PYTHONPATH。完整命令/UTC/环境变量名/rc见各raw目录的JSON；逐项跳过名与条件见raw/SKIPS.json。

| 包/门 | PASS / FAIL / SKIP |
|---|---|
| mcpgw | 229 / 0 / 0 |
| extseat | 27 / 0 / 0 |
| extshared | 6 / 0 / 0 |
| extwire | 13 / 0 / 0 |
| feishulogin | 96 / 0 / 0 |
| store | 599 / 0 / 5 |
| store/migrate | 43 / 0 / 0 |
| identity | 215 / 0 / 0 |
| m8全包 -p 1 | 1512 / 0 / 37 |
| workpulse | 259 / 0 / 6 |
| Consent相关race -count=3 | 165次目标PASS，无DATA RACE |
| build.sh --test-only | rc0；claude/codex各1129/1129，91文件；27 Python、2 shell；BUILD_TESTS_OK |
| OpenSpec validate --all --strict | 29 / 0 |
| 定点变异 | 30/30目标红，逐刀日志与目标断言见raw/mutations-final |
| 官方CLI 0.167.0 URL-only原生E2E | native rc0、Hub rc0；授权页无编辑输入；登录、工具发现、list_online成功 |

全包命令原文见raw/packages-final/packages.json；M8见raw/m8-final；最终受影响三包全量补跑见raw/packages-final/affected-final.stdout。产品首冻f37c6fe8802fbe8ac9cdf6b206576180a918f76b；9666/fab为测试清单与测试访问上下文修正；最终123e0e6追加“已持久化自动意图遇nil默认策略时拒绝”的防护。最终受影响包、race、变异和原生E2E均在123e0e6上重跑；其他未变包沿用完整通过结果。原生探针使用模拟飞书＋真实临时PG，无真实凭据、无线上席位、无推理turn；只复制白名单日志，不复制测试HOME/CODEX_HOME、凭据、密钥或env文件。

## 四项判据与变异归属

| 判据 | 正反例 | 定点撤除/放宽 |
|---|---|---|
| 长度与旧数据 | 旧行不变、20位可写、21位/非法字符拒绝、最长58位往返、普通规则不变 | external-length、external-long-parse、migration-upper、migration-old |
| 默认关闭与租户集合 | 缺席/空/他租户保留原表单，单有效目标无输入；无有效者拒绝、客户端覆盖拒绝 | disabled-form、single-input、manual-length、default-overrides、config-tenant |
| 目标与命名 | 多目标过滤/最近主体隔离/选择重放/撤销/过期/错误浏览器；多音字、空名、符号、超长、UTC、同秒同名、同attempt并发一次 | 其余目标/名字/浏览器/期限/outer-attempt-once各刀，见RESULT.json |
| 回退边界 | 关配置不改已有长名；存在长名时恢复旧CHECK真实失败且保留新约束；封口/待投递/墓碑/ACK成文 | rollback-close、rollback-pending、rollback-check；这些是发布说明合同撤除，不冒称已做真实旧二进制降级 |

## 首轮问题与作废读数

- 早期测试替身tenant与英文名不符预期，修正测试数据后重跑；未把错误夹具算产品通过。
- 初选用新OAuth kind被既有CHECK拒绝；改用已有login命名空间并以selection:做哈希域隔离，不改0107。
- 新迁移使账本固定名单/条数断言过期；已更新migrate、workpulse及真实设备认领链路清单。canonical第一次真实认领PG格的红因此作废重跑，不吞错。
- 新PG测试新增SystemCtx调用点触发棘轮；改成NewAccessCtx测试上下文，未调宽闸口。
- 一次复制测试片段带入重复辅助类型，编译失败已修；首轮subject-active变异因unused变量编译失败不计，修正变异后全部30刀目标红。
- 一次探针目录与历史目录重名，启动失败，不作为E2E证据；启动器已增加拒绝复用目录保护，最终使用独立目录。相关可用失败原文单列raw/superseded；最终门无FAIL。

## 风险与发布边界

桌面版仍**未测**。CLI原生通过不能代替业主桌面验收。不同于固定精确ID的chatgpt域下带12位段client_id仍不获前缀放宽，会红在invalid_client或落入在线获取（rulings补23/27口径）；本单未改该策略。

功能默认关闭；现网允许集合须组长另定并经业主确认。关功能不等于可回旧二进制：必须先封口全部长名席位、处理确定未执行投递，保留墓碑/ACK；存在长名行不得恢复旧CHECK，只能前向修复。发布单专项验证清单见RELEASE-NOTES.md，本席未执行部署、重启、真实降级或现网配置变更。

估算宽区间：实现自测3～7人日，独立复核/发布专项1～3人日；考虑环境与真实桌面差异，总范围4～12人日。当前自测完成不折算为他席复核或生产验收完成。M3必补排后，开工须另抢M3锁，不碰原持锁者工作树。
