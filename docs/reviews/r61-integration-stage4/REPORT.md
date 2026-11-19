# r61 第四段：冻结N-1c合入与轻量门

状态：**本段轻量门通过；全门按令暂缓，非发布PASS或GO。**

依据组长2026-11-19T04:18:57Z令（本席收件dw-dlv_4aec11ae4fd2071cbfc4f6bbc83a5ec7）。合前实际ls-remote：r61-integration=d6e56a8b13bcb622eb0610e4a7cd129f834d9ff3，N-1c=1362f08f4f8dbb1c621319215ab414f6791b8778。普通no-ff合后源 `acd6ead19dab1ae969d7c72b58359967a20c7018`、tree `d12d5411911cb2ebc9a6d45b9680b745d5778bd5`，父严格为上述两锚。ort自动合并dispatch_lifecycle与sub_agent_helper测试，无冲突、无手工产品改写。

保护核验：d6已有c6f935096的 `test_aa010_feature_off_drains_without_native_or_capability` 方法与合后源按AST截取完整文本，SHA逐字相同；仍含等待AA010_FEATURE_OFF_DURABLE_RETURNS_SETTLED，未回退为只看send计数。VERSION保持0.2.3.40，NAME与GROUPS均未合入。

## 固定源轻量门

普通用户、白名单、Go1.22.2、锁定Python3.12.3、独立HOME/TMP，SOURCE_COMMIT=acd6ead19dab1ae969d7c72b58359967a20c7018。只跑本令轻量部分：

| 格 | 结果 |
| --- | --- |
| go build ./... | rc0，仅本地编译，不作发布件 |
| 本席真实worker／adapter／SQLite＋合成RPC FIFO连续Timeout | rc0，队首先走 |
| 同上initialize -32603两次暂错 | rc0，队首先走 |
| blocker授权读失败让行 | rc0，普通投递可前进 |
| blocker＋transport恢复交叉 | rc0，恢复未set时零提交，set后恰一次 |
| R6首次启动确定性鉴权拒绝＋AA010单格 | rc0，2 tests OK |
| canonical helper部分双profile | rc0，Claude1290、Codex1290，各groups1281+9，inventory97 |

原命令、UTC、环境与原始日志SHA见evidence/light-results.json；双profile直接调用仓内scripts/run-helper-build-tests.py，未把它说成完整BUILD_TESTS_OK。

## 下一段条件与边界

本段没有启动整套canonical、m8/store三遍、focused race、浏览器、三向兼容或strict；等第五段选择后对最终合后源只跑一次。AUTH-FIRST单分支完整门是独立实现交件要求，不能冒充本段集成门。

若qwen在2026-11-19T09:00:00Z前对AUTH-FIRST PASS，则在本段tip上no-ff合该冻结锚再跑全门；否则只对第四段跑全门，AUTH-FIRST走r61后的热修。收到结论前不擅自并入。全门后另一个提交抬VERSION至0.2.3.41，再按令跑m8全包与run-build-tests。不推main、不部署、不出发布件；组长另发构建与GO。

交接：完成N-1c冻结合并与七项轻量门，无本段产品阻塞；剩余为第五段选择、完整门与独立终审。作者门不替代组长签收。
