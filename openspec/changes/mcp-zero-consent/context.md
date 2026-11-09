# 现场与入口

执行者01a1be21-a016-7ff1-bf0f-a48b78f9d3d6；锁/tmp/coder56-mcp-zeroconsent.lock；工作树/data/ai-dev-data/mcp-zero-consent。指定基线main fce2ede8，未使用旧ZERO-CONFIG工作树写入。

新代码：feishulogin/defaults.go，extwire/defaults.go，mcpgw/selection.go；PG新增0108。复用原有加密OAuth Store和已钉拼音模块。临时selection状态存可信身份，原OAuth状态仍不存上游凭据。试验只用隔离PG和模拟飞书，不接线上席位。
