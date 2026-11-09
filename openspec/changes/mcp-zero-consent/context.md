# 现场与入口

执行者01a1be21-a016-7ff1-bf0f-a48b78f9d3d6；锁/tmp/coder56-mcp-zeroconsent.lock；工作树/data/ai-dev-data/mcp-zero-consent。指定基线main fce2ede8，未使用旧ZERO-CONFIG工作树写入。

新代码：feishulogin/defaults.go，extwire/defaults.go，mcpgw/selection.go；PG新增0108。复用原有加密OAuth Store和已钉拼音模块。临时selection状态存可信身份，原OAuth状态仍不存上游凭据。试验只用隔离PG和模拟飞书，不接线上席位。

2026-11-09 首轮返工：经组长明确改派及三项只读核验，写者现为01a1bf0d-d5c9-7613-9845-5a3707ec3c9b，新工作树/data/ai-dev-data/mcp-zero-consent-01a1bf0d；原树只读，旧锁留证/tmp/coder56-mcp-zeroconsent.lock.stale-20261109T074843Z。远端交件分支不变，追加合并d70016f3。新增namespace_mutation.py、browser_regression.py/browser_probe.js，后者登记tools/run-browser-regressions.py。当前提供的/data/ai-dev-data/work/pw/node_modules不存在，本机已有/tmp/coder-a3-playwright/node_modules及Chromium1194可用，NODE_PATH可显式配置。浏览器测试HOME/TMPDIR隔离，环境白名单剥离SH_与DINGWEI_。

浏览器新增实测修复：internal/feishulogin/form_policy.go把服务端OAuth授权origin加入连接页CSP，mcpgw.connect与renderSelection仅加入已校验attempt回调origin。真浏览器不改Origin/响应头、不拦截回调导航，临时HTTP客户端监听随机端口实际接收授权码；上游仅飞书替身。首次环境MODULE_NOT_FOUND、测试中CSP禁止fetch及无本地回调监听造成的失败原文保留；实际CSP跨源阻断另列，不混作环境成功。
