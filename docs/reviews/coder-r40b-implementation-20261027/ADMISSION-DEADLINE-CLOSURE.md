# 追加：admission 截止、阻塞 transport 隔离、迟到原生证据

响应组长10:50:56Z／10:52Z／11:04Z与11:31Z规格，原提交不改写。当前为实现边界，完整门结果另追加；不部署、不改席位env文件、不重启在线helper。

- `SH_ADMISSION_TIMEOUT_SECONDS` 默认60秒，独立于 `SH_TURN_TERMINAL_TIMEOUT_SECONDS` 默认1800秒。前者从持久dispatch intent的真实时刻折算单调起点，不因deliver返回、processing或partial进展重新计时；只有正证据admitted后才使用terminal进展watchdog。无持久件的本地输入从派发尝试开始计时。
- deliver调用由绑定旧driver实例的独立线程执行。无accepted或stdin/RPC同步阻塞到截止，先落盘unknown、入ACK outbox、告警并释放current；原件不自动重投、不合成终态。
- 恢复在独立线程进行，FIFO在恢复完成前不再写旧stdin：Claude先kill并确认子进程与reader退出，再等旧deliver线程退出、close，构造并启动新driver。先close buffered stdin会等待被阻塞的写锁，因此必须先kill。Codex只终止本driver的stdio子进程或关闭本WS连接，绝不kill共享daemon。
- 不能证明旧写线程退出或恢复失败，保持明确告警与dispatch隔离，后续件保持queued；不会拿同一堵塞stdin继续派发。正常恢复后推进后续件。CLI进程内未落盘上下文可能丢失，原未知回合必须核原记录，不能宣称无损恢复；helper与inbox继续在线。
- c按11:04Z修正规则：原turn的完整原生正证据可迟到补账；保留原admitted/processed时刻及原turn，不重派。真实wire测试运行实际Claude、受控模型、原生driver、ACK outbox、WS、Hub及浏览器：先transport unknown，再真实terminal补账，显示“迟到确认”，模型请求仍仅一次。已实跑rc0。
- d：共享HomeIsolation与新增dispatch fixture在夹具内部剥离SH_/DW_/DINGWEI_/DATAD_，再设置夹具显式变量；真实席位文件不修改。`scripts/test-helper-suite.py`默认串行运行claude/codex两个外层profile，专项fixture自行明确driver证据合同；可用 `--driver unset` 原样复现历史入口。
- e：test_unified_queue.py与test_dispatch_lifecycle.py已在run-build-tests的69组Python列表内，完整helper discover是另一道独立门。

## S-BUILD-GATE-HELPER-COVERAGE

登记：build.sh／make test／CI统一构建入口为显式文件列表，不等于helper全量discover。此前缺少unified_queue与dispatch_lifecycle，已随watchdog提交补入；ACK outbox等未列入的helper测试仍须通过独立全套门。交付必须同时报告统一构建入口的实际组数、完整helper逐例总数、profile与命令，禁止以“69组通过”替代“完整helper通过”。本次不把其余历史差集扩成范围外产品修改。

## 当前专项与待关闭门

- claude/codex专项各24项PASS，覆盖独立默认/可配值、缺accepted、阻塞deliver、真实OS stdin阻塞、intent起点、恢复成功后推进、恢复失败隔离。
- 原统一队列各14项PASS；ACK outbox各11项PASS。
- 实际CLI迟到wire端到端rc0，View含迟到确认。
- 基线a738同历史脚本、unset profile、私有测试HOME：989项／429.004s／rc0，无清理异常；因此不能把r40b临时目录错误宣布既有。真实HOME基线对照与r40b清理定因继续运行，完整门尚未关闭。
