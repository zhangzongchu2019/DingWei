# r38 0.2.3.11 版本位交审

基线 integration/spec5ff-r38-wip 精确 dd3d84e33ce53314ade2d0eb1b817cb89a5863ad，tree 0a77267995bb0928ba32b427f89a3dc2ee3c4c52。执行源 a84c1a08e02a042b04a0f64e074c009ae8bb5ab5，tree 18f813c43b78449e0d2f610b2dd4381e74455784，直父即指定基线；分支 review/coder/r38-version-02311。

## 范围

与参照 01f87ac2b457d3e2386eba1c699ae351496c4ece 的改动路径完全一致，只有7个文件：

- VERSION、internal/version/version.go、tools/sessionhelper/sessionhelper/__init__.py：三处 0.2.3.10 → 0.2.3.11。
- internal/version/helper_0233_test.go：TestHelper02311ReleasePins 钉死 r38 三处同号，并将前一发布说明改为0.2.3.10；保留依赖锁SHA断言，文案明确本次版本位不能改变依赖。
- tools/sessionhelper/tests/test_legacy_mcp_entry.py：受测源包 manifest／回执／升级夹具钉值同步。
- tests/pilot-project-context/local_context_roundtrip.py：真实本地 HTTP → helper IPC → MCP stdio initialize 回执期望0.2.3.11。
- view-service/console-helper-update-regression.js：关闭／重开后迟到快照回归夹具与断言同步0.2.3.11。

不改任何能力下限、部署配置、默认行为或产品逻辑；单调版本检查的 lastReleased 原值0.2.3.2也按参照保留。requirements-runtime.lock 字节不变，SHA256仍为 5ef8d5815239c3e165ae6ede14559fb545cce4f59cc144f49e20d8162453d2a0；“发布锁”更新的是发布号钉值测试，不重新解析或升级运行依赖。raw/SOURCE-BINDING.json 给出参照路径集合核对及7文件SHA／Git blob；其他基线文件完全不变。

## 验证

| 门 | 实际结果 | 原始记录 |
|---|---|---|
| 版本包 go test ./internal/version -count=1 -v | 4格PASS，含发布钉值／根文件／四段单调／helper同号 | raw/version-packages.* |
| Console与MCP版本 go test ./internal/m8 -run 'Version 或 TestProjectContextAcrossRealHTTPHelperIPCMCP' -count=1 -v（精确正则见JSON） | 11格PASS；明确设置DW_DESKTOP_INTEGRATION_PYTHON，真实MCP initialize版本断言实际执行，没有skip | raw/console-mcp-version.* |
| helper-update页面 Chromium 回归 | 全HTTP拦截，14格PASS，覆盖修改的迟到快照版本夹具 | raw/browser-version-pin.* |
| 原封 canonical sh scripts/run-build-tests.sh | rc0，BUILD_TESTS_OK python=63 shell=2，135.698秒 | raw/canonical.* |
| helper全套 python3 -B -m unittest discover -s tools/sessionhelper/tests | 961 tests，OK，unittest 426.881秒，rc0 | raw/helper-full.* |

Go1.22.2；Python3.12.3来自既有锁定venv /tmp/coder-0232-locked-venv，websockets15.0.1／pexpect4.9.0／certifi2026.7.22，详见raw/python-runtime.json。canonical及helper全套使用各自的本轮隔离HOME与短ext4 TMPDIR；PG STRICT=1、SKIP_OK=0、EMBEDDED=1。白名单环境、命令、UTC起止、rc、执行源commit／SHA均在各门JSON。系统Python预检无websockets包元数据，正式门均使用已验证的锁定venv，没有改依赖或跳过测试。

helper stderr原样保留测试夹具后台线程OSError、connection-close traceback、ResourceWarning及负例诊断；最终unittest汇总为961 tests OK、退出0，不声称stderr为空，也不把夹具trace当成本席运行helper故障。未删除／屏蔽这些诊断。

本轮未重跑用户未要求的全仓Go或vet；那是基线组长已完成的门，不冒标成本件自测。本件指定的版本包、canonical、helper全套全部实际执行通过，另补受影响MCP与浏览器回归。未产生发行制品、安装、重启、部署、迁移或上传源码；当前运行helper未被本席升级。等组长审与prodops交叉。
