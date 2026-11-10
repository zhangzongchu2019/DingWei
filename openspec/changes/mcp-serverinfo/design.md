# 设计

抽取 `serverInfoName`、`serverInfoVersion` 两个具名常量和 `serverInfoMeta()` 构造函数；discover、tools/list 与成功 tools/call 都调用同一函数。把 tools/call 成功结果构造抽为 `toolCallResult(data, isError)`，使 `isError` 两种值都带相同 metadata；当前 HTTP 路由仍只在业务成功时返回 `isError=false`，既有业务错误继续使用现有错误响应。

错误响应在结果构造之前返回，不附加 `_meta`。2025-06-18 codec 不调用新 helper，保持既有 wire shape。
