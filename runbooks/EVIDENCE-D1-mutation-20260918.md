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

本件只证明：**上述七道守卫各自被至少一条用例盯着，且那些用例在整包跑里确实被执行。**
不证明：守卫本身在生产语义下正确（那要真环境），也不证明没有第八道该有而未写的守卫。
