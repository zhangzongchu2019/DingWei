# S-MCP-DCR 实现交件

状态：作者实现与自测交审，独立复核及组长终审未完成。无 main 推送、无部署、无发布制品。

## 授权与锚

设计按组长2026-11-19T08:37:52Z四条条件修订，设计冻结80f6f8392a41f120b9bbe7ae958827b637b55cf0；08:54:56Z确认共享公共client按(resource,client_id,principal_hash)分绑定。f38152fb仅将spec概要中的旧24h表述对齐已批两段TTL，不新增政策。基点4a349ee59a1d8d7b8b9c562c462bded2f8b0f192。实现及测试冻结5bc0280d7d6d8da8402ed6459766ce9ae1e0e0f4，后续交件只docs/openspec。VERSION仍0.2.3.41。

## 实现与可见行为

- 增加受限公共DCR：none／authorization_code／code，强制S256；只发client_id，无client_secret或管理凭据。原内置Codex/CIMD/权限不变。
- 回环仅127.0.0.1、::1、localhost；HTTPS完整URL精确名单初始空；自定义scheme、通配、userinfo、fragment、非回环HTTP拒绝。实际authorize URI再次校验，不只校验登记元数据。拒绝原因固定四枚举，自定义scheme只过滤后最多32字节，不记整URI。
- 三张独立PG表存加密metadata、主体绑定、限流桶；未绑定绝对24h不续，成功token兑换才绑定/续期默认30d，可配7～90d；稳定外部主体四元组HMAC，最多16个存活绑定，淘汰最久未兑换者。公共client可被多人使用，绑定隔离，client_id不进入Agent身份或去重键。
- 可信代理显式CIDR，右向左取首个不可信地址；非可信直连忽略XFF。持久source/global桶、存活额度、32并发、16KiB/3秒请求体、2秒DB事务、分批GC。GC先子后父，避免共享client的级联删除无界。
- 默认关闭；开启且存储ready才公告registration_endpoint。关闭后停新注册/新DCR authorize，已建五分钟快照可收尾，既有bearer仍按业务鉴权。未认证GET/畸形请求先401原challenge，认证GET405，不做旧SSE。
- 无Agent自动去重复用修法，不新增按client_id开户路径。既有IssueToken再次授权仍可能建新seat/AG，属另单。

## 迁移与固定夹具

仅新增PG 0115_mcpgw_dcr.sql；旧迁移字节、应用SQLite迁移、history迁移、依赖、go.sum未改。0112/0113/0114由其它r62片占用，起终两次核号见证据。终核main=e07ad81ebea94ba41316b011df751ab400091cc1，独立fetch后读取对象，与基点产品路径/依赖/VERSION零差；NAME=ceab869c、GROUPS=4fd154f8，均无0115。基点不随门中main文档前进而改写。原有精确迁移清单/表计数夹具显式加入0115与三表，未改成宽松断言。

## V01～V07 覆盖

| 格 | 实际测试 | 作者删保护刀（不与他席相加） |
| --- | --- | --- |
| V01 | TestDCRActualHTTPRegisterFeishuTokenAndInitialize；真实本地TLS/HTTP、PG、登录/Issuer/外部席服务；仅上游飞书返回桩，无预建Agent | K01/K22/K29/K39 |
| V02 | PublicMetadataAndRedirectPolicy、LoopbackPortDoesNotChangeOtherBindings、AuthorizeIsolationActualRedirectAndClosedSnapshots | K02/K16/K33/K34 |
| V03 | 公共metadata及真实HTTP错误、auth method/secret拒绝 | K03/K41 |
| V04 | PG持久限流/并发/额度、代理、32槽、慢体、DB取消/截止 | K04/K09～12/K15/K18～20/K30/K35～36 |
| V05 | PG生命周期/续期/主体16上限/GC/共享主体/配置缩期/快照 | K05～08/K13～14/K21/K24～28 |
| V06 | 内置/CIMD并存、关停快照、旧池独立、真实旧Hub回退探针 | K17/K32/K37/K40 |
| V07 | AuthFirst真实HTTP及旧codec既有格、关闭/HTTP协议边界 | K31；既有边界不冒称本次新刀 |

全部41刀在同一5bc源上编译rc0、测试业务断言红、原文件SHA逐字还原、对应未变异格rc0；详细编辑点/命令/UTC/输出SHA见mutations。早期72e候选K15初刀存活（外层trust删后内层仍保护，未覆盖非可信畸形XFF忽略分支）；补精确测试后同刀在最终源业务红。历史14红1存活不与最终41刀相加。

## 正式门

固定源5bc0280d7d6d8da8402ed6459766ce9ae1e0e0f4，普通用户dev，env白名单，go1.22.2、Python3.12.3、PostgreSQL18.3。命令/UTC/rc/SHA见evidence，逐包表见GATES.md，逐名SKIP条件见SKIPS.md。

