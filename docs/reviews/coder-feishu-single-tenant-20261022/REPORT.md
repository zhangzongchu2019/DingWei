# S-FEISHU-SINGLE-TENANT 候选

基r34 `c61866cd60ac196cc45f0d3c004e0c5e436a2f9d`，只改Hub及相关开发测试。冻结源 `7942d5071ba5f4e103250b5b06acf6b992fc862a`，tree `341ccb13b36e71c70023611e05024d19a8d61fa0`，tag `evidence/coder/feishu-single-tenant-341ccb13`。设计 `docs/designs/S-FEISHU-SINGLE-TENANT.md`。候选仅供组长审、prodops交叉核，不是dw123验收或上线授权。

## 最终行为

原生SDK gateway启动时提供当前issuer／AppID，无须新增飞书权限或调用tenant查询接口。某issuer未配租户映射时，只接受该原生认证事件：AppID准确匹配当前应用，header tenant非空且格式合法，与SDK sender tenant完全相等。映射存在则仍严格匹配，坏映射配置仍拒绝启动。FCS／proxy／Fake／Stub没有原生可信能力时不开放fallback；普通客户端JSON不能制造可信回复metadata。

此前出站通知准备也要求tenant，故一并解开必要前置：未映射的原生应用可准备PM身份／binding SHA／AppID／政策／修订／条目已绑定的通知，tenant暂空。可信回信按准确parent查找同tenant或未绑定tenant的通知，多条候选即拒绝。入帖事务只补全用于比较的tenant，其余快照字段逐项一致；PG锁与当前PM重核、原通知、有效期、政策／修订、重放、D2唤醒及限额均保留。不会用第一条事件修改全局映射或隐式给issuer设pin。

已看到的tenant持久化到0062原有 `spec_clarification_inbound.tenant` 和metadata_json；新增sender tenant随可信metadata持久化并可重读，通用message回复metadata也沿原JSON存储路径保存。不增加迁移、不改PG、helper、版本位、依赖或三开关默认值。公开帖子与普通消息JSON不新增tenant字段，tenant保留作审计标识。

## 正式证据

18项撤修全部指定断言红，恢复均绿（完整循环37次命令）；涵盖原生issuer装配、SDK sender证据、tenant空／格式／长度／控制字符、sender一致性、当前AppID、明确pin、认证／provenance、未绑定通知前置与准确parent、歧义拒绝、事务绑定与sender审计持久化。编译失败不能算指定红。回归含无映射通知→PM回信→补充／重放、显式映射拒绝、延迟回信在AppID或pin变化后重新拒绝、SQLite审计重读，以及原有clarification全套。无映射正例与严格pin兼容均有执行读数。

vet rc0；canonical `BUILD_TESTS_OK python=56 shell=2`（基r34原入口，未改helper／脚本）。全仓Go最终标准profile、包账本和跳过项以 `GO-FINAL-SUMMARY.json` 为准；严格内嵌PG、DSN未设、skip-ok=0，单包先准备好PG缓存，再默认并行运行全仓。原始JSON日志仅做无损gzip，原字节长度／SHA在LOG-COMPRESSION.json，可还原逐行复核。Go1.22.2、GOTOOLCHAIN=local、非root、私有短TMPDIR；canonical与跨运行时控制使用锁定venv和隔离HOME，未安装依赖、未连生产。

SOURCE-MANIFEST从Git列全部1874项非docs输入。VERIFY从Git核冻结源码、证据文件集／大小／SHA、18撤修指定红、最终门、helper／版本／迁移不变；不依赖工作区里另抄的源码。运行 `python3 docs/reviews/coder-feishu-single-tenant-20261022/VERIFY.py HEAD .`。

## 原样保留的红与结论限域

- 额外设置DW_DESKTOP_INTEGRATION_PYTHON的全仓首轮中，旧D3跨运行时格 `TestPilotQuestionAcrossRealHTTPHelperIPCMCP` 红。r34与候选同环境单跑均同样KeyError result，三份执行输入逐字相同。原因：旧夹具创建问题漏传schema现已必填的entry_refs，MCP返回-32602；只补该字段的本地schema控制可进入IPC层。额外门红不改写为通过，也未把旧夹具或helper改动混入本件；最终标准Go profile不设置该可选键，所有条件跳过明确列出。canonical现有真实跨运行时集合已通过。是否另立旧夹具修复由组长定。
- 空HOME的一轮标准全仓，nodegate五格在initdb阶段找不到同目录postgres，非业务断言。其后相同HOME下已具备这两个程序、同环境重跑nodegate全包rc0；最终先以一个PG用例准备完整缓存，再严格跑全仓。症状支持并行缓存准备窗口，但没有失败瞬间的系统调用归属记录，不声称定位到具体解压进程。未放宽产品检查或把起库失败改为跳过。
- 开发初轮歧义parent负格夹具漏policy_version导致NOT NULL错误，补齐夹具后通过；正式冻结后无源码改动。辅助schema控制初次误要求通用错误文本包含字段名，实际是泛化invalid tool arguments；最终只比协议错误码与补字段后进入IPC，原始输出保留。

## 上线与回退交接

本件不访问dw123、不发真实飞书消息、不操作席位或开关。真实验收只按第95条在现有dw123、DingWei本组安排，不向dataConnector索要测试。若已有开关启用，本修复会解除合法pending_config通知的租户前置阻塞；执行前应盘点pending_config／prepared及收件人，确保真实通知仅本组，仍由组长／prodops安排。

schema未变，旧二进制可忽略新JSON字段，但旧版不识别空tenant通知的回信匹配。回退前关闭相关试点出入站并冻结新增写入、对账在途；不清账本重放，不承诺这些通知可在旧版继续回复。已入帖及审计保留。若未来采用严格映射，由管理员核对审计后填写并重启，不自动把审计转成授权配置。跨版本重复输入摘要变化时失败关闭，不据此生成新楼层。

最终全仓实取：rc0，66包＝53测试包通过＋13无测试文件；39个条件用例跳过（逐项列于GO-FINAL-SUMMARY.json，新增SingleTenant用例无跳过）。额外跨运行时全仓原轮为52包通过／1包红，冷缓存标准轮为52包通过／1包红，完整错误与环境对照均保留，未覆盖或重写。
