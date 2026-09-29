# view-service/ 全套件清单(只量不改)

- 承办:opus50tc2k · 日期:2026-09-24
- 树:**干净 preprod** `1c3c2990`(merge: 0.2.1.66 回流 preprod),工作区 `git status --porcelain` 为空
- 跑法:`cd view-service && NODE_PATH=/data/ai-dev-data/work/pw/node_modules node <文件>`,单跑、无并发、无环境变量
- 口径:**死的 = 在干净树上就红**。红的只登记,不修(t145/t146 例外,按组长边界单独修桩)

> 🔴 **通则(2026-09-29 补):本表每一个读数都绑在量它的那个 commit(`1c3c2990`)上。**
> 某条套件、或它所测的那一片产品代码,在该 commit 之后被改过或重建过,那一条的读数就**作废**,
> 必须在新树上重量后才能拿来指挥人。判法:`git log --format=%h 1c3c2990..<新树> -- <套件文件> <它测的产品文件>`
> 非空 ⇒ 重量。🔵 此判法的阳性对照:对 entry 套件取 `1c3c2990..7241bae2`,命中正是 `db81911b`、`2a09a68c` ⇒ 当初若照此判,会提前发现。🔴 **不许拿旧读数给新树判死活** —— 活着的闸被台账注销,比闸本身坏了更难发现。
>
> 🔵 **更正记录**
> - 2026-09-29 · `console-agent-groups-entry-regression.js` 由「死的」移入「活着」,见 §一、§四。
>   原读数 2 / 2 在 `1c3c2990` 上是对的;之后 `db81911b`(09-24 重建 grouping workspace)与
>   `2a09a68c`(09-26 task board)重建了那一片,G3/G4 口径随之改写,两笔都不在 `1c3c2990` 之内。
>   **其余 30 条未在新树上重量**,下表数字仍只对 `1c3c2990` 成立。

## 〇 口径先说清楚:`view-service/*.js` 一共 37 个,不都是套件

| 桶 | 个数 | 说明 |
|---|---|---|
| 活着(绿) | 28 → **29** | 干净树上跑通(29 = 28 条 `1c3c2990` 读数 ＋ entry 1 条 `7241bae2` 重量读数) |
| 死的(红) | 3 → **2** | 干净树上就红(entry 已移出,见 §一) |
| 需外部装置(未起,**不判死**) | 3 | 套件自己的头注写明要先起 fixture 服务;我没起,所以红的是我的环境不是它 |
| 非套件(库/夹具,`module.exports`) | 3 | 跑它没有断言,rc=0 也不算"通过" |

🔴 把后两桶算进"死的"会虚报 6 条。分桶依据写在各条备注里。

## 一 死的(2 条;原 3 条,`console-agent-groups-entry` 于 2026-09-29 重量后移出 → §四)

| 套件名 | 通过/失败 | 判定 | 红在哪 |
|---|---|---|---|
| `renderer-t145-tabs-regression.js` | 0 / 8 | 死的(已登记) | 全部 8 条 |
| `renderer-t146-agent-refresh-regression.js` | 0 / 4 | 死的(已登记) | 全部 4 条,均为 `Cannot set properties of null (setting 'onclick')` |

**(历史,仅对 `1c3c2990` 成立)** 原登记:`console-agent-groups-entry-regression.js` 2 / 2 · 死的 ——
G3 当时要求 builtin「Agents」**存在**且为页内锚,G4 当时要求 owner 看不到入口而 builtin agents 仍在;两条都因 builtin agents 项不见而红。
🔵 其后重建把 G3 **反转**成「builtin Agents 入口必须不存在」,所以旧红不是回归,是旧判据跟着产品被改写了。

**关于 console-agent-groups-entry 的一句话读数(不是结论,仅对 `1c3c2990` 成立):** 两条红都指向"左栏 builtin Agents 入口在页里找不到"。
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

