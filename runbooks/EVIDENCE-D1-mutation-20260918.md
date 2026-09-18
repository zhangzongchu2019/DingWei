# D1 变异实证（按纪律丁重做）· `opus50tc2k`

- **纪律读到**：`82b27dd061b22fb2ee162ccd22d3ee61f71f3d03`（协作纪律丁：变异实证的呈交形态）
- **被测锚**：`a92ab621d9fbf35bbbae6f28dcad15f60f43822f`（D1 实现）
- **执行时间**：2026-09-18T12:55Z

## 0. 组长问的那一句：**带筛。**

我先前提交的变异实证**带 `-run` 筛**，两轮都带：
首轮 `-run 'Console'`、第二轮 `-run 'ConsoleSession|RenewedCookie|RevokedConsole'`。
下面按纪律丁①**整包重跑**，附 `-v` 的实际执行条数与 `--- FAIL` 行。

## 1. 跑法（不带任何 `-run`）

    go test ./internal/m8/ ./internal/store/ -v -count=1

**基线（未变异）：执行 376 条，PASS 376 / FAIL 0。**
以下每个变异体都是同一条命令，**执行条数恒为 376** —— 条数本身就是「没被筛掉」的证据。

## 2. 七个变异体，逐条对应它该打中的守卫

| 变异 | 执行 | PASS/FAIL | 变红的用例 |
|---|---|---|---|
| **M1** 续期整段短路 | 376 | 373/3 | `ConsoleSessionSlidesWhenPageKeepsPolling`<br>`RenewedCookieMatchesLoginCookieAttributeByAttribute`<br>`ConsoleSessionCappedByAbsoluteTTL` |
| **M2** 去掉续期时的 `Set-Cookie`（组长硬闸） | 376 | 374/2 | `ConsoleSessionSlidesWhenPageKeepsPolling`<br>`RenewedCookieMatchesLoginCookieAttributeByAttribute` |
| **M3** 去掉 30 天绝对上限（D1-b） | 376 | 374/2 | `ConsoleSessionCappedByAbsoluteTTL`<br>`ConsoleSessionAtAbsoluteCapStopsRenewing` |
| **M4** 去掉「剩余不足一半才续」 | 376 | 375/1 | `ConsoleSessionNotRenewedWhileMoreThanHalfRemains` |
| **M5** store 去掉 `revoked_at IS NULL` | 376 | 374/2 | `TouchConsoleSessionGuards/阴性:已吊销的不得被续活` |
| **M6b** store 删掉 `expires_at>now` | 376 | 374/2 | `TouchConsoleSessionGuards/阴性:已过期的不得被续活` |
| **M7b** store 删掉 `expires_at<新值` | 376 | 374/2 | `TouchConsoleSessionGuards/阴性:只延不缩` |

（M5~M7b 的 FAIL 计数为 2 是因为父用例 `TestTouchConsoleSessionGuards` 与其子用例各计一行。）

### 2.1 🔴 组长复核补打的两发（2026-09-18T13:10Z，锚 `82b27dd0`）

组长按纪律丁在**我自己点名的那个风险场景**上打了一发，打出了一条**空断言**：

| 变异 | 执行 | PASS/FAIL | 结果 |
|---|---|---|---|
| **M8** 登录侧（`console_login.go:244`）拆出构造器 ＋ `Secure: true→false` | 376 | 374/2 | `TestConsoleLoginHTTPPageToCookie`（既有）<br>`TestRenewedCookieMatchesLoginCookieAttributeByAttribute`（**修复后才红**） |
| **M9** 对称：**续期侧**拆出构造器 ＋ `Secure: true→false` | 376 | 375/1 | `TestRenewedCookieMatchesLoginCookieAttributeByAttribute` |

**修复前 M8 下我那条断言是绿的**，因为：

    want := newConsoleSessionCookie("raw-a")   // ← want 与 got 同源，都出自生产构造器

⇒ 它断言的是「续期 == 构造器」，**不是**「续期 == 登录」。
而我保留这条断言的**唯一理由**就是「共用构造器将来可能被拆开」——
**那正是它兜不住的那一种**。名字叫 `MatchesLoginCookie`，测的却不是登录。

**已改**：`want` 改为走一次**真实登录流程**（`consoleLogins().issue` → `dispatchConsoleLogin`
→ `HandleConsoleLoginStatus`）后取其实际 `Set-Cookie`，再逐属性比对。
`Value` 不参与属性比对（登录发新签的 token，续期必须发**手上这张**），
另立一条断言 `got.Value == "raw-a"` —— **续期若换 token，等于把凭据在无声中轮换掉**。

