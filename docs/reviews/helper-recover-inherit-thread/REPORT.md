# S-HELPER-RECOVER-INHERIT-THREAD：r59 候选

需求来源：opus50tc2k.zzc 2026-11-12 派单，㊿-补103，投递可靠性第一优先；截止2026-11-12T20:00:00Z。基线 coder56solzzc/r58-integration 3c9f2722fd5b2e05b52188a8ccb664427f5debdd；分支 coder56solzzc/helper-recover-inherit-thread。执行线程01a1ceef-693f-7b22-97c4-bf4d5017ccd3，原子写锁 /tmp/coder56-helper-recover-inherit-thread.lock。未改VERSION，不构建发版制品、不部署或重启。

## 行为与实现

adapter 恢复时仍先中止旧传输并等待旧投递worker退出。停止旧driver后，新建替换driver通过 inherit_recovery_thread 接收旧对象最终的根号（不是dispatch开始时缓存号，也不从账本最大号或 thread/list 猜）。新旧 lane/generation 必须一致，继承清校验缓存；同lane上下文与动态工具一并保留。显式绑定配置仍优先，不被运行态提示覆盖。

替换driver首封新信前沿用原 thread/read 校验；notLoaded 才 thread/resume，保留 ID、cwd、入场状态与 canAcceptDirectInput 检查。成功时沿用原号，无 thread/start。只在继承的未绑定根遇到 missing_thread 或 ThreadUnavailable（ID/cwd/入场状态不符）时允许换新；认证、连接失败、未知RPC错误都不会触发换新，子分道亦不换根。

确认换新成功得到新号后，helper stdout 输出一行固定告警，且既有事件通道发一个 state_change；两者均在新根 turn/start 前。告警只含旧新号与原因码，不含服务端异常正文：

```text
[delivery][THREAD_REPLACED] old_thread=<旧号> new_thread=<新号> reason=<missing_thread|thread_unavailable>
```

事件 data 为 change=delivery_thread_replaced、old_thread_id、new_thread_id、reason；session_id 为新根。使用 driver._emit_event→adapter._event_json/_event_q→现有发送链路；不改 Hub。成功换新后再 read 校验新号/cwd再入场。创建响应丢失、后续校验或private落盘失败时，本对象进入拒收状态，后续投递不自动重复 thread/start，需运维核对创建结果；不伪造创建失败或原线程不可用以外的原因码。

## unknown 原信的处境

触发恢复的那封信继续记 unknown/原持久状态，不由恢复代码重新调用 deliver。即便后续以相同 envelope 请求命中驱动账本，也沿用持久幂等回执，不再 turn/start。原信**可能已经在原线程执行或仍在执行**：RPC响应/通知丢失、写出后超时无法证明未执行。继承成功不等于原信未执行，也不把 unknown 改成 processed；原有精确thread/turn/clientId与原生起止时间对账规则不改。线程换新后也不把原信重投到新根。

恢复沿用原号时，下一封信仍走既有忙等待/入场边界，不能越过原生正在运行的回合。若旧对象没有已知根号，无法凭空恢复一个根，保留原初次创建路径；此变更不新增 helper 跨进程重启持久绑定策略。

## broker 与 private 边界

- sub_agent_runtime 的根恢复工厂改为动态读取当前 root_adapter.driver._thread_id，避免捕获首次driver；第2次恢复不会回到首次旧号。重建根携带原private endpoint对象，供受控落盘；子driver不携带根private绑定。
- broker 原有普通 bind 不可变、跨lane请求白名单保留。新增 helper 内部 replace_root_thread 仅接受当前root facade、空lane、准确旧号及其现有所有权；共享唯一写队列和private admission门不变。新建成功且新号不属于其它lane才替换根映射，子facade映射不变；子facade不能调用该入口换号或向根写入。
- private replace_thread 沿用 writer、boot/config 验证，要求当前绑定与继承旧号一致才保存新根。普通 save_thread 冲突保护和显式绑定冲突行为不变。落盘失败有新建告警但不继续模型入场。
- 本单只实现根恢复；不允许子分道校验失败时自己新开并冒充原lane，也不把 SH_CODEX_BOUND_THREAD_ID 下传给子分道。

产品改动限于 tools/sessionhelper/sessionhelper 下 driver_adapter.py、codexDriver.py、sub_agent_runtime.py、app_server_broker.py、private_endpoint.py。新增9个单测复用 test_codex_stale_thread.py 与 test_sub_agent_helper.py；库存文件不变。OpenSpec 变更 helper-recover-inherit-thread，4条新增需求与5个场景。

## 单测与语义撤防

