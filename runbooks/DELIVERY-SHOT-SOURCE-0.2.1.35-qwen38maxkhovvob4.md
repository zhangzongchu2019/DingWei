# 交付件 — 截屏选源:让【当前这个 tab】回到选源列表里

    席      `qwen38maxkhovvob4`(renderer 侧)
    来源    业主实操原话「**不能截屏本 tab 窗口页**」;组长 `ts-No.25` 提出假设、`ts-No.26` 撤回其中第③条
    板件    「🔴🔴 业主实操暴露两件」③ 截屏选源(锚 `d105665c`,2026-09-20T06:46)
    分支    `qwen38maxkhovvob4/overlay-iframe-dark-crossrun`
    坐标锚  闸 `c5dd8f17` · 修 `f32e0cbb`(下行号一律以 `f32e0cbb` 为准)
    改动    `view-service/renderer-canary-sse.html:4641`(产品,一行 ＋ 三行注释)
            `view-service/renderer-0206-file-render-regression.js`(新增用例 ㉚)
    🔴 发布影响  renderer sha256 `0a0d6261…` → **`9b5656f4505c16bd…`**
                 ⇒ `deploy/invariants-dingwei-gray.json:16` 出单时必须重算(那条自述"当日最贵的那条断言")

---

## 〇 为什么有这一格

板件 ③(06:46)当时的判断是:「我们不设任何约束 ⇒ 选源框里出现哪些来源**完全由浏览器 + 操作系统决定**,
**产品侧无从限制也无从放开**」。

🔴 **这句被 `ts-No.25`(06:53)推翻,而本件把它实测钉死了**:产品侧**有**一个旋钮,叫 `selfBrowserSurface`,
它正是"要不要把**发起调用的这个标签页**放进选源列表"。板件 ③ 写在 ts-No.25 之前 ⇒ **以 ts-No.25 为准**;
🔵 请组长在板件上更正 ③ 那一句(本席不写板件)。

业主的症状形状与那个默认值**逐字吻合**:他先前的描述是「只能截 chrome 内**其他**窗口」——
即"别的都在,只有我眼前这一页不在"。🔵 这是**旁证,不是实测**(见第六节第 2 条)。

---

## 一 判据(闸 ㉚,`renderer-0206-file-render-regression.js`)

钉的是**产品实际传给 `getDisplayMedia` 的约束对象** —— 不是"上传发生了一次"那种零判别力的形状
(U7 已经证明过那玩意儿零转红)。

    断言 0  记录器恰好被走到 **1 次**          ⇒ 否则下面每条都是"没记录到"式的假绿
    断言 1  `selfBrowserSurface === 'include'` ⇒ 🔴 核心
    断言 2  `preferCurrentTab === undefined`   ⇒ 机制钉:不许把选源窄化成"只有本 tab"
    断言 3  真路径没被弄坏:遮罩出现 ＋ 抓到真帧,且**不是** ㉕ 那个 200x100 替身
    守卫    本浏览器**认识**这个成员(两半,缺一不可):
              ① 传**非法枚举值** ⇒ 必须 `TypeError`(枚举转换失败,发生在任何采集逻辑之前)
              ② 传**编造的成员名** ⇒ 必须**不**是 `TypeError`(未知成员被忽略)
            🔴 只有两半同时成立,那个 TypeError 才是"认识"的证据,而不是环境噪声

**手法**:在页面里包住真 API **只做记录**,随后**委托真实现**;点 📷 用 Playwright 的 trusted click,
整条截屏路径(抓帧 → 遮罩 → …)照常走完 ⇒ 观察的是**真调用点**,不是 grep 源码。

🔵 **断言 2 的有效期**:它钉的是**当前修法**,不是永恒不变量。若哪天产品决定"📷 就是只截本页",
这条会红而产品是好的 —— 届时请**连注释一起改写**,别只删断言。

---

## 二 牙口:先写闸、跑到红、再动手

| 轮次 | renderer sha256(前16) | 结果 | 红在哪 |
|---|---|---|---|
| 基线(加闸前) | `0a0d62617cb58fce` | 通过 **40** / 失败 0,rc=0 | — |
| 🔴 加闸、**未修** | `0a0d62617cb58fce` | 通过 40 / 失败 **1**,rc=**1** | 只 ㉚;实测约束 = `{"video":true,"audio":false}` |
| ✅ 修完 | **`9b5656f4505c16bd`** | 通过 **41** / 失败 0,rc=**0** | — |

**缺陷是活的 ⇒ 阳性对照天然存在**(与遮罩那格同型,不用靠变异模拟)。三发变异:

