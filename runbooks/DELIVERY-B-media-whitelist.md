# B 线：媒体白名单通配语义修复

更新时间（UTC）：`2026-09-19T00:36:37Z`

## 实施状态

- 分支：`coder56solzzc/b-media-whitelist-wildcard`
- 基线：`c1db31cb8c59c652c9956172e8af28944fb7e27c`
- 实现锚：`6a4ad018d0d2b527e3ae5e500434ecf4fe982793`
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

评审返工：已删除 `exts` 中 SVG/HTML/XML/XHTML 的危险死映射，并从 `inlineCandidate` 删除 XML/XHTML 分支。`TestDirectLinkExtensionAllowListsExcludeActiveContent` 现通过 Go AST 同时读取 `manager.go` 的 `exts` 与 `downloadExts`、nginx `file_ext`、`directObjectKey` 四个来源。向 `exts` 重新插入 `text/html→html` 后，全仓 980 条仅该配置形状测试转红；还原后 rc=0。当前实现同时保留未知 `text/*→.txt`，覆盖 Python/YAML 等未枚举文本。

判据升级：同一测试现要求 manager 两张映射表（加通用 `.bin` 回退）、nginx 两个 `file_ext` 正则并集、`directObjectKey` 三处扩展名集合严格相等，并继续拒绝主动内容扩展名。MB-1 仅在 manager 加 `flv`、MB-2 仅在 nginx 加 `flv`、MB-3 仅在 `directObjectKey` 加 `flv`，三发均执行全仓 980 条，且都仅该测试转红；全部还原后全仓 rc=0。

## B7：危险项优先、受控文件名后缀保真、MIME 兜底

- 文件名后缀先经过危险表：php/html/htm/xhtml/xml/svg/svgz 无论声称何种 MIME 都强制 octet-stream/.bin。
- 其余对象先按 MIME 走既有内联/附件映射；无法识别时，仅允许文件名后缀命中受控文本表，命中后存 `text/plain` 与真扩展名，表外或无扩展名仍 `.bin`。
- 使用系统实测 MIME 的 yml/yaml (`application/yaml`)、toml (`application/toml`)、sql (`application/sql`)、sh (`application/x-shellscript`) 夹具，并覆盖空 MIME、evil.php 与伪造为 text/plain 的 html/xml/svg。
- 删除文件名兜底行为后，全仓 980 条仅 `TestNormalizedMediaWildcardCandidatesAndFixtureFallbacks` 转红；向受控表加入 html 后，全仓 980 条仅配置形状闸转红。全部还原后 rc=0。

实验二改裁后，最终顺序为：危险 MIME/危险后缀 → Office/ZIP 附件 MIME → 受控文本文件名后缀 → 内联 MIME → `.bin`。因此 yml/yaml 保留各自原扩展名，ini 可在空 MIME 下恢复，而无扩展名仍 `.bin`。受控表固定为 txt/md/csv/json/log/py/js/go/java/yaml/yml/toml/ini/sql/sh；html/htm/xhtml/xml/svg/svgz/php 不在表内。删除文件名优先分支后，全仓 980 条仅媒体夹具测试转红。

Playwright 实测后的 MIME 后备键已校正为 `application/yaml→yaml`、`application/toml→toml`、`application/sql→sql`、`application/x-shellscript→sh`；保留实测正确的 text/x-python、text/x-go、text/x-log、text/javascript、text/markdown、text/csv。文件名表优先，因此 `.yml` 与 `.yaml` 仍分别保真；无文件名扩展名时后备 MIME 映射有独立断言。最终全仓 rc=0。

第五道 nginx types 闸解析内联 location 的 `types` 块，逐项要求受控文本表所有扩展名映射为 `text/plain`。单独把 js 改挂 `application/javascript`、把 md 改挂 `text/markdown`，两发各执行全仓 980 条，复跑后的精确转红集合均仅配置形状闸；全部还原后 rc=0。

## B8：音频内联候选机制

- `audio/*` 加入 image/video 同级的内联候选机制；实验尚未确认的 mp3/m4a/flac/aac MIME 映射未提前加入。
- 按已裁定规范加入 `audio/ogg→oga`；WAV 别名 `audio/wav`、`audio/x-wav`、`audio/vnd.wave` 均映射 `.wav`，nginx 统一声明 `audio/wav`。
- nginx 内联正则、types、`directObjectKey` 与 manager 映射同步加入 oga/wav，严格集合相等闸保持全绿；测试明确断言 video/ogg→ogv 与 audio/ogg→oga 不相等。
- 删除 `audio/*` 候选条件后，全仓 980 条仅 `TestNormalizedMediaWildcardCandidatesAndFixtureFallbacks` 转红；还原后 rc=0。

最终音频文件名受控表为 mp3/m4a/flac/aac/opus/ogg/wav：前五类分别返回 audio/mpeg、audio/mp4、audio/flac、audio/aac、audio/opus 与同名扩展名；ogg 返回 audio/ogg 但落 `.oga`，wav 返回 audio/wav。已识别 MIME 优先于文件名音频兜底，因此 video/ogg→ogv 与 audio/ogg→oga 保持单值且不冲突。

nginx types、内联正则、`directObjectKey` 与 manager 映射同步完成，严格集合闸保持通过。删除 `filenameAudioExts` 的返回行为后，全仓 980 条仅 `TestNormalizedMediaWildcardCandidatesAndFixtureFallbacks` 转红；危险 php/html/svg 同轮夹具仍强制 `.bin`。还原后全仓 rc=0。V21–V24 的生产响应头与播放行为仍须部署后实测，本件不宣称已在现网生效。

## P0：UTF-8 上传文件名协议

- `X-File-Name` 以 `UTF-8''` 开头时先用 `url.PathUnescape` 解码 UTF-8，再执行 `safeName`；无前缀时保持旧客户端裸值语义。
- `safeName` 统一剥离 `/` 与 `\\` 路径前缀并删除 CR/LF、NUL 等控制字符；解码失败降为 `download`。
- 覆盖中文、emoji、空格括号、旧 ASCII、原名含 `%` 的新旧路径，以及 `%2e%2e%2f`、`%0d%0a`、`%00` 与对应裸攻击值。
- 将实现变异为“先 safeName、后解码且不再清洗”后，`go test -v ./... -count=1` 执行 980 条，唯一转红 `TestDecodedUploadFileNameSupportsUTF8LegacyAndSanitizesAfterDecode`；还原后全仓 rc=0。
