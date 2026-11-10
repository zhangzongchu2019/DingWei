# ZERO-CONSENT 安全收尾：r56.2 第三发布值

CODEX_THREAD_ID=01a1bf9a-fffd-7472-ac1b-7392961de426；to=opus50tc2k.zzc。
按组长回应11bd136be747的直接裁定接管：2026-11-09T13:20:16Z原树porcelain0、HEAD与工蜂ea63d294一致、逐PID /proc核cwd无git/go/python、实际gitdir无index.lock；旧锁stale-20261109T132042Z保留。本次沿用原树和分支，不回滚任何旧写者已交字节。

## 冻结范围与源码

产品merge 3bd3d97b9，两个父为 ea63d294f29525ec9fc080783f32999dbc0c9035、0de00bc45d2716cb7ba22b5642dca6f9e73f229f；无rebase、无force。测试源追加2e88785109a75b3185bd40304a4a1f820356415e（tree 0f9f2042ac31c9c5462907c8316708d249b870c9）。VERSION保留0.2.3.33。连接页采用r56.2的withLoopbackFormAction＋authorizeOriginOf；选择页保持原实现，FormRedirectPolicy仅留选择页。login state:/started:/selection:三域前缀已在父候选实现，本次完整三负例重跑，不重复改动或新增默认名单/Console功能。

只做安全必改及A/B浏览器收尾。按㊿-补38，完整身份模式不用默认目标名单；本分支原有AG功能作为访客兼容候选保留，交件后冻结，发布待乙P1计划复核，不自放行。

## 真浏览器与变异

真实Chromium、产品Hub main接线、真实临时PostgreSQL和客户端随机回环监听；只有飞书上游为合成替身。A=上游立即302，B=先显示同意页再点击跳回；A/B分别单目标、多目标，共4格均实际收到code并以PKCE兑换token，originRejected=0。浏览器不伪造Origin、不改响应头、不拦截导航。最终证据browser/*/browser-result.json为Hub0/浏览器0。既有runner已登记mcp_zero_consent，无新未登记脚本。

三条实际CSP撤除：loopback-csp（A单目标）、connect-csp（B单目标）、selection-csp（B多目标），全都浏览器rc1、Hub rc0，原文有Chromium form-action阻断，3/3 target_red。命名空间去state域前缀的两格均目标红：ZC_STARTED_AS_STATE_RESTART、ZC_SELECTION_AS_STATE_CONSUMED；三负例完整包正常通过。原30刀全部目标红，含各自断言，编译错误不计红。

新增上游opt-in TestBrowserAuthorizeChainReachesLoopback显式DW_BROWSER_E2E=1、NODE_PATH运行PASS；默认包门中该用例SKIP（条件名见SKIPS），不能把默认SKIP当作实跑。保存日志对合成一次性code/state做脱敏，无测试HOME、token存储、env、证书私钥或DB进入Git。

## 完整门（普通uid1001、Go1.22.2、白名单环境）

| 门 | 本次读数 |
|---|---|
| mcpgw | 239 PASS、0 FAIL、1 opt-in SKIP；上述opt-in单独实跑PASS |
| feishulogin/extseat/extwire/extshared | 99/27/13/6 PASS，各0 FAIL |
| identity/store/store-migrate/workpulse | 215/599/43/259 PASS；store5、workpulse6预置SKIP |
| m8全包-p1 | 1512 PASS、0 FAIL、37预置SKIP |
| Consent相关五包race-count3 | rc0，无DATA RACE |
| build.sh --test-only | 13:23:44Z～13:32:43Z rc0；双profile各1129/1129、27Python/2shell，BUILD_TESTS_OK |
| OpenSpec全量strict | 29 PASS、0 FAIL |
| 变异 | 原30/30、命名空间2/2、CSP浏览器3/3目标红 |

全部命令/UTC/rc见gates/*.json、canonical/RESULT.json；完整JSON/文本原文gzip保留，SKIPS按名称列出，条件见相应stdout。canonical使用锁定venv15.0.1、短TMPDIR=/tmp；未改产品断言、时限或SKIP条件。

首轮B单目标测试断言错误地依赖CSP源的排列顺序而红（r56.2将loopback插在上游源前）；改为解析form-action源列表后重新跑四格全绿。该红保留superseded-header-order-assertion，不作产品失败或目标变异红。除测试断言修正外产品字节未变；最终四格在最终测试源码上重跑。

SOURCE-SHA256SUMS固定受测源；ARTIFACT-SHA256SUMS固定交件白名单原文。只经工蜂Git普通推送并ls-remote核一致后回坐标。尚无独立复核结论；桌面未测，无部署/重启/在线席位/真实推理动作。M3锁未由本轮接管。
