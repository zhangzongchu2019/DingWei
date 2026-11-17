# 容量退回复交：固定源作者证据

冻结产品/测试源 12f851f239e43a3049cb3c366b335e368295f584，基main d2cc47c7。仅此目录gate-summary为本轮门；旧evidence只属于ba历史，不冒充当前覆盖。


## 当前固定源最终门读数

| 门 | rc | 顶层 PASS | 含子项 PASS | 含子项 SKIP | FAIL |
|---|---:|---:|---:|---:|---:|
| canonical-retention-final | 0 | 6 | 21 | 0 | 0 |
| go-retention-final | 0 | 1558 | 2583 | 49 | 0 |
| race-retention-pinned | 0 | 20 | 31 | 0 | 0 |
| build-retention-pinned | 0 | 0 | 0 | 0 | 0 |
| strict-retention-pinned | 0 | 0 | 0 | 0 | 0 |

Canonical 两 profile：claude 1230/1230 rc0；codex 1230/1230 rc0；BUILD_TESTS_OK helper_profiles=2 python=27 shell=2。focused race仅覆盖所列筛选，不是全包race；Go包PASS不与Python/helper子项混计。strict明细见原日志。

`gate-history.json`保存现存历史命令、UTC、起止工作树与rc；source_commit=null的工作态不能计固定源。raw gzip解压后的SHA见对应JSON。历史第一轮36刀的global_window存活原记录与原日志位于historical/mutations-retention；首轮脚本索引错误另列，不抹除。最终mutations.json为36项，每项编译成功且业务断言红，baseline绿色，不与历史25刀合并成61。

当前完整四树Go包含cmd/workpulse，第一轮b273两条Unix socket超长路径失败保留；最终用短TMPDIR完整重跑。普通uid1001、固定空环境白名单、PGstrict真实临时库。无私有账本/正文/凭据。

复现：仓库根`python3 docs/reviews/r60-send-lost/evidence-retention/VERIFY.py`；`sha256sum -c docs/reviews/r60-send-lost/SHA256SUMS`。argv和起止UTC见gate-summary，驱动的本机路径应替换为独立工作树，勿直接破坏冻结树。受限分片网页/UI不在本轮改动，未冒称浏览器全量。