- canonical-final rc0：BUILD_TESTS_OK helper_profiles=2 python=27 shell=2；两profile各1290、inventory97，无helper SKIP。
- 三包-count=3原始rc1：mcpgw 285顶层/1149含子项PASS、6/18 SKIP；extseat69/162 PASS、0 SKIP；m8 2990/5035 PASS、33/87 SKIP，唯一失败叶项TestViewMentionRoute/a_peer_without_delivery_ack_v1_is_not_sent_to（view_mention_route_test.go:420 回执=[]）及其父项。同段3行lease release interrupted(9)清理诊断，GoTest日志归属不作为因果证明。原始非绿未修改。
- 基线4a349ee5 TestViewMentionRoute -count=20也有同一子格/同一断言失败一次（19顶层/238含子项PASS），同段3行interrupted(9)；只作为继承形态对照，不拿不同命令的小样本估计失败率或证明增量因果。
- 依现行Z-3口径，在同源全新串行五轮，不沿用上面三遍，不择优：
  - z3-fresh-1：rc=0，top PASS=997，all PASS=1679，FAIL=[]，interrupted行=0。
  - z3-fresh-2：rc=0，top PASS=997，all PASS=1679，FAIL=[]，interrupted行=0。
  - z3-fresh-3：rc=0，top PASS=997，all PASS=1679，FAIL=[]，interrupted行=0。
  - z3-fresh-4：rc=0，top PASS=997，all PASS=1679，FAIL=[]，interrupted行=0。
  - z3-fresh-5：rc=0，top PASS=997，all PASS=1679，FAIL=[]，interrupted行=0。
- 五轮全绿5/5，门要求至少3；允许名单外失败/非允许形态停报，未扩名单、未改store。初始门rc1与补证结果分列，不宣称整段测试零失败。
- migration-full：rc=0，top PASS/SKIP=29/0，all PASS/SKIP=44/0；完整包/失败见GATES。
- cmd-focused：rc=0，top PASS/SKIP=18/0，all PASS/SKIP=30/0；完整包/失败见GATES。
- focused-race：rc=0，top PASS/SKIP=22/0，all PASS/SKIP=49/0；完整包/失败见GATES。
- cmd-focused已设DW_DCR_OLD_BINARY，TestDCRAdditiveMigrationOldBinaryAndForward三次实际PASS、0 SKIP；focused-race未出现DATA RACE。
- strict-final rc0：48 passed／0 failed；勾选tasks及最终报告后documentation-strict再跑rc0，同为48/0。

## 开发失败与修正历史

1. dev-dcr-1：额度夹具未计第六次拒绝也扣global attempt、以及多语句带参prepared SQL夹具错误，修正测试。
2. dev-dcr-3/4：慢体/早拒绝的HTTP连接处置实质问题，读截止后错误清除及server自动drain可继续等待。修订为请求Close加Connection close、仅完整读完后清read deadline；最终格和K19/K20钉住。
3. 首轮canonical在4f4289cf rc1：两profile绿，末尾真实PG认领五子格因旧精确迁移清单未含0115红；不是环境抖动。dev-ledger-correction进一步发现领域表清单/计数未补，均保留原日志，显式补0115/三表/计数后在最终源重跑全门。

## 回退与实测边界

使用独立本地4a测试exe，SHA与Go工具链见old-build.result.json；未作为发布件、未上传二进制。TestDCRAdditiveMigrationOldBinaryAndForward实际运行旧进程在施加0115的新库上bootstrap读账本并前滚；真实旧Hub HTTP进程兑换预先由新SQLStore构造的有效DCR五分钟grant并拒绝重放。此格是协议快照/迁移探针，不是实际CodeBuddy或生产回退GO。

## 已知边界

- 真实CodeBuddy IDE callback形式、真实飞书登录与公网网络未实测；公开资料不等于客户端实测。云WorkBuddy资料涉及广域HTTPS/refresh，本片不采纳通配/refresh能力。
- DCR默认关闭，运营须按runbook配置可信代理和精确HTTPS列表；公开客户端名称不代表厂商认证。私有scheme仍拒绝，若实际客户端需要须另审。
- 4096是存活client额度，不宣称整个PG表磁盘硬上界；同一公共client可有多主体绑定，GC每批有界，积压须按runbook读表观测。GC失败保留待后轮，不声称已有专用失败计数器。
- 旧Hub可收尾已有5分钟授权code，但不会续DCR绑定；关闭/回退超过TTL后需重新登记。既有token不因client清理被撤回，仍受席位/令牌业务有效性检查。
- 自测不替代qwen独立复核及组长终审；无部署、无main、无发布件。

交件记录UTC：2026-11-19T10:41:39.368800+00:00；已完成实现及作者门（含按口径Z-3补证），待qwen独立复核和组长终审。
