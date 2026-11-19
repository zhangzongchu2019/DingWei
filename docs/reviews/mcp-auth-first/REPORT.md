# S-MCP-AUTH-FIRST 实现交件

## 对象与状态

分支coder56solzzc/mcp-auth-first，基main206d41b26dab646c0e50a111eed5a45ccf7c72f6。设计cec23819e先提交；固定产品/测试源ce9b7a959e828ad717b8708651c9094a421272ec，tree1a1e9e174722f45acfd4520182800382abf47894。仅改internal/mcpgw与internal/feishulogin十一件（五产品、六测试），无迁移/版本/依赖/业务工具改动。交件docs tip不改变产品对象。等待qwen（GROUPS之前）与组长复核，不自验收、未推main、未部署、不产发布件。

派单来自2026-11-19T03:01:23Z dw-dlv_a17e4b9ea5895bdcd9cdb4ddf8eb8476。截图是业主/组长提供的现网线索，本席未连接dw123、未跑真实Antigravity或访问其账号、未使用现网凭据。本地真实HTTP只替代传输，不冒称实际CLI验收。

## 改动及安全边界

gateway.go的/mcp改为直接注册mcp鉴权入口。该入口先设置no-store等响应头，再验证Authorization和调用Authenticate：缺bearer、错误/过期/吊销bearer优先401，WWW-Authenticate仍为配置PublicOrigin的protected-resource URL及既有scope；不受请求Host、版本、方法、Origin、Content-Type/Accept或JSON影响，不读取body，不反射未经验证的JSON-RPC ID（错误id为null）。结构无效Authorization不进入Seats；结构有效bearer每HTTP请求只调用一次Seats.Authenticate。

鉴权成功后，同一请求局部Seat经闭包交给原guard→mcpAuthorized→codec。无全局token、session或握手授权快照，后续HTTP请求再次鉴权，过期/吊销立即挑战。其它路由的Origin/鉴权顺序未改；有效token但tenant/AG/权限不允许仍沿用原403，不错误促使重复登录。

按派单“任何未带有效bearer的/mcp请求先401”落实：非法Origin未鉴权先401，**已鉴权非法Origin仍403**且不进入codec/业务。这是有意的错误优先级变化，不把它说成MCP官方要求Origin必须在认证后。合法请求的原POST-only、媒体类型、双Accept、体积界、已知字段、参数/capability对象校验、tenant/scope/audience/AG/幂等与业务权限不放宽。错误bearer的unauthenticated/expired/revoked映射与challenge逐字保持。

有bearer但格式错误的请求现在会先完成认证检查，可能按既有Authenticate策略触达token；它仍不执行业务。这是鉴权先行的预期顺序变化，不新增资源访问能力。

## A-AUTH-01 版本协商与形状

本片**未宣称完整支持03-26和11-25**。完整标准codec仍为2025-06-18，私有2026封套不变。03/06/11及未知非空版本的标准initialize都回06-18；客户端必须按返回版本继续，不支持返回版本的客户端应断开。因此“协商成功”不等于证明Antigravity一定支持06-18或其实际接入已验收。

| 方法 | 03-26差异 | 06-18 | 11-25差异 | 本片行为 |
|---|---|---|---|---|
| initialize | 版本/capabilities/clientInfo | 可选title | 可选description/websiteUrl/icons及新client能力 | 返回06-18，只广告tools；接收已知info但不反射、不按URL取件 |
| notifications/initialized | 无id，202空体 | 同 | 同 | 按协商06处理 |
| ping | result空对象 | 同 | 同 | 按协商06处理 |
| tools/list | 无structured输出schema | 可选outputSchema | 可选任务增强相关信息 | 维持原九工具，不加任务/资源能力 |
| tools/call | content/isError及旧batch传输 | content加structuredContent，不再batch | 可选tasks，不是自动启用 | 维持06结果，不实现batch/tasks |

03全支持需补旧batch的请求/响应与资源界、相应旧结果codec；11全支持须逐项评估新tasks/elicitation等可选能力。不能只把支持日期列表写成三个就当完成编解码。这些额外功能未在本片实现。

