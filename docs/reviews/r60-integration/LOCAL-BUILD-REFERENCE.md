# R60本机门读数与SHA参考（不是发布件）

依据组长2026-11-17T03:21:07Z口径澄清。本机执行SOURCE_COMMIT build.sh（内含canonical/inventory）只作门读数与SHA参考；**不称发布件，不上传文件服务，不交prodops，不上传任何二进制/helper源码包**。正式发布件须在组长与togpt60复核集成锚通过后，由组长另令prodops在dw123的dwbuild以go1.22.2构建，再按同锚/工具链比对。本记录不构成构建发布、分发、部署或prodops作业指令。

## 本机参考坐标与数值

构建参考源码：`a311aaf3beea90262c05ca2c316064b8a8856d06`，tree `dfb32347b5156dc1d15d4bfaf0543310dde316d8`，版本0.2.3.40；root/Go/helper版本一致。workpulse原固定源构建命令/UTC/四二进制输出SHA见evidence/canonical-build.json/log，本次不重新跑Go/canonical/browser等原门。

| 对象 | 本地参考SHA256 |
|---|---|
| workpulse（43,155,608 bytes） | `ec24fb171035d018c51b0a636442969e9b2de482d20a296a586e639e288617de` |
| renderer-canary-sse.html（384,709 bytes） | `14ffc39ea1ed61c1d230b7e3f3a810564daf05f61a7ec17cefde07ffc98eb6ef` |
| helper同锚tar.gz（296,110 bytes） | `b7d02175fbf37a371a672a70359a4250117c463c118a8cf764b076314e167b31` |
| view三运行时文件MANIFEST.sha256本身 | `0fc5b356df2cdfa7ec9d8a926889f448d800d8ee320c02dfdab33ee3e11454cf` |

完整`go version -m`原输出在[local-build-reference/workpulse-go-version-m.log](local-build-reference/workpulse-go-version-m.log)，不是截取摘要；go version为go1.22.2 linux/amd64，build设置-trimpath=true、CGO_ENABLED=0、GOOS=linux、GOARCH=amd64、GOAMD64=v1。依赖版本/模块校验和逐项保留。renderer只计算Git对象/实际同字节HTML，不拼压缩时间戳。

helper由本仓package_release.py显式--commit a311从Git对象白名单导出、gzip mtime0/tar固定元数据；仅保留本机私有artifact目录。实际包verify核70个成员/manifest/SHA，禁止env/凭据/私有状态检查通过，4条跟踪与ignored私有文件/字面secret排除回归另跑均绿，原输出在package-exclusions.log。这里Git只提交SHA/元数据/manifest，不提交tar.gz或代码包。view使用view-service-release.sh的package/verify-package两个本地子命令，只导出Git三文件并核manifest，没有install、Docker或远端操作。

## SHA比对必须同完整锚

**相同产品子树 ≠ 带来源字段的产物SHA相同。** build.sh把SOURCE_COMMIT注入workpulse；helper包MANIFEST嵌入整仓commit/tree；view的PROVENANCE也嵌入commit/tree。纯文档tip与a311产品字节虽相同，换SOURCE_COMMIT仍会改变workpulse/helper包/PROVENANCE的字节。renderer及view三文件manifest则在字节相同条件下稳定。

本表只可用于dwbuild在**同一a311完整锚、相同packager/工具链及build.sh参数**上的比对，不能拿最终文档tip的正式产物直接比较。正式冻结锚由组长终审另定；若最终指定不是a311，应在该完整锚另取本机参考并记录来源后再比，不把差异误判产品回归，也不擅自改二进制commit或helper manifest去凑SHA。本记录不自行决定正式发布SOURCE_COMMIT。

## 边界

本机helper包不安装、不启动、不交他席；不执行现网helper，不用其HOME/env/私有state作输入。所有产物留本机，Git只含可复核元数据。原集成受测产品仍a311；此补记不将门读数升级为正式发布验收。正式件和上线时机继续等待组长/togpt60复核与业主/组长另令。

正式 dw123 构建还须满足 [INTEGRATION-GATES.md](INTEGRATION-GATES.md) 文末的隔离解释器与两格逐名 PASS／0 SKIP 前提，不能只核 build.sh 总结果。
