# S-HELPER-BOUND-THREAD 交审证据

分支 `coder56solzzc/helper-bound-thread`，基线/唯一初始父 `329eb9f9c45d2d785a0429b01eaa0fa5264fe15d`。写者 `01a1c782-7090-7071-8d99-6ea51512ac13`；原子锁 `/tmp/coder56-helper-bound-thread.lock`，开工 UTC `2026-11-11T23:17:56Z`。派单 ACK 的原始字段为 `to=opus50tc2k.zzc#enrolled`，request_id `33a596825846`。

## 范围与行为
根配置 `SH_CODEX_BOUND_THREAD_ID` → Config → 根 driver，构造时固定 ID。通过现有 read/resume 的 ID、cwd、状态、直接输入能力校验后才投递；失败为 `bound_thread_unavailable`，不创建替代 thread。private 旧绑定冲突时启动报两个 ID 并释放 writer，不写 binding；无冲突只保存校验成功的 ID。已发出 turn 的未知状态仍按原证据规则处理。

M2 重建根 driver 保留根要求；子 lane 使用自身运行时 ID，不继承根配置；broker 源码零改动。未设置时工厂不新增构造参数，原用例断言保留。增量产品文件仅 helper 的 config、driver_adapter、codexDriver、sub_agent_runtime；不改 Hub、版本号、依赖锁或发布构建，不部署。操作员步骤见 `docs/sessionhelper/CODEX-BOUND-THREAD.md`；private 已绑定不同 ID 的换绑需要独立端点维护，本功能不强制覆盖。

## 判据与读数
- a：配置根 read 后 turn/start.threadId 精确相等；notLoaded 恢复同一 ID；无 thread/start。
- b：缺 thread、read/恢复失败、ID/cwd/状态/直接输入不符都 failed＋bound_thread_unavailable；无 thread/start/turn/start。失败回执持久幂等，adapter 事件显示同一原因且不发成功 ACK。
- c：未设置的工厂参数逐项不变；原 codexDriver 测试纳入完整双 profile 门。
- d：private 冲突在 app transport 连接前拒启，错误有两个 ID，未保存绑定且释放 writer；匹配和空文件正格通过。
- e：一次性源码副本撤掉 _ensure_thread 的显式根保护，b 用例红：错误返回 processing，断言 BOUND_UNAVAILABLE_FELL_BACK_TO_NEW_THREAD。未污染工作树。
- g：配置根＋M2 创建子 lane、受控 inbox 真实写入流程、父子 turn ID 分开、跨 lane 写拒绝、根重建保留绑定通过。既有 M2 单测全绿。

定向 `test_codex_stale_thread`＋`test_sub_agent_helper`：70 例，rc0，3.858 秒；完成时刻见 `raw/focused-metadata.json`。撤防 7/7 红，绿控制 6/6，UTC `2026-11-11T23:28:58.174986Z`～`23:29:00.764146Z`，各刀 selector、rc 与原始失败见 `raw/mutations.json`、对应日志。覆盖根配置、不得新建、未设置、private 冲突、子隔离、根重建、adapter reason。

OpenSpec 1.16.0 `validate --all --strict`：37 passed／0 failed，rc0。源码 SHA256、定向及 strict 原始日志在 raw/。f：完整 canonical `run-build-tests` rc0，`BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`；claude、codex 各 expected/actual=1179/1179，库存 93 文件，分组为 1170＋9。两套 helper 均无失败或跳过。UTC `2026-11-11T23:28:08.201896Z`～`23:37:22.775897Z`。原始完整日志保留本机 `/tmp/helper-bound-thread-c782/canonical.log`；因其体积约 1 MB，不加入 Git，逐行摘录和完整日志 SHA256/字节数见 `raw/canonical-excerpt.log`、`raw/canonical.json`。

`git diff --check` rc0；最终 tip、tree 与推送后 ls-remote 核验通过的坐标见交审回执。重启恢复后重新核锁、HEAD 和门完成元数据，未重复提交或重跑已通过的门。

## 复跑
使用锁定 venv、普通 uid1001、隔离 HOME、短 TMPDIR，不带在线 SH_/DINGWEI_ 凭据。原生交互全为本地合成 app-server；账本临时创建。

```sh
env -i HOME=/tmp/hbt-review-home TMPDIR=/tmp/hbt-review-tmp \
  PATH=/tmp/coder-0232-locked-venv/bin:/usr/local/bin:/usr/bin:/bin \
  PYTHONPATH=tools/sessionhelper:tools/sessionhelper/tests \
  python3 -m unittest -v test_codex_stale_thread test_sub_agent_helper

env -i HOME=/tmp/hbt-review-home TMPDIR=/tmp/hbt-review-tmp \
  PATH=/tmp/coder-0232-locked-venv/bin:/usr/local/bin:/usr/bin:/bin \
  python3 docs/reviews/helper-bound-thread/mutations.py --output /tmp/hbt-review-mutants

env -i HOME=/tmp/hbt-review-home TMPDIR=/tmp/hbt-review-tmp \
  PATH=/tmp/coder-0232-locked-venv/bin:/usr/local/bin:/usr/bin:/bin \
  HUB_TEST_PG_STRICT=1 LANG=C.UTF-8 TZ=UTC \
  sh scripts/run-build-tests.sh
```

运行前创建上面两个临时目录。每刀复制 helper 和测试至一次性目录，恢复原副本后才跑下一刀；任何 mutation anchor 不唯一或绿控制失败即退出非零。