无MCP-Protocol-Version且无其它版本识别依据的后续标准请求按03-26处理，因操作codec不支持它而返回明确400/-32022，不误进私有HeaderMismatch。initialize从params提议版本协商，无头不阻断。已协商客户端应使用返回的06头；继续使用03/11操作头明确400。显式06头维持原优先分流（不会因为多一个无关vendor头误进2026）；私有2026头/meta/Mcp-Method封套保持原精确匹配与未知字段拒绝。标准初始化的已知客户端info扩展字段不会用作权限。

官方依据（2026-11-19只读核）：[Lifecycle](https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle)、[HTTP缺头默认和版本头](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports)、[03传输](https://modelcontextprotocol.io/specification/2025-03-26/basic/transports)、[06工具结果](https://modelcontextprotocol.io/specification/2025-06-18/server/tools)。这些是版本协商规则依据，不是对本产品全协议合规认证。

## 回归与查删项

新auth_first_test.go覆盖：三版本×三方法×有/无版本头18格；POST/GET/DELETE/OPTIONS/PATCH/HEAD及重复版本头/错误Origin/媒体/不可读body；错误/过期/吊销token；四个提议版本initialize后通知/ping/list/call及下一请求吊销；无头默认；已认证Origin/媒体/操作版本拒绝；真实本地HTTP401后带token协商；显式06不被额外vendor头误分流。最终矩阵另覆盖8类私有提示×03/11×已鉴权/未鉴权32格、相同8类×原06两种选择16正格，以及普通progressToken／其它namespace不误伤。此矩阵保护旧私有严格路径，未扩大业务权限。

四处原测试只调整被需求明确改变的前提，不删除安全断言：
- TestDiscoveryWithoutInitializeAndNoExtraCapabilities的GET/DELETE添合法bearer，继续断言405而非未认证时先405。
- TestRequestMetadataAndOriginGuards从“无鉴权”改成“恰一次鉴权/零业务”，原400/403/406及wire code断言保留。
- TestOAuthMetadataAndOrigin在Origin拒绝用例加合法bearer，继续断言全部路径403；本次只有/mcp先鉴权，其它端点仍是原guard。

原2025-06-18与2026正负格均由mcpgw完整三遍覆盖，不把定向13顶层/50含子项读数当完整门。

## Connect失败页与目标提示追加

依据组长03:30:24Z追加 `dw-dlv_253b11c26545050d364d4b1f02351b59`：仅在readAttempt失败时按Accept展示统一HTML400，说明必须同浏览器五分钟内从AI工具重新Authenticate、不复制链接。不存在、过期、缺cookie、错cookie四类返回相同HTML字节，不反射attempt/client/redirect/原始错误；非HTML仍为原400 invalid_request JSON，HTML q=0仍JSON。静态页无表单、脚本、外链或自动重试，CSP default/base/form/frame均none，no-store/no-referrer/nosniff。readAttempt的constant-time浏览器哈希比较、oauthTTL五分钟和Origin守卫均未放宽；原浏览器5分钟减1ns成功、恰好5分钟失败有常驻格，删绑定与延长TTL分别业务红。

依据组长03:48:19Z追加 `dw-dlv_624585bb04b3232701c84f2b93ddc4c5`：手填target_agent去掉具体Agent placeholder与示例，初次值空，提示逐字「填写你要协作的 Agent 全名（向你的组长或管理员索取），系统中必须已存在」。服务端目标、CSRF、required／pattern等验证不动。原TestConnectPageGETRendersSelfContainedValidatedForm只将三条旧文案期望换成业主指定新提示；其余身份、安全头、不可枚举和校验断言保留。新增格检查实际渲染无placeholder、无预填、无具体全名示例。

与GROUPS aaed8aae在internal/feishulogin/connect.go的关系：本片只改模板两行，GROUPS只改renderConnectPage中的allowUnpaired／allowReauthentication及空目标自动入口条件，属于不同hunk；后续须同时保留GROUPS可不建临时配对语义与本片无默认灰值文案。未在此单合入GROUPS、NAME或TTL；不把本分支main206上的手填页当成GROUPS联测结果。

## 冻结源中的检查坐标

| 判据 | 产品与常驻测试 |
|---|---|
| MAF-001 每请求先鉴权、不读未认证body | gateway.go:276；auth_first_test.go:19、57、164 |
| MAF-002 协商与下一请求吊销 | codec2025.go:104；auth_first_test.go:86 |
| MAF-003 缺头默认及操作拒绝 | codec2025.go:120；auth_first_test.go:132 |
| MAF-005 浏览器绑定／统一失败／期限 | oauth.go的readAttempt保留；connect_failure_test.go:12、73、95 |
| MAF-006 无具体目标示例 | feishulogin/connect.go:69；connect_target_hint_test.go:10 |
| MAF-007 私有提示不旁路／原06正格 | codec2025.go:20；auth_first_test.go:207、228、271、309、338 |

表内除标明feishulogin外均相对internal/mcpgw，坐标以ce9冻结对象为准。MAF-004证据与删保护见下节及VERIFY.py。

## 作者门

普通uid1001、Go1.22.2、锁定Python3.12.3，env白名单、数据盘HOME/TMP，SOURCE_COMMIT钉ce9b7a95；HUB_TEST_PG_STRICT=1，显式DW_DESKTOP_INTEGRATION_PYTHON以实跑跨运行时门。未继承在线SH/DINGWEI凭据，无现网连接。最终门按既定Z3例外完成，原始三遍并非全绿，见下列独立分项；不把author门当独立复核结论。

ce9原四包三遍中m8出现既有允许名单 TestSessionNameReconnectKeepsNameAndReleaseReuses 的 interrupted(9)，原rc1与全部输出保留，不称全绿。main206同环境独立HOME下同用例count20为18PASS／2FAIL、两条同形中断，rc1；这是基线落点对照，不是候选整包替代，不据短样本推断概率。候选按既定Z3规则另起全新串行五轮整包，五轮每轮原日志单独保存，终值见表。

## 最终作者门终值（全部绑定ce9）

canonical：两个helper profile各1270，inventory97，`BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`。OpenSpec strict42/0（另有最终文档strict）。Go1.22.2、Python3.12.3、普通uid1001、PG18.3真实子进程证据在anchor-pg-runtime.json；最终两个白名单的PYTHONPATH均指本分支，不沿用早期GROUPS路径。

下表三遍PASS/FAIL/SKIP按各级`---`记录统计；顶层PASS另列，子项不重复冒称独立顶层测试。

| 包（三遍） | 顶层PASS | 含子项PASS | 含子项FAIL | 含子项SKIP | 原始包结果 |
|---|---:|---:|---:|---:|---|
| mcpgw | 240 | 1092 | 0 | 18 | `ok  	github.com/zhangzongchu2019/dingwei/internal/mcpgw	345.737s` |
| extseat | 69 | 162 | 0 | 0 | `ok  	github.com/zhangzongchu2019/dingwei/internal/extseat	60.856s` |
| feishulogin | 141 | 333 | 0 | 0 | `ok  	github.com/zhangzongchu2019/dingwei/internal/feishulogin	0.034s` |
| m8 | 2957 | 4964 | 1 | 87 | `FAIL	github.com/zhangzongchu2019/dingwei/internal/m8	961.819s` |

35个唯一SKIP名称／105次出现逐名及条件见SKIPS.md与SKIPS.json；没有将FAIL改SKIP。没有强启浏览器/真实CLI/perf等其它opt-in，未宣称真实Antigravity验收。

按既定Z3允许名单另跑**全新串行五轮**m8，未择优、未沿用上述三遍：

| 新轮次 | rc | 顶层PASS | 含子项PASS | 各级SKIP | interrupted(9)原始行 | FAIL |
|---|---:|---:|---:|---:|---:|---|
| 1 | 0 | 986 | 1655 | 29 | 0 | 0 |
| 2 | 0 | 986 | 1655 | 29 | 0 | 0 |
| 3 | 0 | 986 | 1655 | 29 | 0 | 0 |
| 4 | 0 | 986 | 1655 | 29 | 0 | 0 |
| 5 | 1 | 985 | 1654 | 29 | 1 | TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent |

五轮全绿4/5；其它FAIL仅可为已批准名单的同形interrupted，否则脚本当场停报。每轮command/result/log与UTC在evidence/z3-anchor-round-*，原三遍rc1及main206对照也完整保留。无store改动或名单扩张，不以单跑绿替代整包。此例外只处置继承N4，不说明其已修复。

## 变异与负对照

最终十二刀均在独立副本，编译rc0、业务断言rc1、无panic；baseline/final-baseline绿，逐字节还原并核当前文件等于Git对象：
1. 版本检查移到鉴权入口之前，未认证401格红AUTH_BEFORE_PROTOCOL。
2. 恢复initialize只认06提议，03/11协商格红VERSION_NEGOTIATION。
3. 去掉已鉴权Origin guard，原应403变200而红。
4. 去掉后续操作版本拒绝，无头默认03错误放行而红DEFAULT_MARCH_VERSION。
5. 删readAttempt的cookie绑定，缺cookie打开仍被允许而红CONNECT_BINDING_REJECT。
6. 五分钟TTL改十分钟，恰好五分钟应拒绝的边界格红CONNECT_FIVE_MINUTE_EXPIRY。
7. 恢复旧target_agent具体placeholder，实际渲染格红CONNECT_TARGET_PRESET_EXAMPLE。
8. 绕过私有封套选择，显式2026头混用标准initialize应拒绝格红。
9. Mcp-Method存在性改成仅非空单值，空头／重复头绕过私有守卫格红。
10. 删除Mcp-Name存在性保护，name-only／空头／重复头混用格红。
11. 删除私有元数据namespace保护，private_caps_only／空私有版本／扩展键混用格红。
12. 删除原无头且params为06的优先选择，原June双选择正格红。

只计ce9源本轮十二刀，不与中间源任何刀相加。每刀有编译日志、定向业务红、逐字还原及最终基线，源未改动。

旧main206搭配同一份新测试overlay：编译成功，401和协商业务断言红；不把旧main自身全套说成红，也不混入十二刀计数。修复源定向基线绿。

首轮资格记录保留：未更新“鉴权前不触达Seats”旧断言时完整mcpgw曾rc1，三处测试前提修订如上；首个新定向命令因未使用net/http导入编译失败（不计任何业务变异），移除后绿，之后真实HTTP用例再按实际需要导入。未隐藏这些尝试，也未从首轮日志拼接最终通过。

## 中间锚与资格执行披露

299前段门只覆盖前段协议改动；6c新增connect后canonical绿，三包三遍唯一FAIL为已知N4 `TestSessionNameReconnectKeepsNameAndReleaseReuses`（interrupted(9)），原始整包日志保留，不改SKIP。6c前段环境PYTHONPATH仍指GROUPS工作树，最终907两套白名单已纠正为本分支，旧跨进程读数不背书最终门。

3c加目标文案后，我漏改原表单旧文案断言，导致Connect资格门与变异初始基线非绿；两道3c完整门按本席PID停止、原日志与停止审计保留，不记PASS。后续907只同步明确要求改变的文案断言，产品字节与3c相同；Connect与MCP定向两包已完整复跑rc0，再钉新锚起完整门。不从中间日志拼接最终通过。

R6于03:40Z到锚时本单已按令暂停，R6 PASS报告5e51a568交件后恢复；追加项与最终测试源在本报告中分别记锚。04:18:57Z r61第四段／第五段条件令另在集成工作树执行，只起第四段轻量门；本AUTH-FIRST分支仍待qwen插队复核，不能自并入集成或当发布GO。

907自查发现显式私有2026头与标准initialize混用可200，独立反例三格红；359只解决显式非空头后，另一存在性矩阵发现空值／重复Mcp-Method、仅Mcp-Name、私有namespace仍会宽松回退。两次均停止本席未完成Go／构建门、保留完整输出和审计；ce9一次补齐私有提示存在性与原06双选择正格后重新固定全门。它们是真实中间缺陷，不记最终PASS，不把八刀或七刀当十二刀。

## 现网修复前基线（组长提供，非本席独立探测）

依据组长2026-11-19T03:21:08Z补件 `dw-dlv_480c5539d6f45658a9fa0b02587b1c86`，其于03:20:56Z对dw123.cmjrt.com/mcp作无凭据只读探测：

| 未认证请求 | 修复前结果 |
|---|---|
| initialize 2025-06-18 | 401＋WWW-Authenticate，1001 unauthenticated |
| initialize 2025-03-26 | 400，无WWW-Authenticate，-32700 invalid JSON request |
| initialize 2025-11-25 | 同上400/-32700 |
| ping 无版本头 | 400，无WWW-Authenticate，-32020 HeaderMismatch |

本表归属是组长自报实际读数，不冒称本席取过HTTP原始响应。下列四条是**上线后获授权执行者**的验收件，本席未执行；从无凭据shell使用curl -q忽略个人curlrc，不加Authorization、cookie或账号参数。三条initialize均不带协议头，只在params提议版本，第四条ping也不带版本头。

```sh
curl -q -sS -i --max-time 10 'https://dw123.cmjrt.com/mcp' \
  -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
  --data-raw '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"auth-acceptance","version":"1"}}}'

curl -q -sS -i --max-time 10 'https://dw123.cmjrt.com/mcp' \
  -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
  --data-raw '{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"auth-acceptance","version":"1"}}}'

curl -q -sS -i --max-time 10 'https://dw123.cmjrt.com/mcp' \
  -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
  --data-raw '{"jsonrpc":"2.0","id":3,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"auth-acceptance","version":"1"}}}'

curl -q -sS -i --max-time 10 'https://dw123.cmjrt.com/mcp' \
  -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
  --data-raw '{"jsonrpc":"2.0","id":4,"method":"ping","params":{}}'
```

四条期望：HTTP状态均401（curl返回传输rc0并不代表验收通过），均有Bearer WWW-Authenticate及resource_metadata/scope，挑战值与原06-18基线保持；JSON-RPC错误1001 unauthenticated、未认证id=null。不得仍为400、出现HeaderMismatch或缺挑战头。逐条保存UTC、状态/响应头/无凭据body与部署源坐标；任何一条不符即停报，不自行重启、换件或扩测。该无凭据验收不能代替真实客户端OAuth及带凭据的后续协商验收，后者由组长另定。

## 回退与交接

无迁移/持久状态/新开关；若按部署命令换回旧Hub，会恢复旧版本先验阻断OAuth的行为，不会造成新数据库回滚要求。本单不执行换件。外部客户端不能靠回退得到已修的认证挑战；发布路线由组长另定。

完成：代码、回归、十二刀和精确最终测试文件配旧源的负对照。作者完整门、十二刀与原始失败已归档，产品未改；初交审623fcf4c已push且ls-remote核同，qwen按组长令与五轮并行复核。下一步只追加文档、核最终远端、给qwen与组长补证，等待独立复核／终审；不自并AUTH、不部署。R6已交PASS并已在r61第四段合入，不再重复复验。

## 本件复核命令与证据定位

在仓库根运行：

```sh
python3 docs/reviews/mcp-auth-first/VERIFY.py
sha256sum -c docs/reviews/mcp-auth-first/SHA256SUMS
```

VERIFY只读Git对象、当前产品与证据；核11个产品／测试路径、228份原始件及gzip解压后的原文SHA、最终十二刀与还原基线、精确旧源负对照、canonical与strict原rc、三遍原rc1及新五轮既定Z3判据。RAW-INDEX保留中间资格失败／停止尝试，不把它们拼作最终门。INITIAL-VERIFY仅是05:10令先推阶段的旧入口，不据它判最终门。

最终原始件：evidence/canonical-anchor.log.gz、go-count3-anchor.log.gz、z3-anchor-round-1到5.log.gz、z3-baseline-count20.log.gz、mutations-anchor/summary.json；每件command/result含UTC和固定源，实际PG版本在anchor-pg-runtime.json。35个唯一SKIP见SKIPS.md，全部105次原始出现见SKIPS.json。

最终交接证据坐标：`e5754c6e91a1cc4a3ae48883a5e8eb90034e4ca7:docs/reviews/mcp-auth-first/gate-summary.json`，另可`git cat-file -p e5754c6e91a1cc4a3ae48883a5e8eb90034e4ca7:docs/reviews/mcp-auth-first/evidence/z3-anchor-summary.json`读取五轮。当前补记只改docs／openspec，产品仍ce9，独立复核与发布路线由qwen／组长另定。
