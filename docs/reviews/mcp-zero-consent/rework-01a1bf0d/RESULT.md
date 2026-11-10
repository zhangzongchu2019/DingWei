# ZERO-CONSENT 首轮返工交审

CODEX_THREAD_ID=01a1bf0d-d5c9-7613-9845-5a3707ec3c9b；原交件c85b4e93之后追加，不改写。组长明确改派后，实读远端为c85b4e93、原工作树status空且HEAD相同、ps与/proc无原工作树或原写者的go test/git/编辑进程，才留证接管。旧锁为/tmp/coder56-mcp-zeroconsent.lock.stale-20261109T074843Z。新工作树mcp-zero-consent-01a1bf0d；写/提交/推送前均复核新owner。

冻结实现 **10e6c2213291abf636a1b13c022ac0a6c4841786**，tree **de5cfaa9da54b3996475dfed68776f7dca066744**。先以merge 4cf28d19885d38e1d9b25815e1857879cfb726ac合入d70016f3；VERSION＝0.2.3.33，ZERO-CONSENT不另抬版本。最新裁定已核review/opus50tc2k/rulings＠dcfd24ef，新增补30/31不改变本轮范围；main仍fce2ede8。

## 修复与直接负例

login kind保持兼容，哈希前分成state:、started:、selection:三个域；GET只查/Take state:，POST只查/Take selection:，不改旧迁移CHECK。

TestConsentLoginNamespaceIsolation三格均绿：

- started-as-state：同attempt/cookie取三个独立CSRF表单；首次POST303、第二次400、伪造GET state=started:attempt为400，启动记录仍在、第三次POST仍400；原真实state可继续完成选择及签发。
- selection-as-state：真实飞书回调生成选择票据；伪造GET state=selection:token为400且选择记录未消费，原选择POST成功302，重放400。
- state-as-selection：POST selection=真实state为400；原state仍能回调生成选择页并完成一次签发。

namespace_mutation.py用Go overlay同时撤state写入/读取前缀，分别运行两格GET负例：固定版各rc0；变异各rc1，分别命中ZC_STARTED_AS_STATE_RESTART与ZC_SELECTION_AS_STATE_CONSUMED，非编译红。namespace/RESULT.json及四条原文可复跑。

## 真浏览器与额外实测阻断

browser_regression.py已登记tools/run-browser-regressions.py的SUITES。无响应头改写、无Origin伪造、无回调导航拦截：真实本地Hub主接线＋真实临时PG＋模拟飞书OAuth，Chromium真实提交表单，随机端口HTTP客户端实际接收授权码，再按原PKCE换票。

单目标：连接页可编辑输入0，POST真实Origin＝Hub，签发/换票成功。多目标：connect→飞书替身→两项单选→POST→客户端回调→换票，两个POST真实Origin均＝Hub，origin_rejected为0。browser/{single,multi}均browser_rc0/hub_rc0。桌面Codex本身仍未测。

真浏览器另证实原form-action self会拦连接页303飞书及选择POST后302客户端；旧CLI探针没有覆盖该行为。最小修复：连接页加入配置OAuth授权origin，网关加入已验证并持久化的attempt回调origin；选择页仅加入该回调origin。来源不取请求Host/未验证参数，丢掉path/query，拒userinfo、非http(s)、空host、通配符与CSP分隔符；保持default-src none、base-uri none、frame-ancestors none，Origin/CSRF/浏览器与OAuth守卫不变。单位负例验证策略注入被拒。

--cut connect-csp（single）撤配置飞书origin、--cut selection-csp（multi）撤客户端回调origin，均Hub rc0/浏览器rc1，分别命中实际浏览器form-action拒绝与授权不能完成，见browser-mut-*；没有把超时或编译失败单独算红，原文均含对应CSP拒绝。原有30刀全部目标红；合计33条新旧撤除动作，其中state一刀分别命中两格，另加两条CSP撤除，共34格目标红。

## 最终门

| 门 | PASS / FAIL / SKIP |
|---|---|
| mcpgw |236 / 0 / 0|
| feishulogin |97 / 0 / 0|
| extseat、extshared、extwire |27、6、13 / 0 / 0|
| store/migrate |43 / 0 / 0|
| store |599 / 0 / 5|
| identity |215 / 0 / 0|
| cmd/workpulse |259 / 0 / 6|
| m8全包-p 1 |1512 / 0 / 37|
| 全量三包race-count=3 |mcpgw708、feishulogin291、extwire39；0 FAIL、无DATA RACE|
| build.sh --test-only（内含scripts/run-build-tests.sh） |rc0，claude/codex各1129/1129，91文件、27Python/2shell、BUILD_TESTS_OK|
| OpenSpec全量strict |29 / 0|
| 官方CLI0.167.0 URL-only原生 |native_rc0、hub_rc0|
| 新浏览器注册环境输入自检 |rc0|

Go官方1.22.2，普通uid1001；环境白名单、隔离HOME/TMPDIR、GOPROXY=off、PG严格。完整命令与UTC/rc在各json，COUNTS.json汇总，SKIPS.json列名，原文保留各跳过条件。冻结后整条多包命令已重跑rc0，packages-final为最终读数；affected-final另记冻结源两包与全量race。

superseded保留首轮多包命令在源码写入期间的vet类型信息不一致错误，不算最终PASS；其后冻结源码，整条原命令重跑通过。早期浏览器的模块路径缺失、CSP禁止测试fetch、未启动客户端回调监听等夹具问题均保留原文；实际CSP阻断独立修复和反向验证，不算环境问题掩盖。

本目录SOURCE-SHA256SUMS为返工当前文件，原目录旧清单/原文保留作c85历史证据；ARTIFACT-SHA256SUMS只覆盖本轮证据。测试HOME、密钥、ca私钥、env、上游token未复制。源码只经Git交付，不上传源码包。

未部署、未改线上配置、未启动真实席位或推理、不自验收。旧裸state的短期进行中授权在切换后会拒绝，应重新授权；没有放宽兼容裸键。ZERO-CONSENT交件后才核M3接管条件与只读全库自检必补。
