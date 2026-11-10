# 设计

## Context

父基线为 r56.2 `0de00bc45d2716cb7ba22b5642dca6f9e73f229f`。C 路持有 OAuth state 与签发意图；B 路持有外层 attempt 及浏览器会话。回调页面在 B，失败分类由 C 返回固定元信息供 B 映射。

## Decisions

1. 增加 `CallbackFailure`，仅携带原因码、attempt 前缀、目标类别和安全 sentinel；底层错误通过固定映射分类，不保留错误原文。
2. 目标未解析分类为 missing；已解析但不具备接入资格分类为 exists。组织校验拒绝独立映射为 tenant，飞书交换/用户资料请求失败映射为暂不可用，state 失效/重放映射为过期或已用。
3. B 的 `failCallback` 是单一 HTML/JSON 输出和结构化日志入口。浏览器 Accept HTML 时输出解释页面；其它客户端仍获 JSON OAuth error 并附 error_description。
4. 日志字段取服务端 callback context，attempt 只保留前八位；目标只保留类别；UTC 使用注入时钟，便于可重复测试。

## Security

页面内容只来自固定文案和固定 OAuth error；使用 `html/template`。日志不输出底层 error、不输出 request 或 query，不记录目标名和主体字段。HTML 设置 no-store、nosniff、no-referrer 及无外部资源 CSP。
