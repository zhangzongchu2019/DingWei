# 逐门逐包与SKIP清单

计数为运行实例，含count重复；top不含子项，all包含各级子项。FAIL列表含父项，不能直接当独立缺陷数。

| 命令日志 | 包 | top PASS/SKIP | all PASS/SKIP | FAIL项 | interrupted行 |
| --- | --- | --- | --- | --- | --- |
| go-packages-count3 | github.com/zhangzongchu2019/dingwei/internal/mcpgw | 285/6 | 1149/18 | 0 | 0 |
| go-packages-count3 | github.com/zhangzongchu2019/dingwei/internal/extseat | 69/0 | 162/0 | 0 | 0 |
| go-packages-count3 | github.com/zhangzongchu2019/dingwei/internal/m8 | 2990/33 | 5035/87 | TestViewMentionRoute/a_peer_without_delivery_ack_v1_is_not_sent_to, TestViewMentionRoute | 3 |
| z3-baseline4a-view20 | github.com/zhangzongchu2019/dingwei/internal/m8 | 19/0 | 238/0 | TestViewMentionRoute/a_peer_without_delivery_ack_v1_is_not_sent_to, TestViewMentionRoute | 3 |
| z3-fresh-1 | github.com/zhangzongchu2019/dingwei/internal/m8 | 997/11 | 1679/29 | 0 | 0 |
| z3-fresh-2 | github.com/zhangzongchu2019/dingwei/internal/m8 | 997/11 | 1679/29 | 0 | 0 |
| z3-fresh-3 | github.com/zhangzongchu2019/dingwei/internal/m8 | 997/11 | 1679/29 | 0 | 0 |
| z3-fresh-4 | github.com/zhangzongchu2019/dingwei/internal/m8 | 997/11 | 1679/29 | 0 | 0 |
| z3-fresh-5 | github.com/zhangzongchu2019/dingwei/internal/m8 | 997/11 | 1679/29 | 0 | 0 |
| migration-full | github.com/zhangzongchu2019/dingwei/internal/store/migrate | 29/0 | 44/0 | 0 | 0 |
| cmd-focused | github.com/zhangzongchu2019/dingwei/cmd/workpulse | 18/0 | 30/0 | 0 | 0 |
| focused-race | github.com/zhangzongchu2019/dingwei/internal/mcpgw | 18/0 | 44/0 | 0 | 0 |
| focused-race | github.com/zhangzongchu2019/dingwei/cmd/workpulse | 4/0 | 5/0 | 0 | 0 |
