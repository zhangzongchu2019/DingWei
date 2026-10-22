# DAEMON-IDENTITY initialized通知竞态（test-only，交审）

仓库wego-ai/dingwei，分支`review/coder/daemon-identity`。父锚ce58c1bfd0a49e3d12161df6b1dffcea38cfb945；冻结测试提交ee8025ef9ef9757f1af5bf90e5009a77715ba795，tree c19b7c767e4f572aaa641f60ffc3caacf121e966。之后仅补本目录docs／证据。

组长提供的r37 258267c7失败原文为：默认observe格在peer.methods仅看到initialize时就断言完整列表；模块单跑10／10通过。该r37失败日志未转交本席，本报告不冒称独立复现了该集成失败；静态时序与下述可控延迟均支持竞态原因。

## 修改与同类断言核查

非docs仅tools/sessionhelper/tests/test_daemon_identity.py：增加monotonic最多5秒、10ms间隔的条件轮询，直到假peer记录initialized，再保留原列表精确断言及OBSERVE_REJECTED_CONNECTION标记。每次检查即满足便退出，没有固定睡满5秒，也未删列表／warning／事件断言。

- 同文件initialize计数在假peer发送对应同步响应前已经append，所以start返回后的initialize计数断言无需补等待。
- 两个拒连后立即断言零RPC的负格，改为先最多5秒等received_done，确保假peer确已读完并关闭，再核零RPC；原有同类关闭等待由1秒统一为5秒。失败标记保留。
- owned/enforce／投递账本／off／observe双重连等其余断言分别由同步返回或本线程事件驱动；未放宽预期、未改假peer协议或产品码。

## 独立读数

冻结源、非root UID1001、Python3.12.3、既有锁定venv `/tmp/coder-0232-locked-venv`；每个进程各自私有HOME／TMPDIR，env白名单＋源码PYTHONPATH，无在线helper、生产Hub或模型调用。

RUN.py：4个并发worker为一轮，共5轮，累计20次完整test_daemon_identity模块（18格／次，共360次执行），全部rc0、无FAIL／ERROR／SKIP。每轮以进程起止monotonic区间硬比，四个进程有共同重叠区间，非把四个串行命令称并发。STRESS.json与20份stdout／stderr／rc保留命令、源码锚、UTC与区间。

另在独立复制的同一测试文件上，仅把LocalPeer的方法列表换成“收到initialized后200ms才append”的夹具：原样绿→仅撤上述等待块后指定OBSERVE_REJECTED_CONNECTION AssertionError红（无ERROR）→恢复绿。第四格故意不记initialized，保留等待的测试约5秒后以同标记红，并断言总耗时在4.5–8秒内；证明有界等待不会吞掉通知缺失。原文件不被修改，复制件SHA、补丁开关与原始日志在PROOFS.json。产品源码始终相同；通知延迟属于假环境控制，不是生产负载实测。

VERIFY.py从Git核测试文件、限定差集、原始日志SHA、20轮18格与并发区间、定向延迟撤修／恢复及缺失通知边界。未重复全仓Go／canonical／helper全套：本件只改测试同步，组长r37集成全门仍须按其流程收尾。本读数不代表r37 849项独立通过，不部署、不改配置或在线席位。