| 变异 | 施加内容 | sha256(前16) | 结果 | 红在哪条断言 |
|---|---|---|---|---|
| **M-E1** | 删掉 `selfBrowserSurface:'include'` | `0a0d62617cb58fce` | 40/1 rc=1 | 断言 1(= 修复前那一轮,**同一份字节**) |
| **M-E2** | 保留 include,**加** `preferCurrentTab:true` | `1e2aa03225e91cfa` | 40/1 rc=1 | 🔴 **断言 2** ⇒ 它有**独立**牙口,不是断言 1 的影子 |
| **M-E3** | 成员名拼错成 `selfBrowserSurfaces` | `48f901bcb0e96a5c` | 40/1 rc=1 | 断言 1 ⇒ **拼错会被抓**(这是最现实的静默失效形状) |

三发都**只红 ㉚ 一条**,其余 40 条全绿。
🔵 **这正是这一格的价值**:M-E2/M-E3 这两种改法,在**全仓其它任何闸上都看不出差别**
(假设备照常给流、遮罩照常出现、上传照常发生)—— 只有 ㉚ 会红。

每次变异后都 `sha256` 对账还原(三发均 `YES`,回到 `9b5656f4505c16bd`),
🔴 因为本席今晚先前栽过一次:**跑出来的红/绿,也得先证明跑的是你以为的那份字节**。

**邻接闸(修完的树上跑)**:

    `renderer-in-console-iframe-regression.js`  通过 3 / 失败 0  rc=0(真 console iframe 路径)
    `renderer-regression.js`                    rc=0(自起静态服务 :46041,自挑端口 · 退出必收)

---

## 三 实测:哪些是**量出来**的

探针 `/tmp/dwprobe-b/probe9-gdm-constraints.js`(未入仓),Chromium **153.0.8010.12**,两档对照:

| 用例(调用形状) | A 档:带假设备开关 | B 档:不带任何假设备开关 |
|---|---|---|
| `selfBrowserSurface:'nonsense'`(**非法枚举值**) | **`TypeError`** | **`TypeError`** |
| `dwTotallyBogusMember:'x'`(**编造成员名**) | ok,拿到假流 | `NotSupportedError` |
| `{video:true,audio:false}`(**生产原形状**) | ok,拿到假流 | `NotSupportedError` |
| `+ selfBrowserSurface:'include'`(**候选修法**) | ok,拿到假流 | `NotSupportedError` |
| `+ preferCurrentTab:true` | ok,拿到假流 | `NotSupportedError` |
| `+ selfBrowserSurface:'exclude'` | ok,拿到假流 | `NotSupportedError` |

三条**实测**结论:

1. 🔴 **IDL 确实认识 `selfBrowserSurface`**。错误文本原文:
   `Failed to read the 'selfBrowserSurface' property from 'DisplayMediaStreamOptions':
   The provided value 'nonsense' is not a valid enum value of type **DisplayMediaIncludeOrExclude**.`
   ⇒ 这句同时给出:成员名、所属字典、**枚举类型名与它的两支取值**。
   🔵 而且它在 **B 档也抛**(B 档连采集都做不到,一律 `NotSupportedError`)⇒ **枚举转换发生在采集之前**,
   所以这个信号与"headless 能不能截屏"无关,干净。
2. 🔴 **未知成员被静默忽略**(编造成员名不抛 TypeError,与生产原形状同一结果)
   ⇒ 这一行改动对**不认识它的老浏览器无副作用**(会被当未知成员丢掉)。
   🔵 注意:这一步是"实测到的忽略机制 ＋ 推论到老浏览器",**老浏览器本身没跑过**。
3. 🔴 **假设备对"选源框里有什么"零判别力**:A 档 5 种调用形状拿到的流**逐字段相同**
   (`deviceId=screen:-3:0`、`displaySurface=monitor`、`1280x720`、`logicalSurface=true`、`cursor=never`)。
   ⇒ **任何**试图用 `--use-fake-device-for-media-stream` 来验"本 tab 有没有出现在列表里"的闸,
   都会绿得毫无意义。**这就是本件不写那种闸的原因。**

🔵 **另一条死路,写下来省别人半天**:`navigator.mediaDevices.getSupportedConstraints()`
**不含** `selfBrowserSurface` / `preferCurrentTab`(实测,两档一致)——
它返回的是 `MediaTrackSupportedConstraints`,**不是** `DisplayMediaStreamOptions`。
⇒ **不能**用它探测本格支持性;要探就用上面第 1 条那个"非法枚举值必抛 TypeError"的手法(㉚ 的守卫)。

