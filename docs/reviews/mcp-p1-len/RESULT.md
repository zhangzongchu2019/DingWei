# S-MCP-P1-LEN 开发交件

基线 main 04cfb018b3578e68f25244aef52582509a41db5a；分支 coder56solzzc/mcp-p1-len；产品源码 afe7ba13bd52ee7642599769074aed5659fc0acc，tree 39a10097ef9eb32d086ec13fa204534bed15d775。交件证据 tip 由回执给出。执行线程01a1c097，三项接管实测已通过；S1暂停、M3冻结，无部署/重启/在线身份操作。本件为开发自测，独立验收归组长。

## 结果与计算

- extshared.MessageBodyMaxBytes=1048576，Go len(string)与Python UTF-8编码字节计数；外部到目标在PG受理前计入服务器身份抬头，投影再次检查；显式及自动回程按原正文计数。
- JSON/IPC行预算=6×1048576+65536=6356992字节（不计终止换行）。每原字节最坏Unicode转义6字节；元数据另预算64KiB。业务正文独立校验，不能靠转义额度塞入大正文。非消息IPC保留64KiB，spec正文仍2MiB。
- 超限拒绝，不截断；body_too_large提供actual_bytes与limit_bytes。PG发送键/outbox/inbox不新增，不TouchGroup；Hub投递失败meta保留同名详情。
- Hub帧预算16MiB未变。新增回环测试发现helper连接未显式设置max_size，库缺省1MiB会以1009拒绝合法1MiB正文加JSON；现显式对齐16MiB，原失败和修复后读数都保留。

## 验证

| 门 | 读数 |
|---|---|
| mcpgw全包 | 235 pass / 0 fail / 2条件跳过 |
| extseat全包（隔离PG，HUB_TEST_PG_STRICT=1） | 46 pass / 0 fail |
| extshared全包 | 6 pass / 0 fail |
| m8 -p 1全包 | 1517 pass / 0 fail / 37条件跳过 |
| scripts/run-build-tests.sh | rc0；claude/codex各1132/1132，92文件，27 Python文件/2 shell |
| OpenSpec 1.16.0 validate --all --strict --no-interactive | 31 pass / 0 fail |
| 变异 | 指定三刀＋新增WS预算撤除，共4/4命中目标标记 |

Go包读数来自相应分侧最终Go源码；05b5679ec之后至afe7ba13b的internal/零差异，后续只改helper/构建清单。canonical最终在afe7ba13b源码上执行。普通用户go1.22.2；锁定Python venv，独立HOME，真实测试helper/Hub剥离继承SH_/DINGWEI_配置，临时数据库和监听地址。

边界：L-1、L、L+1 × ASCII/中文/emoji × 外部到目标、目标经真实独立send.py --file回外部、目标AMR自动回程，共27格。Go夹具使用真实PG、Hub WS投影、RouteEnvelope、AMR；send.py经过本地测试认证IPC桥接，生产listener另外以普通/body-only/file三通道实测（含最坏JSON控制字符），不冒称该桥是线上helper或真实模型推理。真实websockets回环通过生产_run_once连接选项验证1MiB正文加JSON接收。

标记：MCP_LEN_EXACT_UTF8、MCP_LEN_OVERSIZE_ZERO_DELIVERY。三指定刀：按字符数计长、超限截断后入队、helper退回65536；额外刀：移除helper显式WS接收预算。全部非编译失败，命中目标断言。复跑：在独立干净detached源码树中，PYTHON指向锁定venv，运行 `python3 docs/reviews/mcp-p1-len/mutations.py --root <tree> --out <output>`。脚本逐刀恢复原字节。

跳过项逐项列于SUMMARY.json：网关两项浏览器环境门；m8 37项外部环境/真实CLI/桌面/浏览器/待定模型等既有门，未当作通过。首次REST壳上限、WS测试接收端32KiB、测试地址/ACK、独立CLI夹具变量声明、旧helper边界期望与清单91→92失败均保留原件；修正后相关定向/整包/canonical通过，未删除原失败。

## Helper包与兼容

helper独立提交18a13e78f，后续限定控制预算0a68cbdba、真实WS接收afe7ba13b；源包从精确afe7ba13b的Git对象allowlist生成，62成员逐项SHA与凭据/私有态排除检查通过，两profile均执行source-kit负例测试。候选源包259451字节，SHA256 64800ab73b0a9dd792064b3401935660ec4d8532433dc8e801e3b102a0e3c902。只作本地候选，未上传/安装/发布；VERSION仍为基线0.2.3.34，正式升级版本及重钉由集成发布流程分配，不能把候选当成线上升级包。接收方从Git坐标独立取源，可用package_release.py重建。

旧helper连新Hub仍受旧本地IPC/WS预算限制，不能承诺完整1MiB；新helper连旧Hub仍可能受旧MCP 256KiB/HTTP JSON壳限制而拒绝，不截断或自动重发。完整能力需配套升级。ACK、租约、scope、PG/SQLite迁移和线上配置均不改。

原始日志压缩文件、未压缩SHA、全部计数与包元数据见SUMMARY.json及raw/。仅源码和报告经私有Git交件，无对象存储源码交换。
