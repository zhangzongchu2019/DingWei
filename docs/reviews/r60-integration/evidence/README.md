# 固定源证据

source a311aaf3beea90262c05ca2c316064b8a8856d06。gate-summary.json为完整门；5份m8 JSON/日志逐次保存，一轮指定格FAIL未抹除；其他三树各包在go-other-four-trees.log。canonical-build.log含两profile1270各自记录、完整run-build-tests与四二进制构建。browser-all.log与browser-receipt.txt包含45套件和两个cache路径覆盖登记，未覆盖生产网络。

run wrapper记录精确argv/白名单env/UTC/uid/raw SHA/起止HEAD和dirty。HOME/TMPDIR/Go缓存为本地隔离位置，无继承SH_/DINGWEI_/DW_/DATAD_环境；DW_DESKTOP_INTEGRATION_PYTHON是显式隔离测试解释器，非在线配置。PGstrict临时库、无模型/现网/在线席位。变异有意dirty，source_commit为null而base_source指固定源，不伪称原树；before/after与编译rc0/业务rc1/非空断言在mutations.json。两绿baseline/heartbeat-restored在还原后，原始失败/不合格锚尝试另存，不计8刀。

85文件product-manifest覆盖实际非docs/openspec产品差异并含版本/依赖/renderer热修回归，VERIFY.py用Git对象和当前文件重算，不能用index哈希代替字节还原。测试/门原日志约17MB，只进私有Git；bin目录为本地构建件不归档/不分发。caller/源消息与投递ID全部是隔离合成数据，不含在线席位历史。

源码交换只经Git；他席评审12项验证单单列qwen-report-git-verification.json，不冒称本席重跑其原始门。自动清理/监控未新增，磁盘无硬上界等边界见发版草稿。运行原脚本需按本机调整隔离缓存/解释器绝对路径；只读核验可在独立fetch树运行python3 docs/reviews/r60-integration/VERIFY.py和仓根sha256sum -c docs/reviews/r60-integration/SHA256SUMS。

浏览器原日志自带部分行末空白；按原字节保留并核SHA，不修饰日志来通过git diff --check。非原始.log文本另做空白检查。
