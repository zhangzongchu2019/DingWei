# caller保留期容量修订：固定源作者证据

产品/测试源 b48a0818aa121a3f68b921fac489926257ad841d；tree 567f180571e352684e06eb3966c150f8e2b48a19；parent 1d8b03a6d386f359a23398da34649007d53c1aa2。代码修订08bc，之后两个提交只加强现有测试；所有最终门及55项变异均重新跑在b48，不据产品逻辑相同复用08bc/1d的门。

`gate-summary.json`只列最终固定源、起止clean、rc0的门；`gate-history.json`保留工作态/08bc首轮环境白名单rc1/1d中间门，source_commit=null保持原义。环境均是显式白名单、普通uid1001、独立HOME/短TMPDIR、GOPROXY=off、PG strict临时库，未传入继承SH_/DINGWEI_/DW_/DATAD_。四树Go在canonical结束后串行-p1，focused race仅Outbound/Retention筛选，不声称整包race。

`mutations.json`最终55项=旧36项在本源重跑+新增19项；各项编译rc0、业务断言非空且rc非0，正文/编译日志各有SHA256。最终两组baseline绿。`mutation-history.json`另留中间不合格记录，不能与最终55相加：08bc的窗内quota刀只是未捕获业务ERROR、过期刀存活、MCP锚缩进拼错驱动提前退出；1d的过期刀仍存活（首批有界清理只删旧积压，尚未触及边界）。b48仅补第二次清理后边界必须保留的断言后完整重跑55；不把ERROR或编译失败计合格红。

`product-manifest.json`从Git对象计算相对main的完整产品/测试及VERSION/deps/renderer清单；新增保留期修订不改0077或版本号。`SKIPS.md`逐名附原因，不将SKIP计PASS。`raw/`为确定性gzip原日志，解压后SHA等于记录。编译日志也校验，不用stderr文字猜编译成功。

复核（仓库根）：

```sh
python3 docs/reviews/r60-send-lost/evidence-caller-retention/VERIFY.py
sha256sum -c docs/reviews/r60-send-lost/SHA256SUMS
```

`run.py`/`mutate-caller-final2.py`为本机执行器原文，复现时替换仓库工作树/隔离缓存/解释器路径；变异必须用单独clean detached b48工作树，不能对运行其他门的树做修改。日志不含私有凭据或真实正文，仅合成夹具。

证据不构成独立验收或部署GO；本轮未跑浏览器全量/identity整包/全包race，未声称①②完整unknown→status恢复联合格已完成，该格属于正式集成必须项。
