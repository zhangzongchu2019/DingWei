# 本地复跑

仓库 wego-ai/dingwei；review/coder/console-agent-table-layout。
冻结输入是 Git tree `31dfb4853e3df0d58f656544d371170ef8bc9354`，
标签 `evidence/coder/console-agent-layout-31dfb485`。独立取件时正常 fetch
该 tree 标签，不使用要求 commit 的 shallow 取法。候选提交的非 docs 输入与此树相同。

在干净检出中，以本地已安装的 Playwright 与 Chrome 离线运行：

```sh
NODE_PATH=/tmp/coder-a3-playwright/node_modules PW_CHANNEL=chrome \
LAYOUT_EVIDENCE_DIR=/tmp/layout-check \
node view-service/console-agent-table-layout-regression.js
```

Playwright 路径是本机预装依赖目录，复核机改为自己的已安装目录即可；不需要线上登录态。
`MUTATION` 取 `fixed-columns`、`actions-wrap`、`buttons-wrap`、`no-scroll`、
`no-ellipsis`、`helper-below`、`wide-checkbox`、`status-overflow`、`wide-select` 逐个运行。每项必须 rc1 且命中 SUMMARY.json
登记的对应断言；锚丢失或未知撤修不是成功。取消 MUTATION 再跑须 rc0。
浏览器脚本读实际生产 HTML 与 helper UI JS；所有 HTTP 均由本地假域拦截，
实际按钮处理函数在此装置中被点击。没有连接 Hub、席位或生产服务。

原有浏览器脚本逐项命令见 `browsers/BROWSERS.json`（node view-service/文件名）；
登录页亦列入矩阵。所有原有脚本未修改。

后端门使用既有通用隔离导出 runner：

```sh
python3 tests/pilot-feedback/gates.py \
  --commit 31dfb4853e3df0d58f656544d371170ef8bc9354 \
  --output /tmp/layout-go-gates --gates vet canonical go
```

runner 从精确 Git tree 导出到临时目录，隔离 HOME、清空环境、
Go 缓存离线使用，真实 embedded PG 二进制缓存，DSN 未设置。
本机 Go 1.22.2（/usr/bin/go），Python3.12.3；锁定 helper venv
`/data/ai-dev-data/helper-deps-lock-venv`，无联网安装。
候选 Git blob 与证据验证：`python3 docs/reviews/coder-console-agent-table-layout-20261020/VERIFY.py --commit HEAD`。
本件不授权部署。