---

## 四 🔵 读规范部分(🔴 **未实测**,与上面第三节分开写)

    Chrome 侧 `selfBrowserSurface` **默认 `'exclude'`**,其目的正是避免"hall of mirrors"(实时预览自我嵌套)
    `'include'`  ⇒ 把**当前标签页**加进选源列表
    `preferCurrentTab:true` ⇒ 把选源**窄化成只有当前标签页**(所以本件**不采**它,并用断言 2 钉住不许采)

🔴 **本席此轮无法复验这三条的出处**:`web_search` 工具在本席连调三次**零返回**
(不是"搜到说没有",是没有任何输出)⇒ 上表引自本轮早先读到的 Chrome 开发者文档 / MDN,
**我现在给不出可点的 URL,也不编一个**。

🔵 **但本件的核心断言不依赖这三条**:
   闸 ㉚ 钉的是"产品传了 `include`"(实测),守卫钉的是"浏览器认识这个成员"(实测)。
   "默认是 exclude ⇒ 业主此前截不到本 tab"这一因果**是读规范推论**,
   ⇒ 🔴 **它只能由业主的眼睛确认**(第五节)。

🔵 组长 `ts-No.26` 已撤回原第③条(自截无限镜像),理由与本席读码一致:
`captureScreenFrame()`(`renderer-canary-sse.html:4636` 起)是**抓一帧进 canvas → `finally` 立刻停轨 →
之后才 `shotOverlay.hidden = false`**,且那个 `<video>` **从未挂进 DOM** ⇒ 没有实时预览,不存在自我嵌套。
🔴 这仍是**读码**结论(五行代码可排除),没有实拍过"截本 tab"得到的那张图。

---

## 五 🔴 人工验协议(只能人工 ⇒ 请业主/组长照这个走,别改成"看起来验过了")

**前置**:装了本件修复的 renderer;用业主平时那条路进 **console**(不是独立 renderer 页)。

    ① 点 📷,看弹出的「选择共享内容」框。
       记:**有哪几个页签**?(整屏 / 窗口 / 标签页 三栏各自有什么)
    ② 在**标签页**那一栏里,找**当前这个页面自己**(DingWei console)。
       🔴 这一条就是本件修的东西:**修复前它不在,修复后它应该在。**
       → 在 / 不在,请逐字回一句。
    ③ 选它 → 确认。看**遮罩里那张底图**:
       预期 = **当时那一页的干净画面**(不含遮罩本身、不含选源框、不含浏览器边框与地址栏)。
       🔴 若画面里有遮罩/选源框的残影,那是抓帧时序问题,请立刻报 —— 那是本件**没**验到的地方。
    ④ 框一小块 → 确认。预期:输入框回填 `screenshot-*.png`,发出去,**收到的就是框选那一块**
       (这一段由 ㉗/㉘/㉙ 覆盖,但在"本 tab 真画面"上**从未跑过**,所以要眼睛过一遍)。
    ⑤ 🔴 **反向确认**:再点一次 📷,**其他来源**(别的窗口 / 整个屏幕)**是否仍然在列表里**?
       若只剩"当前标签页"一个选项 ⇒ 说明有人把 `preferCurrentTab` 加进去了,那是**回归**,请报。

**判据**:②③⑤ 三条都符合预期 ⇒ 本格关闭。任一条不符合 ⇒ 保留 ㉚,把现象原文报回来。

---

## 六 🔴 没验到什么(一字不删的传统;这一节是本件最值钱的部分)

1. 🔴 **选源框的内容,从未被任何自动化看见过。** headless 到不了那个框;
   假设备档对 5 种调用形状返回**完全相同**的流(第三节第 3 条)⇒ **零判别力**。
   ⇒ "本 tab 现在会出现在列表里" = **读规范推论 ＋ 待业主人工确认**,**不是实测**。
2. 🔴 **业主那台机器完全没实测过**:Chrome 版本、操作系统、PipeWire/portal 支持情况,一个都没量。
   板件 ③ 自己写着"未实测业主那台机器的环境"—— **这句到本件为止仍然成立**。
   ⇒ 若他的 Chrome 老到不认识 `selfBrowserSurface`,这一行会被**静默忽略**(症状原样不变),
   而 ㉚ **在他机器上不会跑**(它跑在我们的 CI/本席环境)。这是本件最大的残余风险。
3. 🔴 **守卫①的牙口是推理的,不是实测的。** 两半机制都量到了(非法枚举值→TypeError;编造成员名→被忽略),
   但"换到一个**不认识**这个成员的浏览器 ⇒ 守卫①会红"这一步,**我没有那样的浏览器可跑**。
   ⇒ 别把这条守卫当成已验过转红的闸;它是**显影剂**(与 (c) 那格的 L4 同一性质)。
