## 实施任务

- [x] T-01 核官方协商/默认版本规则与基线，记录A-AUTH-01最小范围。
- [x] T-02 前置每请求鉴权，保留已鉴权严格校验，补三版本与协商后链路、追加connect统一HTML/JSON及绑定/TTL回归。
- [x] T-03 固定产品源跑canonical与mcpgw/extseat/m8三遍PG strict，逐名列SKIP。
- [x] T-04 删保护业务红与原始日志归档。
- [x] T-05 普通推送核远端，请qwen（GROUPS之前）和组长复核，不部署。

## 交接记录

2026-11-19 coder56solzzc：完成T-01技术设计与基线对照；产品与测试工作树实施中，尚无冻结门。阻塞无；R6新锚到达立即中断本单复验，之后恢复。下一步定向测试与固定源完整门。

2026-11-19 coder56solzzc：299b5d75前段canonical与三包三遍均rc0。收到03:30:24Z追加connect失败页，原门只作前段证据；新代码/规格待钉新源后重跑完整门，不以299绿覆盖新增产品。readAttempt与oauthTTL不改。

2026-11-19 coder56solzzc：R6复验PASS已交5e51a568，恢复本单。03:48Z追加授权页无具体Agent默认示例，新增MAF-006与渲染格；原6c门保留（m8仅已知N4落点rc1），新源完整门再钉，旧读数不覆盖追加产品。GROUPS同文件为不同函数hunk，后续须保留其可选目标语义。

2026-11-19 coder56solzzc：3c5e8f3d首次文案资格门暴露旧表单测试仍强制旧示例字符串；仅同步这一旧文案断言为业主指定新提示，原required／pattern／CSRF／目标校验断言不变，新无placeholder格保持。两道3c门已按本席PID停止，完整原输出和停止记录留档，不记PASS。修正后Connect与MCP定向两包rc0，再钉产品／测试新源跑全门。最终环境PYTHONPATH显式指向本分支，前段6c所用GROUPS路径仅作原始资格记录，不背书最终跨进程门。

2026-11-19 coder56solzzc：自查发现907标准initialize可覆盖显式2026私有头，隔离反例三格200而红。暂停本席907未完成Go门后修正codec选择，保留原06两式优先规则；补MAF-S009与第八刀，最终新锚重跑全门。907 canonical绿／七刀绿仅作中间读数，不背书未测私有混合边界。未改已认证权限，不部署。

2026-11-19 coder56solzzc：359在私有头空值／重复、仅Mcp-Name或私有元数据时仍可宽松回退，独立矩阵红；一次补齐存在性判定及私有namespace识别，并补普通metadata不误伤与原06选择正格。停止本席359未完成门、保留原记录，最终源只以新完整门与本轮删保护为准。

2026-11-19 coder56solzzc，冻结产品ce9b7a95、初交审623fcf4c82a7a22ce3de06135b9fa4ef3cc7bcaf：T-04十二刀全部compile0业务红、还原绿；canonical与strict完成，原始证据可git cat-file 623fcf4c82a7a22ce3de06135b9fa4ef3cc7bcaf:docs/reviews/mcp-auth-first/evidence/mutations-anchor/summary.json。T-05普通push及独立ls-remote核同，qwen交审回执dw-dlv_6d97204a965ec3c863e77fa9a472a86b，按组长05:10令代码复核与Z3并行。T-03仍未勾：原四包三遍只在m8见已知N4，rc1保留，主干同例18PASS／2FAIL；新五轮串行进行中。阻塞仅剩最终作者门与独立终审；后续只改docs／openspec，不改产品、不部署。

2026-11-19 coder56solzzc：T-03按既定Z3口径完成。ce9四包三遍原rc1仅已知N4，mcpgw／extseat／feishulogin三遍rc0；全新串行m8五轮rc依次0／0／0／0／1，前四轮各986顶层PASS，第五轮985PASS、唯一TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent在hub_test.go:3741 interrupted(9)。4／5绿满足既定门，原三遍和主干同例18／2对照均保留，未择优、未改store、未扩名单。完成canonical双profile各1270／inventory97／BUILD_TESTS_OK、严格校验、十二刀与精确负对照；阻塞为独立复核和组长终审，下一步仅推文档补证并等待条件集成，不部署。
