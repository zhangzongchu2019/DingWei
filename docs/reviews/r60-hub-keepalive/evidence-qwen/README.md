# qwen复核修订固定源证据

source_commit=eca016fb2f0ed272bc063f8170b5588dab37ce17；当前主入口。gate-summary只选本源clean完整门，gate-history保留原失败/WIP/旧锚历史，绝不拼成单次全绿。raw_file为确定性gzip，解压SHA在JSON；25刀均编译成功业务断言红，两个baseline绿。manifest逐文件对应Git对象及当前字节。独立评审另计，Q6联合格未跑不计PASS/SKIP。

| 固定门 | rc | 顶层 PASS | 含各层子项 PASS | SKIP | FAIL |
|---|---:|---:|---:|---:|---:|
| canonical-qwen | 0 | 6 | 21 | 0 | 0 |
| go-qwen | 0 | 1558 | 2580 | 49 | 0 |
| race-qwen-complete | 0 | 38 | 54 | 0 | 0 |
| build-qwen | 0 | 0 | 0 | 0 | 0 |
| strict-qwen | 0 | 0 | 0 | 0 | 0 |

canonical 两 profile：claude 1204/1204 rc0；codex 1204/1204 rc0；BUILD_TESTS_OK helper_profiles=2 python=27 shell=2。focused race 包含 m8/store/obs/cmd 与原较宽筛选，非全包 race。strict 明细见原日志。

SQLite interrupted(9) 首轮对应测试，本源完整四树 Go 内逐条读数：`--- PASS: TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent (0.44s)`。这不是其历史失败根因已定，也不用单跑抵消历史完整 rc1。

复现：仓库根 `python3 docs/reviews/r60-hub-keepalive/evidence-qwen/VERIFY.py` 及 `sha256sum -c docs/reviews/r60-hub-keepalive/SHA256SUMS`。变异驱动中的本机路径须换到独立工作树，不要在冻结交件树直接删保护。
