# G3 console 复验说明（跨席复验照此做）

适用：`view-service/console-*-regression.js`（真 Chrome 浏览器闸）与 `tools/sessionhelper` 的客户端用例。
派工 R-NEWPLAT-G3-tosonnet50ts-20261001084334。

## 1 浏览器回归

仓库里没有 `node_modules`，也不应提交。playwright 装在仓库**外**，浏览器用系统 Chrome，不下载：

    mkdir -p /tmp/pwprobe && cd /tmp/pwprobe
    PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1 npm install playwright      # 企业网内需要走 /home/dev/bin/corp-vpn npm install playwright
    cd <仓库>/view-service
    NODE_PATH=/tmp/pwprobe/node_modules PW_CHANNEL=chrome node console-agent-mgmt-enroll-regression.js

- `PW_CHANNEL=chrome` 让 playwright 用系统 Chrome（本机在 /opt/google/chrome/chrome、/usr/bin/google-chrome）。
- 退出码 0 全过；这些脚本末行打印 `passed=N failed=M`。
- 脚本自己从 `internal/m8/console_auth.go` 实时抠出 console 页，Hub 用本地桩，不需要起真 Hub。
- 变异对照：`console-agent-mgmt-enroll-regression.js` 与 vega 用环境变量 `MUTATION=<名>`；`console-browser-regression.js` 用参数 `--mutate=<名>`。
  判读按"首红是不是该条"，不按红的总数（后续用例共享桩状态会级联变红）。变异结果一律用 `view-service/mutation-classify.sh` 判（约定见板 runbooks/CONVENTION-MUTATION-EXIT-CODES-20261002.md）：env 形态六个脚本的退出码 **1＝变异被杀（红，有 FAIL 行）、0＝存活、3＝变异没打上（MUTATION-ANCHOR-STALE／MUTATION-UNKNOWN／MUTATION-SWITCH-WRONG-FORM），不是对照成立**、2＝脚本崩。

## 2 客户端用例

    cd tools/sessionhelper
    PYTHONDONTWRITEBYTECODE=1 /opt/dingwei/sessionhelper-venv/bin/python -m unittest discover -s tests -p 'test_*.py'

- 用 helper 自带 venv：里面有 `websockets` 和 `pexpect`。用系统 python3 会缺 `pexpect`（9 例报 ModuleNotFoundError，与改动无关），`test_enroll_wiring.py` 会因缺 `websockets` 整体跳过。
- 做变异对照务必带 `PYTHONDONTWRITEBYTECODE=1` 并清 `__pycache__`：同长度的源码改动在同一秒内改回，旧字节码可能被沿用，造成"恢复后仍红"的假象。

## 3 已知基线红（与 G3 改动无关）

`console-agent-tree-regression.js` 在 `fcd3a3dd` 上就失败（online 置灰断言），已登记为新平台已知红，归 G3 后续处理。
