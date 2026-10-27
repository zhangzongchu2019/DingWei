# ACK v2：实际 CLI 端到端门

验收合同来自 `7af11ba96aac10cbd1d0ce1b78fde819fc1e1f29` 的④设计；本门不部署或接触在线 helper。

`internal/m8/delivery_ack_real_cli_e2e_linux_test.go` 启动隔离 Hub、原 SessionHelper、DriverAdapter 与生产协议 driver，以及 PATH 中实际 `claude` / `codex` 可执行文件。模型 HTTP/SSE 端点只控制模型输出及释放时刻，原生接纳/终态必须由实际 CLI 产生。合成 app-server、手工 helper ACK 均不能替代此门。

运行：

```sh
DW_ACK_V2_E2E_PYTHON=/path/to/locked/python3 \
NODE_PATH=/path/to/node_modules \
go test -p 1 -count=1 -v ./internal/m8 -run '^TestDeliveryACKActualCLI'
```

Python 必须有项目锁定的 websockets 依赖；Node 必须有 Playwright，Chrome 使用 `chrome` channel。缺环境记 INCONCLUSIVE，不能用单测夹具转绿。全部测试状态、身份凭据、模型凭据、原生 ledger 和数据库均为本轮隔离件；不读取本席密钥或生产数据库。不改 HOME，不安装共享启动脚本；实际 Codex 的现有 MCP 名称仅用于逐个禁用连接，配置值不进入日志。只终止本测试创建的独立进程组。

判据覆盖：

| 判据 | 实际链路与时间屏障 |
| --- | --- |
| 1–4 | A 模型保持运行，B 排队超过31秒；实际 Hub 数据库与实际 renderer 订阅 Hub 事件流共同校验。释放 A 后 B 必须有原生 admission，B 终态前不能 processed。释放 B 后用原生 terminal 的 turn 与时刻验终态、同卡更新。 |
| 5 | 排队及终态各重投相同原信封；终态后重放捕获的原 queued ACK，等真实 Hub 确认；验证原时刻、阶段与模型请求数。 |
| 6 | `scripts/test-delivery-ack-real-mutations.py` 在一次性 Git 工作树恢复 enqueue 即 admission、admission 函数返回即 terminal；分别要求红在判据2和4。 |
| 7 | 两 driver 分别丢 injected 及丢其持久确认；断开 WS，保持实际 CLI 完成，再连回原 inbox。原 injected 前驱与 processed 后继有序补放，不新增模型请求。 |
| 8 | 两 driver 分别卡住请求已送但回执未读、回执已读但 inbox admitted 未写两窗口；终止隔离进程组，再用原 ledger 恢复。等待实际原请求查询及恢复结束；只能找回原 turn 或隔离 unknown，禁止新请求和假终态。去恢复对账的负控必须红在判据8。 |
| 9 | Git 锚 ec40752d 的实际旧 helper，真实 CLI 完成而 ACK 丢失，换新 helper；同一 View 保持观察升级。旧件仍 legacy，不得换成模型注入/终态；新 v2 件正常。迟到帧使用真实旧 helper 原样捕获 ACK。 |
| 10 | claim 前暂停及 dispatch-intent CAS 后暂停，分别让取消/派发获胜。真实 helper 生成取消回执，Hub CAS 后才可取消；后一方向必须 not_cancelable 且仍能完成原回合。 |

负控命令（ref 必须包含本门文件）：

```sh
DW_ACK_V2_E2E_PYTHON=/path/to/locked/python3 \
NODE_PATH=/path/to/node_modules \
python3 scripts/test-delivery-ack-real-mutations.py --ref HEAD --logs /absolute/evidence/mutations
```

两次 ACK 重放测试只丢包、断连接，不构造 admission/terminal；旧连接迟到测试只重播原始捕获帧。浏览器代理仅只读转发 Hub 的原事件环，不直接调用页面投影函数。另有42条投影浏览器断言覆盖时钟回拨、重复重绘、三种 outcome、桌面与混版文案；它们不计作上述真实 CLI 门的替代。
