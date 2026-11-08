# PG test cache outside the source tree

派单：S-PGTEST-CACHE-OUT-OF-TREE。基线 `opus50tc2k/r54-integration` / `6e2c836491f8ba506833d6f849fbe73530e13f92`。实现提交：`b7b5a69d8560f9ad1d70da57bfc4e621acf71ba0`。

## 根因核实

判读成立。旧逻辑在 `os.UserHomeDir()` 失败时让 cache 留空，并跳过 `CachePath`；embedded-postgres v1.34.0 因而使用相对当前目录的 `.embedded-postgres-go`。`go test` 将包目录作为工作目录，所以缓存会落进源码树。代码注释和配置链见 `tools/pgtest/main.go:196-212`；无 HOME 的端到端用例在 `tools/pgtest/cache_root_e2e_linux_test.go:15-52` 直接检查这一目录。

## 修改范围

只改测试工具与其测试：

- `tools/pgtest/main.go`：新增 `resolveCacheRoot`。缺省优先取 HOME 下缓存，其次 `os.UserCacheDir()`，最后固定使用 `os.TempDir()/dingwei-pgtest-cache`；所有候选均转成绝对路径。配置始终显式设置 `CachePath(cache)` 和 `BinariesPath(cache/extracted)`。
- `tools/pgtest/cache_root_test.go`：覆盖 HOME/XDG 缺席的临时目录回退、用户缓存目录优先级和显式相对路径绝对化。
- `tools/pgtest/cache_root_e2e_linux_test.go`：在临时工作目录通过 `env -u HOME -u XDG_CACHE_HOME` 启动 pgtest/内嵌 PostgreSQL，断言协议输出中的绝对缓存路径、服务正常启停、二进制缓存存在且工作目录未生成 `.embedded-postgres-go`。

无产品代码、build.sh、版本号改动。

## 验证记录

运行环境：Linux；Go module 缓存使用现有模块缓存；运行构建门时使用锁定的 `tools/sessionhelper/requirements-runtime.lock` venv `/tmp/flake-viewretry-venv.6366BZ`，通过 `env -i` 清除继承环境，`HOME` 与 `XDG_CACHE_HOME` 未设置，`TMPDIR=/tmp`。

1. 单元测试：

```text
cd tools/pgtest && go test -run 'TestResolveCacheRoot' -count=1
PASS
COMMAND_RC=0
```

2. 无 HOME 端到端与变异：

```text
cd tools/pgtest && TMPDIR=/tmp go test -run 'Test(ResolveCacheRoot|NoHomeCacheStaysOutOfWorkingTreeEndToEnd)' -count=1
ok tools/pgtest 2.834s
COMMAND_RC=0
```

临时恢复旧逻辑（不设 fallback、仅在 cache 非空时调用 `CachePath`）后单独运行 `TestNoHomeCacheStaysOutOfWorkingTreeEndToEnd`，预期变异红，实得 `COMMAND_RC=1`，失败断言包括：

```text
CACHE="" want absolute "/tmp/dingwei-pgtest-cache"
BINARIES=".../binaries" want "/tmp/dingwei-pgtest-cache/extracted"
relative fallback cache leaked into working directory: lstat err=<nil>
```

变异后已恢复正式实现。

3. pgtest 包完整测试：

```text
cd tools/pgtest && TMPDIR=/tmp go test ./... -count=1
ok tools/pgtest 20.200s
COMMAND_RC=0
```

4. 使用 pgtest 的包无 HOME 端到端：

```text
env -u HOME -u XDG_CACHE_HOME GOCACHE="$gocache" GOPATH="$gopath" TMPDIR=/tmp \
  go test ./internal/store/pgtest -run '^TestPool_RoundTrip$' -count=1 -v
--- PASS: TestPool_RoundTrip (2.65s)
PASS
ok github.com/zhangzongchu2019/dingwei/internal/store/pgtest 2.656s
COMMAND_RC=0
```

紧接着 `git status --porcelain` 输出为空。

5. 总构建测试门：

```text
env -i PATH=<locked-venv,Go,system> GOCACHE="$gocache" GOPATH="$gopath" \
  GOMODCACHE="$gomodcache" TMPDIR=/tmp TZ=UTC LANG=C.UTF-8 scripts/run-build-tests.sh
BUILD_TESTS_OK helper_profiles=2 python=27 shell=2
COMMAND_RC=0
```

原始完整输出在执行环境临时文件 `/tmp/pgtest-cache-run-build-tests.txt`；store 包输出在 `/tmp/pgtest-cache-store-e2e.txt`。完整 `tools/pgtest` 输出为本次终端读数（20.200s）；所有源码改动已提交，之后的 store 端到端确认工作树仍干净。
