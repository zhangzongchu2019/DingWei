# View 页级解锁 v2 · §10 E2E 验收清单（9 条）

> **性质**：本清单用于把 `view-service-page-unlock-architecture-spec.md` 的
> **§10「回归与 GO 门槛」**操作化为可执行的端到端验收用例，
> 验收判据如下，可据此构建测试 harness 并核验结果。
>
> **它【覆盖】spec §10 的属性门槛，将其转成可执行 E2E；它【不是】spec §10.4 的原文，
> 也不替代或覆盖 spec 的任何章节。** spec 的 §10.4 指「生命周期」，与本清单 E9(a)/(b)
> 对应但表述不同。执行判据以本清单为准；属性定义以 spec §10 为准。

统一前置(所有条共用):真 Go Hub 二进制 + 真 Python VS 进程 + 真 helper+CLI(至少1个测试会话S);v2 capability on;S路由到v2 target、对照会话使用v1;双向secret(hub_to_vs/vs_to_hub,≥32B,建议encrypt_key)配好;owner O 的飞书open_id绑定到S的owner。每条"失败信号"命中均须先修复。

━━━ E1 · 配置接线 + owner解锁happy path(覆盖①)━━━
前置:浏览器打开S的view URL→VS建locked页P(拿到page_id/token/code)。
步骤:O用真飞书发 `#unlock <P.code>`。
硬断言:①飞书回"已解锁页面输入：S" ②VS view_pages[P].state==unlocked、grant_owner==O ③审计行 result=granted 且 sender_owner==target_owner==O ④VS未对该unlock返401(=双向secret/路由/capability接线正确)。
失败信号:飞书回"拒绝解锁…"/VS 401(→①secret或config错)/404页面码不存在(→路由或code错)/state仍locked。★这条绿=①配置接线通。

━━━ E2 · owner安全:冒充全拒(§10.1)━━━
前置:P属O;另一账号A(不同飞书open_id、绑不同owner)知道P.code(公开)。
步骤:A发 `#unlock <P.code>`。
硬断言:①飞书回"拒绝解锁：发信人不是该会话owner" ②VS state不变(仍locked)③审计 result=rejected ④Hub**没有**向VS POST过unlock(VS view_control_commands无此条)。
失败信号:P变unlocked / VS收到过该unlock POST。

━━━ E3 · 群空发信人 + 未验签入站 全拒(🔴1+🔴2信任根)━━━
前置:v2-enabled channel。
步骤:(a)构造/重放一条 ChatType=group 且 SenderOpenID空 的 `#unlock`;(b)伪造一条无/错飞书签名的 `#unlock`(未验签入站)。
硬断言:(a)拒"群消息缺少可信发信人"、**绝不**用群id解析owner;(b)拒"消息来源未通过可信入站认证"(ingress_provenance=untrusted);两者state均不变、审计reason对应。
失败信号:任一路径解锁成功 / (a)从群id解析出owner / (b)未验签消息被当webhook_verified。
(注:此条难用真飞书触发,可作Hub定向集成测试,但**必须有**——它守的是整个owner模型的信任根。)

━━━ E4 · ★input真到CLI出输出(覆盖②,关键)★ ━━━
前置:P已按E1解锁;真helper+CLI在线;浏览器持P.token。
步骤:浏览器 POST /input {text:"echo DW_CANARY_<uuid>", page_id, page_token, request_id=<32位hex>}。
硬断言:①/input返 ok:true ②该 DW_CANARY_<uuid> 标记在**N秒内真出现在会话CLI输出/SSE事件流**里(证明走通 VS authorize_page→反向HMAC→Hub HandleInternalViewV2Input→routeViewServiceInput→helper队列→CLI)③Hub审计/日志见该 request_id 一次投递。
失败信号:ok:false / 标记**从不出现在CLI输出**(→②input没落到CLI)/ Hub 401(→反向secret错)/ 出现两次(重复投递)。★这条绿=②input到CLI通。

