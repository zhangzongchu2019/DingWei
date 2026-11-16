# 历史167源证据：不作为eca修订门

当前入口为 ../evidence-qwen/README.md。以下原记录不改写，不能延用于新产品锚。

# 固定源作者证据

source_commit=1671a59667c544ff3a3e0270ac2a293e15560d20；基main d2cc47c7。各 gate 均保存起止工作树状态及 UTC，raw_file 是原始日志 gzip（解压 SHA 在 JSON）。

`gate-summary.json`仅最终合格固定门；`gate-history.json`保留完整历史（含rc1与工作态source_commit=null），不能拼成虚构单次全绿。`mutations.json`只计合格最终刀，每刀编译rc0且业务断言红；初始脚本失败另存raw，不混PASS/SKIP。`product-manifest.json`按冻结Git对象哈希，含测试/源码及版本/依赖/renderer真值；当前交件文件须同字节。`VERIFY.py`从Git与工作树双核。无私有账本/凭据/env值。



## 固定源最终门读数

| 门 | rc | 顶层 PASS | 含子项 PASS | 含子项 SKIP | FAIL |
|---|---:|---:|---:|---:|---:|
| canonical-reviewed | 0 | 6 | 21 | 0 | 0 |
| go-reviewed-retry | 0 | 1553 | 2575 | 49 | 0 |
| race-reviewed | 0 | 33 | 49 | 0 | 0 |
| build-reviewed | 0 | 0 | 0 | 0 | 0 |
| strict-reviewed | 0 | 0 | 0 | 0 | 0 |

Canonical helper 两 profile：claude 1204/1204 rc0；codex 1204/1204 rc0；末尾 BUILD_TESTS_OK helper_profiles=2 python=27 shell=2。表格的 Go PASS 不与 Python/helper 子项混算。focused race 不是全包 race。strict CLI 明细见原日志。

复现：仓库根运行 `python3 docs/reviews/r60-hub-keepalive/evidence/VERIFY.py`、`sha256sum -c docs/reviews/r60-hub-keepalive/SHA256SUMS`；命令精确argv在gate-summary。变异脚本的本机workspace/artifact路径需按独立工作树替换，勿对冻结交审工作树直接删保护。