定向两个模块79例，rc0；新增9例独立再跑rc0。所有transport均为本地合成，数据库/绑定目录均临时，普通uid1001，env -i +临时HOME，不访问运行席位或真实推理。原文见 raw/focused.log 与 raw/new-tests-final.log。

变异在一次性源码副本逐刀执行，不修改受测工作树；绿控制5例rc0，9/9刀均为指定断言FAIL、rc1。每刀selector、判据与原文见 raw/mutations/mutations.json 和对应log：

| 刀 | 撤掉的语义 | 红在判据 |
|---|---|---|
| inherit | adapter不传旧根 | HRI001_INHERIT_FINAL_ROOT |
| fallback | 禁止不可用根换新 | HRI002_FALLBACK_NOT_ADMITTED |
| warning | 去掉固定stdout告警 | HRI002_FIXED_WARNING_MISSING |
| event | 丢失状态事件change | HRI002_STATE_EVENT_MISSING |
| explicit-bound | 去掉显式绑定策略字段 | HRI002_BOUND_OR_CHILD_OPENED |
| dynamic-root | 根工厂重新捕获首次根号 | HRI004_ROOT_FACTORY_CAPTURED_FIRST_ID |
| no-replay | 去掉历史回执幂等拦截 | 原unknown投递返回processing而非failed，定位uncertain原信的断言；不会等到下一次调用才报红 |
| cross-lane-write | 放宽facade线程白名单 | child向latest根号请求未抛PermissionError |
| private-hint | private保存旧号而非新号 | HRI004_PRIVATE_HINT_NOT_REPLACED |

初次变异汇总曾因no-replay已经在更早断言失败而未匹配后续标记，判据匹配器已改为该实际原信断言，再跑9/9全红；没有以其它测试失败充数。先前诊断保留在本机 /tmp/hri-ceef/raw/mutations，交件使用 mutations-final 的最终原文。

## 全量门与交件

两种 PATH 的完整 scripts/run-build-tests.sh 均rc0，均输出原文 BUILD_TESTS_OK helper_profiles=2 python=27 shell=2。4个helper profile全部 expected/actual=1190/1190（1181+9），库存94文件；27个Python文件与2个shell回归均完成。没有实际unittest或Go SKIP用例（业务日志中的 skipped= 去重计数不算测试跳过）。OpenSpec 1.16.0 validate --all --strict 为37 passed/0 failed、rc0；git diff --check rc0。VERSION及helper版本文件相对基线保持不变，版本0.2.3.38。

| PATH场景 | UTC开始～完成 | 全门rc | helper两profile |
|---|---|---:|---|
| 常规，codex=/usr/local/bin/codex | 09:01:07.380125Z～09:12:02.282987Z | 0 | claude 1190/1190，codex 1190/1190 |
| 无codex，shutil.which确认None | 09:01:08.545236Z～09:12:04.945221Z | 0 | claude 1190/1190，codex 1190/1190 |

日期均2026-11-12。完整逐行日志分别在本机 /tmp/hri-ceef/raw/canonical-normal.log（1168061字节，SHA256 edffbdf746f631ddfcee378f97a24847567463596fed71c2348b087022252805）、canonical-no-codex.log（973421字节，SHA256 d8c7a49abaab1f9d259c37aa8ea1971a6f55c069831530ca6545e32569d7d226）；按仓库大文件规则不入Git。原始计数/profile/结果/shell回归读数逐行摘录在raw/canonical-*-excerpt.log；命令、PATH、UID、时间、rc、完整日志hash在raw/canonical-*.json与gate-summary.json，未改写原文读数。raw/skips-*.log为空清单，未将缺环境跳过算成通过。

复跑：先创建临时HOME/TMPDIR，以 env -i HOME=<临时目录> TMPDIR=<短临时目录> PATH=<下述PATH> HUB_TEST_PG_STRICT=1 LANG=C.UTF-8 TZ=UTC sh scripts/run-build-tests.sh 执行。变异：同样隔离环境，python3 docs/reviews/helper-recover-inherit-thread/mutations.py --output <新空目录>。定向：PYTHONPATH=tools/sessionhelper:tools/sessionhelper/tests python3 -m unittest -v test_codex_stale_thread test_sub_agent_helper。

复跑使用 /tmp/coder-0232-locked-venv/bin 的锁定Python环境；常规PATH加 /usr/local/bin:/usr/bin:/bin；无codex PATH以仅含node的临时bin替代/usr/local/bin。两者 env -i、独立临时HOME和短TMPDIR；canonical本身再隔离SH_/DW_/DINGWEI_/DATAD_与CLI状态目录，并运行claude/codex两个profile。不存在在线helper/Codex守护进程调用或升级动作。

最终commit/tree/父与远端ls-remote同值坐标由交审回执提供，不自验收，由opus独立复核及安排r59集成。
