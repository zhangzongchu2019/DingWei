# HKF-PM04 真实 WS 终端输出对照补证

这是作者本地受控合成突发测量，非现网日志取样、非现场峰值、非完整Hub端到端压测；不推算历史419次失败占比或生产SLO。组长2026-11-16T19:33:23Z要求补此边界，产品冻结1671a596不变。

## 方法与范围

精确从Git读取1671a59667c544ff3a3e0270ac2a293e15560d20和d2cc47c7e699eb45efb3a07ee197aa8c20a9f07e的SessionHelper.terminal_loop，通过AST仅编译这一原方法，使用真实asyncio.to_thread及真实websockets15.0.1连接；不是fake ws、不是手写替代发送循环。baseline是去掉本次“有界批/UTF8分块/逐块让出”的完整原方法，不改其它方法。输入是标准queue接口，预置1024个8192 UTF8字节块（中/emoji/ASCII），合计8388608字节；没有真实CLI/LLM/IPC、没有真实消息或凭据。driver/cli的next_terminal_chunk本来从queue取未设单块长度界的数据，本测量只选择其中一种允许的合成排队突发，不宣称这是现场典型大小。

Go端用与Hub相同的coder/websocket v1.8.13、默认禁压缩、16MiB读取界，独立进程持续Reader并在同连接上发Ping/收Pong；未启用Hub鉴权、DB、viewer、业务队列，目的是隔离发送端背压。Hub完整生产路径的6.1秒业务阻塞/Pong测试仍由既有证据覆盖，本测量不替代它。链路是loopback ws://而非公网/TLS；Go内TCP代理只给client→server字节限速，反向不加延迟，不伪造Ping/Pong。

每种限速只建一条真实WS，在该连接上交替测试chunked/baseline、baseline/chunked两轮（即4次突发）；控制与终端数据共用这条WS。Go每50ms触发一个Ping，最多一个在途，观察超时设20秒以量到超过Hub默认5秒的迟到Pong；above_default_5s是阈值比较，不是假报生产Hub真的关闭连接。长期在途会减少Ping样本数，不能把成功数当成功率比较。每轮服务端累计正文SHA，均完全一致且顺序未变。

帧峰值从真实websockets protocol.send_frame旁路只读记录len(frame.data)，原方法仍照常发真实帧；不把body长度当frame长度。表中为帧payload，排除WS头/mask、TCP/TLS开销。本组未协商压缩。chunked每轮280帧，baseline每轮1帧；body上限32768不等于JSON/frame payload上限。

## 主读数：保持客户端默认socket参数

主组不调用SO_SNDBUF设置，实测客户端SO_SNDBUF=2626560，asyncio transport写水位8192/32768；Go限速代理接收缓冲请求32768、块8192字节。代理使用累计deadline pacing（空闲后重置，允许25ms调度追赶），限速值是上限，实际正文吞吐见表，不能把标签当精确线速。无1C CPU限制；affinity/cgroup实际值在environment.json，不冒称1C/特定生产机器性能。

| 配置限速上限 | 变体 | 实际帧payload峰值（字节） | 两轮最长Pong RTT（ms） | 两轮正文吞吐（Mbps） |
|---|---|---:|---|---|
| 不限速 | baseline | 8388833 | 71.602 / 56.261 | 378.620 / 370.291 |
| 不限速 | chunked | 32993 | 3.458 / 3.757 | 113.598 / 103.592 |
| 50Mbps | baseline | 8388833 | 1403.822 / 1412.463 | 44.426 / 44.566 |
| 50Mbps | chunked | 32993 | 402.094 / 400.809 | 48.268 / 48.386 |
| 10Mbps | baseline | 8388833 | 6782.564 / 6766.208 | 9.747 / 9.757 |
| 10Mbps | chunked | 32993 | 1986.856 / 1766.046 | 9.880 / 9.875 |

主组命令与时间：在仓库根，以runs.json中的完整白名单env运行`env PROBE_SOCKET_SNDBUF=0 PYTHONPATH=tools/sessionhelper python3 <本地测量目录>/probe.py 2`。UTC 2026-11-16T19:52:39.331224+00:00 至 2026-11-16T19:53:19.213409+00:00，rc=0；原日志SHA256 `da432d7b8d22490961fbb06132a8a743e98cd2a2750eb7a9f4262e6cad84ce77`，在raw/terminal-wire-default-buffer.log.gz中。Go先按同环境`go build -o <本地目录>/server <本地目录>/server.go`，编译命令/UTC/SHA在runs.json。运行时仓库HEAD为d69defeae20a2bc956506e5039d85dbfed2e8ba1且起止clean，与产品167五产品子树相同；额外测量夹具当时在仓外，现按environment.json的SHA入Git，不伪称它当时已在产品锚里。

正文每轮SHA256 `0eae9225a496a4c4d636a07a5a89fdcb441c05073d7eb333ebd8232aa3dc31ed`；12轮总量/顺序均由服务端核过。原始逐Ping读数、帧数/峰值、起止UTC在readings-default.json，未只保留汇总。baseline两轮10Mbps最长RTT均大于5秒，而chunked均低于2秒；50Mbps、无限速下也存在控制延迟差异。因此HKF-PM04有受控实测支撑，作者建议保留分块，是否满足并入条件由组长确认，不自签收。

## 补充组与限制

readings-small-buffer.json为校准限速器、显式SO_SNDBUF请求32768（实际65536）组。10Mbps baseline6754.176/6795.372ms，chunked146.507/146.607ms；50Mbps baseline1392.046/1401.885ms，chunked31.173/27.796ms。该组只是缓冲敏感性对照，不能用146ms替代默认组的1766～1987ms，更不能当生产保证。将probe-small-buffer.py.txt复制为probe-small-buffer.py可复现当时脚本，或用probe.py.txt复制的probe.py显式PROBE_SOCKET_SNDBUF=32768。

首轮terminal-wire-probe使用逐块固定sleep，调度开销会累积（尤其50Mbps实际低于上限）；只作方法历史保留原log/SHA，不作为主表依据。之后改累计deadline pacing，主/补充组共用同一校准server。初轮未保留该版server源码副本，不能冒称可逐字节复现该历史夹具；主组和校准组源码及二进制hash均可核。

分块增加帧数和JSON开销，并增加Python逐字符处理CPU；不限速总吞吐可能下降，表格已如实列出，不说全方位性能提升。本测量是整体有界批/分块/公平发送对照，没有单独量化sleep(0)那一行的真实网络因果；原先两刀是受控AST断言，不混成此次实测。默认缓冲、RTT/丢包、TLS、CPU、输出形状变化仍可能使延迟超预算；只支持“此允许的突发/限速条件下存在影响”，不保证任意洪泛不断连，不用两轮推统计故障率。

## 复现

在冻结产品树或产品同字节的当前文档tip，创建仓外目录，将本目录server.go.txt/probe.py.txt复制到仓外并分别命名为server.go/probe.py；仓库根设置固定白名单env，`go build -o <目录>/server <目录>/server.go`，然后`PROBE_REPO_ROOT=<仓库根> PROBE_SOCKET_SNDBUF=0 PYTHONPATH=tools/sessionhelper python3 <目录>/probe.py 2`。仅127.0.0.1监听，不需key、DSN或真实模型。每轮有45秒上界；不要上传生成server二进制、私有env或凭据。源码交换只经本Git分支。

测量夹具以.go.txt/.py.txt的文档源码清单形式入库，避免给go build ./...引入新的docs包或给运行时新增入口；复现时才在仓外按上面改名。文本字节SHA与实跑夹具一致，未把二进制制品纳入Git。
