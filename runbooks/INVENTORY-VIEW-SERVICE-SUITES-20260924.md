# view-service/ 全套件清单(只量不改)

- 承办:opus50tc2k · 日期:2026-09-24
- 树:**干净 preprod** `1c3c2990`(merge: 0.2.1.66 回流 preprod),工作区 `git status --porcelain` 为空
- 跑法:`cd view-service && NODE_PATH=/data/ai-dev-data/work/pw/node_modules node <文件>`,单跑、无并发、无环境变量
- 口径:**死的 = 在干净树上就红**。红的只登记,不修(t145/t146 例外,按组长边界单独修桩)

## 〇 口径先说清楚:`view-service/*.js` 一共 37 个,不都是套件

| 桶 | 个数 | 说明 |
|---|---|---|
| 活着(绿) | 28 | 干净树上跑通 |
| 死的(红) | 3 | 干净树上就红 |
| 需外部装置(未起,**不判死**) | 3 | 套件自己的头注写明要先起 fixture 服务;我没起,所以红的是我的环境不是它 |
| 非套件(库/夹具,`module.exports`) | 3 | 跑它没有断言,rc=0 也不算"通过" |

🔴 把后两桶算进"死的"会虚报 6 条。分桶依据写在各条备注里。

## 一 死的(3 条)

| 套件名 | 通过/失败 | 判定 | 红在哪 |
|---|---|---|---|
| `console-agent-groups-entry-regression.js` | 2 / 2 | **死的(新发现,登记)** | G3:builtin「Agents」须页内锚(`#agents`、无 target、点击不开新页)→ **builtin agents 项不见**;G4:非 management(owner) 须看不到入口,而 builtin agents 仍在 → **owner 连 builtin agents 都丢** |
| `renderer-t145-tabs-regression.js` | 0 / 8 | 死的(已登记) | 全部 8 条 |
| `renderer-t146-agent-refresh-regression.js` | 0 / 4 | 死的(已登记) | 全部 4 条,均为 `Cannot set properties of null (setting 'onclick')` |

**关于 console-agent-groups-entry 的一句话读数(不是结论):** 两条红都指向"左栏 builtin Agents 入口在页里找不到"。
这与左栏改造是同一片区域。🔴 我**没有**查它是哪一笔改动打红的,也没有修——按令只登记。
谁接它,第一步应当是 `git log -S` 定位入口是被删了还是被改名了,再决定是改格还是回产品。

## 二 需外部装置(3 条,红 ≠ 死)

| 套件名 | 结果 | 它自己要求的装置 |
|---|---|---|
| `renderer-sse-smoke.js` | rc=1 `ERR_CONNECTION_REFUSED @ 127.0.0.1:19400/renderer` | 头注第 5 行:`RENDERER=<件> python3 renderer-sse-fixture.py 19400 &` |
| `renderer-sse-profile.js` | rc=1 同形(默认 PORT 19450) | 同上,端口不同 |
| `renderer-0206-file-render-regression.js` | rc=2 `ERR_SSL_VERSION_OR_CIPHER_MISMATCH @ https://127.0.0.1:19443` | 头注第 9 行:`MOCK_BASE=https://127.0.0.1:19443 FIXTURES=<样本目录>` |

这三条**未经测量**其真实死活。要判它们,得先按头注起装置再跑一遍;我没做,因为那超出"只量"的范围。

## 三 非套件(3 条)

| 文件 | 为什么不是套件 |
|---|---|
| `renderer-regression-selfhost.js` | 末行 `module.exports = { finishRegression, launchChromium, regressionCollector, startRendererServer }`,是公共夹具 |
| `md-render-core.js` | 末行 `module.exports = api`,被 parity 套件引用 |
| `md-injection-payloads.js` | 末行 `module.exports = { CANARY, PAYLOADS, ALL_TEXT, escTextNode }`,是载荷表 |

🔴 这三个 `node` 跑起来都 rc=0 且零输出。**rc=0 在这里不等于通过**——它等于"没有断言被执行"。

## 四 活着(28 条)

| 套件名 | 通过/失败 |
|---|---|
| `console-agent-groups-vega-regression.js` | 8 / 0 |
| `console-browser-regression.js` | 19 / 0 |
| `console-panel-pointerevents-regression.js` | 11 / 0 |
| `console-spec-workflow-regression.js` | 12 / 0 |
| `console-specs-browser-regression.js` | 34 / 0 |
| `console-theme-global-regression.js` | 10 / 0 |
| `md-render-core-parity.js` | 99 / 99 项逐字节相同 |
| `renderer-c2-regression.js` | HARD PASS 25(见下方备注) |
| `renderer-canary-g2-regression.js` | 绿(末行 `G2 #11 renderer-half PASS`) |
| `renderer-failcard-regression.js` | 5 / 5 |
| `renderer-in-console-iframe-regression.js` | 3 / 0 |
| `renderer-longtext-regression.js` | 绿(`ALL PASS`) |
| `renderer-markdown-image-regression.js` | 15 / 0 |
| `renderer-markdown-injection-regression.js` | 11 / 0 |
| `renderer-page-identity-regression.js` | 绿(`PASS: cloned-tab tie-break + credential`) |
| `renderer-provenance-icon-regression.js` | 5 / 5 |
| `renderer-regression.js` | 绿(`PASS: waiting admission + continuous SSE`) |
| `renderer-scroll-follow-regression.js` | 9 / 0 |
| `renderer-sse-reconnect.js` | rc=0(末行自报"重复本身仍在,治它要 ⑰") |
| `renderer-storage-denied-regression.js` | 3 / 3 |
| `renderer-t054-errorboard-regression.js` | 6 / 6 |
| `renderer-t074-delivery-id-regression.js` | 绿(`ALL PASS`) |
| `renderer-t086-deferred-clear-regression.js` | 8 / 0 |
| `renderer-t086-empty-ring-e2e-regression.js` | 2 / 2 |
| `renderer-t138-shortname-regression.js` | 5 / 0 |
| `renderer-t141-capability-reuse-regression.js` | 9 / 0 |
| `renderer-t147-paste-regression.js` | 10 / 0 |
| `terminal-storage-denied-regression.js` | 2 / 2 |

**两条"绿但自报带伤"的备注(登记,不修):**

1. `renderer-c2-regression.js` 末行是 `HARD PASS 25; KNOWN-DEFECT T-1…` —— 它把一个已知缺陷**降级成非硬断言**后放行。
   绿灯掩着一条已知缺陷,rc=0 分辨不出"没缺陷"和"缺陷被豁免"。
2. `renderer-sse-reconnect.js` 末行是「重复本身仍在(见 L4 那两行测量输出),治它要 ⑰」。同形:**它测到了问题,但不红**。

🔴 这两条不是我的发现范围内要修的,但它们说明一件事:**"全绿"这张表本身不能单独当质量读数**。

## 五 一句总结

干净 preprod 上,`view-service/` 的 31 个可跑套件里 **28 绿 / 3 红**,红的三条是
`console-agent-groups-entry`(新发现)、`renderer-t145-tabs`、`renderer-t146-agent-refresh`。
另有 3 条因未起外部装置而没被测到,3 个文件根本不是套件。
