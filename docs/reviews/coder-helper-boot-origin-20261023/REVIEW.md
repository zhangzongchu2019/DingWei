# S-HELPER-BOOT-ORIGIN 可审候选（仅开发，不部署）

分支 review/coder/helper-boot-origin；基线 `c8c023c289b254f9f8a3ad8ca4ac1357e21071d0`；冻结源 `a7d797b69b88b78ab269edd7465f632cdc0f61db`，tree `17daf8fc8ed8f4a2d335fb8932d2f8f6aee6639e`；不可变输入 tag `evidence/coder/helper-boot-origin-17daf8fc`。设计见 `docs/designs/S-HELPER-BOOT-ORIGIN.md`。冻结源之后仅 docs 证据与设计说明。

## 实现与限域

`app.main` 导入完成、load_config 取得最终状态目录后、构造 SessionHelper 前，由实际 helper PID 写 `helper-boot-origin.json`。字段 pid、/proc/self/stat 第22字段 starttime、实际已导入包的 strict realpath、isolated、随机一次性 boot_marker，加固定 type。每 PID 仅尝试一次；默认开，无新环境开关。0600临时文件 fsync＋同目录原子替换＋目录fsync；stderr 同一 JSON 行。采集或写入失败固定去敏告警、不阻启动，不输出异常文本；stderr失败也不阻启动。

未改 Hub、Go、锁、VERSION，不碰任何在线席位。只适用于本地诊断，不是认证：影子包可能伪造，r37 停后起前无影子与无外部写者门保留。目录叶不安全或proc不可读时会告警；写失败旧文件可能还在，不能只凭存在／mtime签收。正常需同时核本轮 marker、PID、starttime和来源。

PID 是 helper 自己命名空间的 PID。docker top 的宿主 PID 须经宿主 `/proc/HOST_PID/status` NSpid 映射并核 stat 第22字段；无法映射时不可证，不能直接等同不同命名空间数字。starttime 单位为自开机时钟 tick，非UTC。

## 作者冻结源读数

- helper 全套：882 项 OK，rc0；从空白环境字典启动，独立临时 HOME，锁定 `/tmp/coder-0232-locked-venv/bin/python`，未使用真实HOME。环境、起止时刻与命令见 final/ENVIRONMENT.json、RESULT.json。
- canonical：BUILD_TESTS_OK python=61 shell=2，rc0；脚本与 deploy 计数合同同步。
- 定向15项覆盖同PID／starttime、真实子进程 isolated=0／1、realpath、独立marker、0600／umask、原子失败保旧清临时、软链／不安全目录拒绝、三种失败不阻启动、实际入口顺序、版本查询不写。
- 撤修12刀全部指定断言红并恢复绿，包含PID、starttime、来源、isolated、marker、stderr、仅一次、可见告警、原子保旧、目录模式／软链、实际app接线。基线与末轮全绿，详见 final/withdrawals/RESULT.json。开发期一次撤修断言抛异常而非指定红已保留 development/，不计通过。
- 全仓Go未重复跑：本件无Go／go.mod／go.sum改动；冻结门对此作字节差集断言。未进行部署／生产验收，全部以上读数为作者本机读数。

## r38 集成提醒与复核

本分支按业主指定叠在 CWD 冻结源 c8c023c2；CWD 后继 d67e294d 的执行单逐字核入口说明仍须合并保留。PCE／RETAIN／CWD／本件 canonical 应取清单并集，预计63项Python，实际以逐项去重清单与合同核定，不覆盖RETAIN稳定入口保护。private仍禁启。

运行 `python3 docs/reviews/coder-helper-boot-origin-20261023/VERIFY.py` 核输入tag／tree、1895项源码blob、证据SHA、三门rc及12刀指定红与恢复绿。组长审＋prodops交叉核待独立完成。
