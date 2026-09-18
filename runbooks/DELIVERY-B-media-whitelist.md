# B 线：媒体白名单通配语义修复

更新时间（UTC）：`2026-09-18T22:59:50Z`

## 实施状态

- 分支：`coder56solzzc/b-media-whitelist-wildcard`
- 基线：`c1db31cb8c59c652c9956172e8af28944fb7e27c`
- 实现锚：`0e09c4b885654ba9df50395285b28ffcb143aa87`
- 状态：代码与仓内证据已交付，未合并、未部署。

## 改动事实

- `image/*`、`video/*` 是候选类别；只有存在受控扩展名映射的子类型才内联，未知子类型仍规范化为 `application/octet-stream`、`.bin`、attachment。
- `image/svg+xml` 在通配判断之前显式拒绝；`text/*` 仍只允许 `text/plain`。
- 新映射：`video/quicktime→mov`、`video/x-matroska→mkv`、`image/heic→heic`、`image/bmp→bmp`、`image/tiff→tiff`。
- nginx `/f/` 正则、显式 `types` 和删除键形状同步加入上述扩展名；`.mov` 配置为 `video/quicktime`。
- 新增 quicktime、SVG、未知类型三份夹具并通过真实 `Manager.Put` 验证落盘 media type 与后缀。

## 验证事实

- 最终 `go test ./... -count=1`：rc=0。
- 删除 SVG 显式排除后，`go test -v ./... -count=1` 执行 978 条，唯一转红 `TestNormalizedMediaWildcardCandidatesAndFixtureFallbacks`；变异已还原。
- 仓内 nginx 配置测试确认 `.mov` 的显式类型为 `video/quicktime`。

## 生产验收边界

V1 必须在部署后上传真实 quicktime 对象并通过生产 nginx 执行 `curl -I`，要求 `Content-Type: video/quicktime` 且无 attachment。仓内配置与替身不能冒充该证据，本交付不宣称 V1 已通过。