━━━ E5 · 页级隔离:别浏览器/未解锁页不能写(§10.2)━━━
前置:P已被O解锁。第二个浏览器(无P.token)打开S→得新locked页Q。
步骤:第二浏览器用Q.token(或无token)POST /input 带唯一标记。
硬断言:①ok:false ②该标记**不出现在CLI输出**(Hub/CLI零收到)③Q仍locked ④P的grant不受影响。
失败信号:第二浏览器的输入到了CLI / P被牵连撤销。

━━━ E6 · 一人多页独立(§10.2)━━━
前置:O开两tab→页A、B(page_id/token不同,BC分身)。O分别 `#unlock A.code`、`#unlock B.code`。
步骤:经A输入标记mA、经B输入标记mB(交错)。
硬断言:①mA、mB**都**出现在CLI输出 ②A、B**始终都unlocked**(无"写权转移走")③各自独立鉴权。
失败信号:用B时A失写(或反之)/ 只一个标记到CLI。

━━━ E7 · 复制tab不继承写权(§10.2/§5.1 BC)━━━
前置:O在tab1有已解锁页A。Chrome"Duplicate Tab"克隆(sessionStorage被克隆)。
步骤:两tab加载后各尝试输入。
硬断言:①两tab最终page_id**不同**(BC字典序让位)②**恰一tab保留A的unlocked grant、另一tab落到fresh locked页(不能写)**③解析后token不共享。
失败信号:两tab共享A.token且都能写(超过~250ms瞬时窗口后仍如此)。
(需由真浏览器自动化与手动复现双重确认。)

━━━ E8 · ★#1 churn→重解锁不first-fail(覆盖③,原始bug)★ ━━━
前置:浏览器已展示S的页P(code可见);helper WS在线。
步骤:①模拟helper WS断连+重连(kill helper的WS/重启helper→Hub对旧连接跑 closeTerminalLocked);②重连后,O用**churn前那张同一P.code**发**一次** `#unlock <P.code>`。
硬断言:**这一次#unlock就成功**——飞书回"已解锁"、VS state→unlocked、**不报"页面码不存在/未找到页面码"**(码存VS SQLite、不随hub churn丢)。
失败信号:churn后第一次#unlock返404"页面码不存在",**要第二次才成**=first-fail复现=#1没真修好。★这条绿=③#1端到端修复被证。

━━━ E9 · 生命周期 + 协议故障 + legacy不回归(§10.4/§10.5)━━━
分测(全硬断言):
(a)断连撤权:P解锁→浏览器断(pagehide beacon或断SSE)→过disconnect_grace(默认5min,测试可调时钟)→**sweep daemon一个周期内**P.state→revoked→此后该token输入 ok:false。失败:>grace仍能写。
(b)续期仅input:P解锁后只发/status与SSE ping(不input)超过8h→P过期不可写;而持续input则续8h。失败:status/ping延长了grant。
(c)HMAC故障拒:错签/过期ts(>±60s)/重放同nonce/target不符 → Hub与VS均拒(401/409),不改状态、不投递。
(d)Hub离线input:Hub down时/input→ok:false、明确失败;Hub恢复+同request_id重试→**不重复执行**(helper按delivery_id=request_id幂等)。失败:双执行。
(e)legacy不回归:非v2会话/4位短码/老xterm 全量走原路径不变;关闭 capability 后页面默认 locked、须走v1短码,VS page token绝不转成Hub writer token。

━━━ 判据总纲 ━━━
9条全绿(尤其★E1配置接线/★E4 input到CLI/★E8 churn不first-fail 三条硬)=§10整体GO=报示例用户扩量。任一★挂=停,先修再验。非★条挂=评估严重度,安全相关(E2/E3/E5)挂必停。
验证范围纪律:仅测试会话路由v2、主力保v1、全程capability-gated、任何失败fail-closed退回v1短码(不牺牲安全换可用)。
