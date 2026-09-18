# B 线：媒体白名单通配语义修复

更新时间（UTC）：`2026-09-18T23:39:54Z`

## 实施状态

- 分支：`coder56solzzc/b-media-whitelist-wildcard`
- 基线：`c1db31cb8c59c652c9956172e8af28944fb7e27c`
- 实现锚：`0611fefcdad9bf60ac2b0631e73522e0e93c84e9`
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

## B5：已知附件类型保留可用后缀

- 受控 media type 映射新增 docx/xlsx/pptx/doc/xls/ppt/zip；扩展名不读取 `OriginalName`，未知类型仍 `.bin`。
- 上述文件仍统一存储 `application/octet-stream`，由 nginx 原 attachment location 强制下载；该 location 扩为一个 `bin|docx|xlsx|pptx|doc|xls|ppt|zip` 正则块，旧 `.bin` 保持兼容。
- `directObjectKey` 同步扩展，避免新增附件对象无法被 sweeper 删除。
- docx、xlsx、pptx、zip 四份夹具与既有未知类型夹具均通过真实 `Manager.Put` 验证后缀和 octet-stream。
- 删除 Office 映射后执行 `go test -v ./... -count=1`：978 条中唯一转红 `TestNormalizedMediaWildcardCandidatesAndFixtureFallbacks`；还原后全仓 rc=0。

V4′ 仍须部署后上传真实 docx，经生产 nginx `curl -I` 验证 octet-stream、attachment 且 URL 末段为 `.docx`；仓内不宣称该项已通过。

## B6：文本通配、危险类型黑名单与 UTF-8 声明

- `text/*` 全部是内联候选；常见子类型映射为 txt/md/csv/log/py/js/go/java/yaml/yml/toml/ini/sh/sql，未映射的 `text/*` 安全回退为 `.txt`。`application/json` 作为显式安全文本映射为 `.json`。
- nginx 对上述全部文本后缀固定声明为 `text/plain`，不向浏览器发送 markdown/csv/javascript 等原始类型。
- nginx 内联 location 增加 `charset utf-8` 与 `charset_types text/plain`；文件字节不做转码。
- `text/html`、`text/xml`、`application/xhtml+xml`、`application/xml`、`image/svg+xml` 在媒体规范化入口显式降为 octet-stream/.bin；直链和删除键正则均不接受这些危险扩展名。
- 新增中文 Markdown 与危险 HTML 夹具；同组测试同时证明 md 内联候选和 HTML/SVG/XML 系列拒绝。
- 新增 Markdown、Python、JSON、未知 text 子类型、危险 HTML/XML 夹具；同组测试证明真扩展名、未知文本 `.txt` 回退及黑名单拒绝。
- 单独从黑名单删除 `text/html` 后，`go test -v ./... -count=1` 执行 978 条，唯一转红 `TestNormalizedMediaWildcardCandidatesAndFixtureFallbacks`；还原后全仓 rc=0。

V7′–V11′ 仍须部署后通过生产 nginx 做 md/py/json 与 html/xml/svg 阳性、阴性对照，验证未知 text 子类型 `.txt` 回退，并比较中文 md 取回字节；仓内不宣称生产响应头或浏览器显示已通过。

## P0：UTF-8 上传文件名协议

- `X-File-Name` 以 `UTF-8''` 开头时先用 `url.PathUnescape` 解码 UTF-8，再执行 `safeName`；无前缀时保持旧客户端裸值语义。
- `safeName` 统一剥离 `/` 与 `\\` 路径前缀并删除 CR/LF、NUL 等控制字符；解码失败降为 `download`。
- 覆盖中文、emoji、空格括号、旧 ASCII、原名含 `%` 的新旧路径，以及 `%2e%2e%2f`、`%0d%0a`、`%00` 与对应裸攻击值。
- 将实现变异为“先 safeName、后解码且不再清洗”后，`go test -v ./... -count=1` 执行 980 条，唯一转红 `TestDecodedUploadFileNameSupportsUTF8LegacyAndSanitizesAfterDecode`；还原后全仓 rc=0。
