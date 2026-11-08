# 验证账本

- 原生失败复现：`python3 -B tests/helper-codex-delivery-reconcile/real_protocol.py --binary /tmp/coder-stale-protocol-01592/codex --output /tmp/helper-reconcile-evidence/native-review`，rc1；7项：FAIL4/PASS3/ERROR0/SKIP0。失败分别为A原生时间丢失、A替换后无终态恢复、B当前绑定不按clientId恢复、B历史绑定不按clientId恢复。`raw/native-final/`逐格记录本次原生请求/响应及本地状态；自己的子进程全回收。
- 等忙超时：`python3 -B tests/helper-codex-delivery-reconcile/timeout_feedback.py`，rc1；1项FAIL，ERROR0/SKIP0。真实driver/adapter/inbox配协议屏障夹具，turn/start=0仍请求替换健康传输一次；替换函数仅观察，不真的启动或关闭传输；自己的worker已join。
- 产品文件：`git diff HEAD -- tools/sessionhelper/sessionhelper`输出为空；hash见raw/product-source-pins.json。基线、分析及红测不含产品修复，不声称产品PASS。
- 规范构建门第一轮系统Python：rc1，三闸通过，helper discovery缺test_desktop_endpoint/test_guide_staging/test_ipc_socket_path_isolation/test_sessionhelper四模块。原文raw/canonical.log，环境改用已有锁依赖venv。
- 规范门第二轮锁依赖venv：rc1，两profile各1097项，两个legacy测试ERROR（缺硬引用b5faf73c8fe4d5c71720daf4eed5b133b8d45dbd的Git对象）。raw/canonical-locked.log.gz；已从工蜂精确fetch该历史对象，不改测试或产品以跳过。
- 规范门补齐历史后的最终结果：rc0，2026-11-08T10:43:47.171373Z～10:53:38.238482Z。三闸通过，helper两profile各1097/1097，27个Python组与两个shell门通过，末行BUILD_TESTS_OK。原文raw/canonical-final.log.gz，执行元数据raw/canonical-final.json；前两轮仍保留真实rc1。
- 本变更OpenSpec strict：`OPENSPEC_TELEMETRY=0 /tmp/coder56solzzc-openspec-1.16.0/node_modules/.bin/openspec validate helper-codex-delivery-reconcile --strict` rc0。
- 全仓OpenSpec strict：27 passed/1 failed，rc1；唯一既有项r52-delivery-status缺delta。该基线目录只有design/tasks，没有specs；本单未改其目录。原文raw/openspec.log.gz、raw/openspec-baseline-r52.log.gz，不声称全仓strict绿。
- 新增测试均为显式入口，位于tests/helper-codex-delivery-reconcile，不扩大现有helper默认库存；预期红并非expectedFailure。SKIP为0。原生终态用自己turn/interrupt生成interrupted，不声称自然completed/failed的全覆盖或真daemon重启E2E。
- 规格漂移核验：工蜂rulings从24985813到10edfeddfab85de74e4a3823bf3ac58afd0de0c5，仅rulings文档追加；〔51〕-补1为待业主决定的子Agent路由提案，仍不授权本单实现。r54-integration仍6e2c8364。随后核到fe847fc9并fetch/read〔51〕-补2，业主已定三项，但下一步详细设计仍另派；未扩大本单。详见tasks交接与后续最终核验。