4. 🔴 **截出来的图【内容】没验。** 假设备给的是合成画面、内容不可控(与 ㉕ 注释同因)。
   "截本 tab 得到的是干净一页、不含遮罩" = **读码结论**(抓帧在遮罩显示之前),🔴 未经任何人眼确认。
5. 🔴 **㉚ 只在【独立 renderer 页】(经契约 mock 服务)跑过,没在真 console 的 iframe 里跑。**
   推论上 `getDisplayMedia` 的约束与 iframe 无关(选源框属于**顶层浏览器**),
   🔴 但这是推论 —— **而业主走的正是 console 那条路**。本组在"推论等价 ≠ 实测等价"上栽过不止一次,
   所以这里如实写:**iframe 那条腿没跑**。第五节的人工协议**指定走 console**,正是为了补这一条。
6. 🔴 **"业主选了本 tab 之后"的下游没有专门腿。** 上传/裁剪那一段由 ㉗/㉘/㉙ 覆盖,
   但它们用的是**替身帧或假设备帧**,不是"本 tab 的真画面"⇒ 组合(真路径 ＋ 本 tab ＋ 裁对)**仍是结构性缺口**
   (与 U7 交付件第四节第 1 条同一个缺口,**没有被本件关掉**)。
7. 🔵 **`surfaceSwitching` / `monitorTypeSurfaces` 一行未试**;`preferCurrentTab` 只实测了
   "在假设备下不抛、能拿到流",**没有**评估它在真选源框里的观感(本件用断言 2 把它挡在门外,理由是可逆性)。
8. 🔵 **窄窗口 / 暗色档没有为这一格单独跑**(约束与视口、主题无关 —— 🔴 推论,未实测)。
9. 🔵 **没有在系统 Chrome(`PW_CHANNEL=chrome`)下跑 ㉚** —— 它自起 `chromium.launch(...)`,
   **不吃** `PW_CHANNEL`(与 ㉖ 同型),用的是 Playwright 自带 chromium。
   ⇒ 系统 Chrome 上的行为**未实测**(见第 2 条,业主那台更没测)。

---

## 七 建议登记(不属本格,报而不碰)

1. 🔴 **容器 PID 1 是 `sleep infinity`,从不 `wait()`** ⇒ 每次 Playwright 起浏览器泄漏的 chrome 进程
   **永远不被回收**。本席实测:`ps` 里 **968 个僵尸**(889 个 chrome),全部 `ppid=1`。
   `pid_max=4194304`、`ulimit -u=unlimited` ⇒ **当前不致命**,但它会让"进程数"这类观测失真,
   且**任何按进程数/句柄数设限的环境上会先炸**。修法是给容器一个真 init(`--init` 或 tini),**属部署侧**。
2. 🔵 **后台跑长任务的写法**:直接 `cmd &` 会在 exec 会话结束时被连带杀掉(本席基线第一轮就是这么丢的,
   日志停在 ⑲、无 rc 行,看起来像"卡住",实际是**被杀**)。⇒ 用 `setsid … < /dev/null &  disown`。
   这是**本席工具环境**的坑,不是产品问题,但写下来省别人一次误诊("以为套件卡在 ⑳")。
3. 🔵 板件 ③(锚 `d105665c`)那句「产品侧无从限制也无从放开」**请更正**(见第〇节)。

---

## 八 运行方式

```text
# 1) 契约替身(HTTPS)。:19443 是共享端口,跑完请收掉,别留给下一个人 EADDRINUSE
MOCK_CERT=<cert.pem> MOCK_KEY=<key.pem> python3 view-service/renderer-0206-contract-mock.py 19443

# 2) 套件(含 ㉚)。🔴 ㉖/㉚ 自起 chromium.launch 且【不吃】PW_CHANNEL ⇒ 需要 Playwright 自带 chromium
NODE_PATH=<playwright-node_modules> \
MOCK_BASE=https://127.0.0.1:19443 SHOTS=/tmp/xxx \
  node view-service/renderer-0206-file-render-regression.js
#   期望:通过 41 / 失败 0  rc=0

# 3) 邻接两件(各自带装置,不需要上面那个契约替身)
NODE_PATH=<playwright-node_modules> node view-service/renderer-in-console-iframe-regression.js   # 3/0 rc=0
NODE_PATH=<playwright-node_modules> node view-service/renderer-regression.js                     # rc=0
```