## 四 活着(29 条;28 条为 `1c3c2990` 读数,entry 1 条为 2026-09-29 重量读数)

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
| `console-agent-groups-entry-regression.js` 🔵 **2026-09-29 重量** | **4 / 0**(见下方专注) |

**`console-agent-groups-entry` 重量读数(2026-09-29T06:42Z,`date -u` 实取;opus50tc2k 本机 6 核 · load 0.29;
detached worktree,`git status --porcelain` 为空;单跑、无并发;跑法同表头,阳性对照另加 `POSITIVE_CONTROL=1`):**

| 树 | 基线 | `POSITIVE_CONTROL=1` |
|---|---|---|
| 现网 `preprod` `7241bae2`(VERSION 0.2.1.94) | rc=0 · ✅ 4 · 🔴 0 | rc=1 · 红 **G1/G2**,G3/G4 绿(该树的对照只声明 G1/G2 会红,与之相符) |
| 分支 `qwen38maxkhovvob4/s-group-entry` `b437c746` | rc=0 · ✅ 4 · 🔴 0 | rc=1 · 红 **G1/G2/G4**、G3 绿,并自报「阳性对照成立」 |

🔴 **两棵树的 G4 不是同一条判据**:
- 现网 `7241bae2`:G4 = 「owner 左栏也不得出现已删除的 builtin Agents」(只守"不出现")
- `b437c746`:G4 = 「owner 左栏**必须有** agent-groups 内部 tab,且仍不得出现 builtin Agents / admin」
⇒ **"owner 必须看到编组入口"这条新钉进去的 G4 目前只在该分支上,不在现网里**(`merge-base --is-ancestor b437c746 7241bae2` 为假)。
现网上这条性质**没有判据在守**;待该分支合入后,须在合入锚上重量本行。

🔴 **失效条件**(任一成立 ⇒ 本行作废,须重量):
1. `git log 7241bae2..<新树> -- view-service/console-agent-groups-entry-regression.js` 非空(套件改了);
2. 同一范围内它所测的产品文件有改动:`git log 7241bae2..<新树> -- internal/m8/console_auth.go internal/m8/console_agent_groups.go internal/m8/console_platform.go` 非空(`db81911b`/`2a09a68c` 动的就是这几处);
3. `qwen38maxkhovvob4/s-group-entry` 或其后继合入 preprod ⇒ G4 口径变为分支版,现网那一格改按分支读数重量;
4. 跑法变了(不再是本机单跑、或 playwright/chromium 大版本变更;本次为 playwright 1.63.0 · chromium headless shell 153)。

**两条"绿但自报带伤"的备注(登记,不修):**

1. `renderer-c2-regression.js` 末行是 `HARD PASS 25; KNOWN-DEFECT T-1…` —— 它把一个已知缺陷**降级成非硬断言**后放行。
   绿灯掩着一条已知缺陷,rc=0 分辨不出"没缺陷"和"缺陷被豁免"。
2. `renderer-sse-reconnect.js` 末行是「重复本身仍在(见 L4 那两行测量输出),治它要 ⑰」。同形:**它测到了问题,但不红**。

🔴 这两条不是我的发现范围内要修的,但它们说明一件事:**"全绿"这张表本身不能单独当质量读数**。

## 五 一句总结

干净 preprod `1c3c2990` 上,`view-service/` 的 31 个可跑套件里 **28 绿 / 3 红**,红的三条是
`console-agent-groups-entry`(新发现)、`renderer-t145-tabs`、`renderer-t146-agent-refresh`。
🔵 **2026-09-29 更正**:`console-agent-groups-entry` 在现网 `7241bae2` 上重量为 4 / 0,已移入「活着」
⇒ 按"混合读数"计为 **29 绿 / 2 红**;其余 30 条未重量,仍只对 `1c3c2990` 成立(见表头通则)。
另有 3 条因未起外部装置而没被测到,3 个文件根本不是套件。
