# 现场与入口

执行者01a1be21-a016-7ff1-bf0f-a48b78f9d3d6；锁/tmp/coder56-mcp-zeroconsent.lock；工作树/data/ai-dev-data/mcp-zero-consent。指定基线main fce2ede8，未使用旧ZERO-CONFIG工作树写入。

新代码：feishulogin/defaults.go，extwire/defaults.go，mcpgw/selection.go；PG新增0108。复用原有加密OAuth Store和已钉拼音模块。临时selection状态存可信身份，原OAuth状态仍不存上游凭据。试验只用隔离PG和模拟飞书，不接线上席位。

2026-11-09 首轮返工：经组长明确改派及三项只读核验，写者现为01a1bf0d-d5c9-7613-9845-5a3707ec3c9b，新工作树/data/ai-dev-data/mcp-zero-consent-01a1bf0d；原树只读，旧锁留证/tmp/coder56-mcp-zeroconsent.lock.stale-20261109T074843Z。远端交件分支不变，追加合并d70016f3。新增namespace_mutation.py、browser_regression.py/browser_probe.js，后者登记tools/run-browser-regressions.py。当前提供的/data/ai-dev-data/work/pw/node_modules不存在，本机已有/tmp/coder-a3-playwright/node_modules及Chromium1194可用，NODE_PATH可显式配置。浏览器测试HOME/TMPDIR隔离，环境白名单剥离SH_与DINGWEI_。

浏览器新增实测修复：internal/feishulogin/form_policy.go把服务端OAuth授权origin加入连接页CSP，mcpgw.connect与renderSelection仅加入已校验attempt回调origin。真浏览器不改Origin/响应头、不拦截回调导航，临时HTTP客户端监听随机端口实际接收授权码；上游仅飞书替身。首次环境MODULE_NOT_FOUND、测试中CSP禁止fetch及无本地回调监听造成的失败原文保留；实际CSP跨源阻断另列，不混作环境成功。

2026-11-09 常设接管规则三条均满足：远端 d311864c、旧树 clean／HEAD 同值、ps 与 /proc 无相关运行进程。当前写者01a1bf6a-20da-7533-8984-532fd2ec65a3；新树 /data/ai-dev-data/mcp-zero-consent-01a1bf6a。旧锁 stale-20261109T083303Z 保留。已核远端 main fce2ede8、rulings dcfd24ef、tasks 7afbfebc，未比首轮交件发生规格更新。连接页 CSP 以上述 r56.2 新 tip 为准，首轮 FormRedirectPolicy 连接页方案废止；该辅助函数仅供选择页使用。

2026-11-09T13:20Z按组长直接裁定接管：当前写者01a1bf9a-fffd-7472-ac1b-7392961de426，两旧锁stale-20261109T132042Z保留；沿用mcp-zero-consent-01a1bf6a工作树。本次第三值0de00bc4的连接页withLoopbackFormAction追加已校验回环源，authorizeOriginOf仍由r56.2提供；替代前次“连接页不含回环源”的过时结论。A立即302/B同意页参数化夹具与CSP三刀见final-01a1bf9a，按补38只收尾冻结。