### 2.2 组长新判据「一发好变异应当【只】打红它针对的那条」—— 我据此复验（2026-09-18T11:55Z）

**M8 红了两条，这不是粗变异。** M8 打的是登录侧 cookie 构造，两条红各有其职：
`TestConsoleLoginHTTPPageToCookie`（既有）守的是「登录 cookie 必须 `Secure`」这条**绝对属性**；
我那条守的是「续期 == 登录」这条**一致性**。**两条红都是信息，不是溅射。**
真正单打一条的是 **M9**（只改续期侧）：376 条、375/1，**只红我那条**。

### 2.3 🔴 由这个判据反查，查出我那条断言的一个真边界

**M10：把共用构造器 `newConsoleSessionCookie` 本身的 `Secure` 改成 `false`** ——
登录与续期**一致地**变差。结果（修补前）：

    执行 376 条 · PASS 375 / FAIL 1 · 只红 TestConsoleLoginHTTPPageToCookie
    --- PASS: TestRenewedCookieMatchesLoginCookieAttributeByAttribute      ← 我那条是绿的

⇒ **一致性断言看不见「两边一起变差」。** 我那条测的是 A 与 B 相等，
而 A、B 同时降级时它照样相等。此时唯一的保障落在**别人写的那条测试**上 ——
若那条哪天被删，一次统一降级将无人接住。

**已补**：在同一用例里再钉一遍**绝对属性**（`Secure` / `HttpOnly` / `SameSite=Lax` / `Path=/`），
不依赖另一条测试继续存在。复跑 M10：

    执行 376 条 · PASS 374 / FAIL 2 · TestConsoleLoginHTTPPageToCookie ＋ 我那条

📌 记一条：**「A 与 B 一致」型断言，天然测不出 A 与 B 的共同基准整体位移。**
它和 `want`/`got` 同源是近亲 —— 前者是两个对象**同源**，后者是两个对象**同步漂移**。
两种都能让一条名字很像样的断言在关键变异下保持绿色。

📌 **假绿的又一种：`want` 与 `got` 同源。**
它比「断言恒真」更难看出来，因为断言里有真实的比较、有真实的两个对象，
只是**两个对象来自同一个源头**。一个测试名字里写着 A 对 B，实际测的是 B 对 B。

🔴 **M6 / M7 的第一版是粗变异**（把 SQL 条件改成 `?=?`），它连正例一起打红 ——
**那只能证明「动了会红」，不能证明「这道守卫被那条阴性对照盯着」**。
M6b / M7b 是删掉整个条件并同步去掉参数的**外科式变异**，各自只打中对应的那一条阴性对照。
**粗变异会高估覆盖，我把它换掉了。**

全部 `-v` 原始日志：`/tmp/mut/*.log`（本机；如需入仓请组长说一声，我不擅自把 8 份全量日志塞进仓）。

## 3. 首轮筛法漏了什么 —— 用组长给的自查两行验

    grep -ohE "^func Test[A-Za-z0-9_]+" <测试文件>... | sed 's/func //' \
      | while read n; do case "$n" in *Console*) ;; *) echo "漏: $n";; esac; done

    漏: TestRenewedCookieMatchesLoginCookieAttributeByAttribute

⇒ 首轮 `-run 'Console'` 下，**执行硬闸的那条测试从未运行**。
对照 M1 的整包结果（3 条红）与首轮筛法结果（2 条红）：**差的正是这一条。**

## 4. 🔴 比「漏掉自己的测试」更值得记的一条

第二轮筛法 `-run 'ConsoleSession|RenewedCookie|RevokedConsole'`，
用自查脚本对着**我自己新增的两个测试文件**跑，**一条不漏** —— 看起来没问题。

但它**实际只执行了 15 条，而整包是 376 条**。

⇒ **筛选器的风险不止于「漏掉你自己写的测试」，而在于它把【别人写的全部不变量】一起排除了。**
📌 实证：D1 开发中我把 `consoleTouched` 写成懒初始化，
被仓里既有的 `TestNewInitializesEveryHubMap` 抓红 ——
**那条测试的名字与我的任何 `-run` 模式都不沾边**。
若我只跑筛选集，这个缺陷会一路绿到评审。

⇒ 所以纪律丁②「优先整包跑」比③「附对照证明目标测试在选中集里」更强：
**③ 只能保证你想到的那些在集合里；② 连你没想到的也在。**

## 5. 结论边界

本件只证明：**上述九个变异各自被至少一条用例盯着，且那些用例在整包跑里确实被执行。**
不证明：守卫本身在生产语义下正确（那要真环境），也不证明没有第八道该有而未写的守卫。
