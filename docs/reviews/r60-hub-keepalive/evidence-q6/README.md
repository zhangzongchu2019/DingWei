# qwen复核修订固定源证据

source_commit=3db78e5b244c4663fe67f7dcd44b74991057a7f6；当前主入口。gate-summary只选本源clean完整门，gate-history保留原失败/WIP/旧锚历史，绝不拼成单次全绿。raw_file为确定性gzip，解压SHA在JSON；28刀均编译成功业务断言红，两个baseline绿。manifest逐文件对应Git对象及当前字节。独立评审另计，Q6联合格未跑不计PASS/SKIP。

| 固定门 | rc | 顶层 PASS | 含各层子项 PASS | SKIP | FAIL |
|---|---:|---:|---:|---:|---:|
| canonical-final-review | 0 | 6 | 21 | 0 | 0 |
| go-final-review | 0 | 1559 | 2583 | 49 | 0 |
| race-final-review | 0 | 39 | 57 | 0 | 0 |
| build-final-review | 0 | 0 | 0 | 0 | 0 |
| strict-final-review | 0 | 0 | 0 | 0 | 0 |

canonical 两 profile：claude 1204/1204 rc0；codex 1204/1204 rc0；BUILD_TESTS_OK helper_profiles=2 python=27 shell=2。focused race 包含 m8/store/obs/cmd 与原较宽筛选，非全包 race。strict 明细见原日志。

SQLite interrupted(9) 首轮对应测试，本源完整四树 Go 内逐条读数：`--- PASS: TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent (0.24s)`。这不是其历史失败根因已定，也不用单跑抵消历史完整 rc1。

复现：仓库根 `python3 docs/reviews/r60-hub-keepalive/evidence-q6/VERIFY.py` 及 `sha256sum -c docs/reviews/r60-hub-keepalive/SHA256SUMS`。变异驱动中的本机路径须换到独立工作树，不要在冻结交件树直接删保护。
