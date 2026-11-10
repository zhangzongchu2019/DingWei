# r57 集成交件

基线 main `3b6157859f5a329026bfcf43bf95de81909919f8`；分支 `coder56solzzc/r57-integration`。唯一写者 `01a1c75a-48c7-7691-87d6-b814e78ea829`，经组长明确裁定接手；接手时工作树 clean、远端分支为空、tip/tree 及五个 merge 均符合。旧锁记录保留，接手证据见 raw/takeover.json。

五个 no-ff merge（顺序）：

1. `0e16a0c841099f8651ee511708926d2b040f2238` serverInfo（冻结候选 ba7670db）。
2. `bb85442f1da46c6357ed725f11d76e82a802ea82` LEN（aed9cf68）。
3. `a9d922beb9b276b30199e230e13e8b64093c32f3` PG 登记（24e9e95b）。
4. `b577e59ea446f3dd43deac80f86401127c0fc9c8` ZERO-CONSENT（c1164243）。
5. `fabc996e73ab1514988b4d81a914c17dee41c27e` M3（c334fb10）。

后续普通修正 `4749e901fad104e2a67659bc1f9144a7753c3889`（tree `4362fa4e41126b9c8652265250dada99c2672615`）仅改 helper 清单测试钉值及 CONFLICTS.md。五个 merge 未改写；VERSION 仍为 0.2.3.36。冲突逐项处置及无文本冲突的计数语义冲突见 CONFLICTS.md。

## 门禁读数

| 门 | 结果 | 原始证据 |
| --- | --- | --- |
| go build ./... | rc0，沿用修正前读数 | raw/build.* |
| m8 全包 -count=1 -p 1 -timeout 40m | 1543 PASS / 0 FAIL / 37 SKIP，沿用 | raw/m8.* |
| store / migrate / mcpgw / cmd/workpulse | 分别 609/0/5、44/0/0、263/0/6、267/0/6，沿用 | raw/packages.* |
| 全仓 go test -json -p 2 -timeout 40m ./... | 5142 PASS / 0 FAIL / 71 SKIP，沿用 | raw/all-go.* |
| sh scripts/run-build-tests.sh | rc0，BUILD_TESTS_OK；claude/codex 各1168/1168、93文件，Python27／shell2 | raw/canonical.* |
| python3 tools/run-browser-regressions.py（无筛选） | 40/40，aggregate_rc=0 | raw/browsers.*、browser-receipt.txt |
| DW_BROWSER_E2E=1 mcpgw 四个浏览器测试函数 | rc0，8 PASS / 0 FAIL / 0 SKIP（含4个子测试） | raw/mcpgw-browser.* |
| OpenSpec 1.16.0 validate --all --strict --no-interactive | 35 passed / 0 failed | raw/strict.* |
| git diff --check | rc0；证据追加后再查完整基线到最终 HEAD | raw/diff-check.* |

各命令、cwd、起止 UTC、退出码和 Go 跳过用例名／条件保存在对应 result.json；环境白名单见 raw/gate-env.json。原始日志以 gzip 无损压缩，SHA256SUMS 记录压缩字节校验。canonical 完成于 2026-11-10T21:38:23Z，最后四门完成于 21:47:01Z。

浏览器 runner 的 renderer_c2 保留既有 T-166 的4项 KNOWN-DEFECT（形态变化0，HARD PASS25）；全量 runner 按既有规则 rc0。本次没有修复或宣称关闭该历史缺陷。其余 Go SKIP 条件逐项见 result.json，不将可选门跳过冒称实跑。

## 首红与本次修正

修前 canonical 在 helper 清单断言报 93≠92；main 对照 rc0。组长 ededa638501f 裁定为两侧各91→92、同文本自动合并导致的语义冲突。修正钉值93并核93唯一行、92 under tests/、两新增项齐全；同类计数三方审计无其他共同变更项。首红和 main 对照原文保留在 raw/superseded，不算本次通过读数。

本次修正未改 Go，依组长明确指令沿用已绿 Go 门；其余门在4749e901f上完整重跑。后续仅追加证据与交接记录，产品／测试输入字节不变。不自验收，等待组长独立复核；不构建发布件、不部署、不重启、不改在线环境或真实数据库。
