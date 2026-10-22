# S-HELPER-BOOT-ORIGIN（r38 候选，设计与实现合一交审）

基线为 CWD 冻结源 c8c023c289b254f9f8a3ad8ca4ac1357e21071d0。默认启用，仅本地写入；不改 Hub、VERSION、依赖锁、容器入口或在线席位。

## 入口与记录

实际 `app.main` 导入完成、取得有效 Config 后，在构造 SessionHelper 前调用 `record_boot_origin(cfg.state_dir)`。guard、MCP、版本查询与配对探测不进入此处。每个 PID 仅尝试一次（失败也不后台重试）；不同启动产生独立 128 位随机 boot_marker，防止旧文件被误当成本轮回执。

文件为状态目录中的 `helper-boot-origin.json`，固定字段：type=helper_boot_origin、pid、starttime、sessionhelper_file、isolated、boot_marker。pid 是记录者自身 os.getpid；starttime 从 `/proc/self/stat` 第22字段读取，单位为系统时钟 tick（自开机，不是 UTC 秒）；路径取实际已导入 `sessionhelper.__file__` 的 strict realpath；isolated 为当前解释器 sys.flags.isolated 的整数值。仅包含这些字段，不读取环境变量，不写凭据、参数或异常原文。

同目录随机临时文件 O_EXCL／O_NOFOLLOW，0600，写完 fsync 后原子替换目标，再 fsync 目录；已存在目标软链只替换目录项，不跟随。目录叶禁止软链、须属当前有效 UID 且无组／他写；不存在时按0700创建，不 chmod 已存在目录。前置写失败保留旧文件并清理自己创建的临时文件。记录收集失败只输出固定去敏告警；写文件失败输出固定告警，仍在 stderr 输出本轮 JSON。正常时文件和 stderr 的 JSON 行字节相同。stderr 自身失败也不阻启动。

## 运维核对与边界

文件是本地诊断记录，不是不可伪造的远程证明。旧文件必须同时核 PID、starttime 和本轮 marker，不能只看文件存在或 mtime。`docker top` 通常显示宿主 PID，记录中的 PID 属 helper 自己的 PID 命名空间，不能直接按数字等同：由运维在宿主读取该进程 `/proc/HOST_PID/status` 的 NSpid（末项对应容器内 PID），并核 `/proc/HOST_PID/stat` 第22字段，再核容器内记录。若命名空间映射、proc 可见性或时间命名空间导致无法对应，则结论为不可证，不猜测。读取 starttime 时以最后一个右括号切分，避免进程 comm 中空格／括号错位。

## 验收

真实子进程验证 isolated=0／1、PID、启动 tick、来源 realpath、独立 marker；0600与宽松umask、原子替换失败保旧／清临时件、目标软链不跟随、状态目录拒不安全叶、记录失败／收集失败／stderr失败均不阻启动、实际 app 入口接线与版本查询不写。每项关键字段、写入、告警、接线与隔离约束有定向撤修，要求指定断言红并恢复绿。helper 全套仅在隔离 HOME 与锁定 venv 内跑；canonical 接线新增模块并同步61＋2合同，r38 合并取各支清单并集。
