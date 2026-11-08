# PG test cache outside the source tree

派单：S-PGTEST-CACHE-OUT-OF-TREE。基线 `opus50tc2k/r54-integration` / `6e2c836491f8ba506833d6f849fbe73530e13f92`。初始实现 `b7b5a69d8560f9ad1d70da57bfc4e621acf71ba0`；安全修订 `567bc09b5d57d78a7e9cfd0b17e91ac829057b2d`。

## 根因核实

判读成立。旧逻辑在 `os.UserHomeDir()` 失败时让 cache 留空，并跳过 `CachePath`；embedded-postgres v1.34.0 因而使用相对当前目录的 `.embedded-postgres-go`。`go test` 将包目录作为工作目录，所以缓存会落进源码树。代码配置链见 `tools/pgtest/main.go` 中 `resolveCacheRoot` 与 `CachePath` / `BinariesPath` 设置；无 HOME 端到端用例检查工作目录未生成 `.embedded-postgres-go`。

## 修改范围

只改 `tools/pgtest` 及本报告，无产品代码、build.sh、版本号改动。

- `resolveCacheRoot` 仍按 HOME、`os.UserCacheDir()` 顺序选择；最后退路按当前 uid 使用 `os.TempDir()/dingwei-pgtest-cache-<uid>`。
- 最后退路以 0700 创建。若路径已存在，不修复或替换；使用前用 `Lstat` 拒绝符号链接及非目录，核属主 uid 为当前 uid，并拒绝任何组／其他写位。异常即报错退出。
- 配置始终显式设置绝对 `CachePath(cache)` 和 `BinariesPath(cache/extracted)`。
- `cache_root_test.go` 覆盖退路路径含 uid、新目录 mode=0700 和当前 uid 属主、拒绝预建 0777 目录且不改变它、拒绝预建符号链接且不改变它，以及用户缓存优先级和显式相对路径绝对化。
- Linux 端到端测试在临时工作目录、HOME/XDG_CACHE_HOME 缺席时启动真实 pgtest/内嵌 PostgreSQL，并检查缓存、运行及清理结果。
- Windows 编译保留：若 Windows 到达需要 uid 隔离的最后退路，会安全失败；正常 HOME 或用户缓存路径不受影响。

## 验证记录

运行环境：Linux。构建门使用锁定的 `tools/sessionhelper/requirements-runtime.lock` venv `/tmp/flake-viewretry-venv.6366BZ`，通过 `env -i` 清除继承环境，`HOME` 与 `XDG_CACHE_HOME` 未设置，`TMPDIR=/tmp`。

### Cache 单测与变异

```text
cd tools/pgtest && TMPDIR=/tmp go test ./... -count=1
ok github.com/zhangzongchu2019/dingwei/tools/pgtest 10.456s
COMMAND_RC=0
```

三项独立变异分别移除 uid 后缀、移除组／其他写位拒绝、把 `Lstat` 改为跟随符号链接的 `Stat`。三项均按预期红（各自 `go test -run <对应测试>`，rc=1）：

```text
uid suffix omitted:
no-home cache root = ".../dingwei-pgtest-cache", want absolute ".../dingwei-pgtest-cache-1001"

write-bit check omitted:
accepted unsafe fallback ".../dingwei-pgtest-cache-1001"

Lstat replaced by Stat:
accepted symlink fallback ".../dingwei-pgtest-cache-1001"
```

变异代码均已恢复；修订代码完整工具测试 rc=0。

### 使用方无 HOME 端到端

在修订提交 `567bc09b` 上运行：

```text
env -u HOME -u XDG_CACHE_HOME GOCACHE="$gocache" GOPATH="$gopath" TMPDIR=/tmp \
  go test ./internal/store/pgtest -run '^TestPool_RoundTrip$' -count=1 -v
--- PASS: TestPool_RoundTrip (2.48s)
PASS
ok github.com/zhangzongchu2019/dingwei/internal/store/pgtest 2.484s
COMMAND_RC=0
```

测试结束后 `git status --porcelain` 为空。

### 完整构建测试门（修订提交 `567bc09b`）

```text
env -i PATH=/tmp/flake-viewretry-venv.6366BZ/bin:/usr/local/go/bin:/usr/bin:/bin \
  GOCACHE="$gocache" GOPATH="$gopath" GOMODCACHE="$gomodcache" \
  TMPDIR=/tmp TZ=UTC LANG=C.UTF-8 scripts/run-build-tests.sh
BUILD_TESTS_OK helper_profiles=2 python=27 shell=2
START=2026-11-08T11:59:12Z
END=2026-11-08T12:08:05Z
COMMAND_RC=0
```

原始完整构建门输出：`/tmp/pgtest-cache-run-build-tests-r2.txt`。使用方端到端原始输出：`/tmp/pgtest-cache-store-e2e-final.txt`。
