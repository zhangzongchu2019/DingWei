# 契约版本 Registry(G2,契约 v0.3 §G2;owner=devo48 起草,coordinator 核完整性+登记)

验收锚一律认 **tag^{commit}**,不认分支 tip(G3)。锚全用完整值(F7)。

## interface-contracts-v1(Phase-0 冻结,现行)

- annotated tag: `interface-contracts-v1` → tag object `50d6393cbc9c8e620e4e7cb5a3656b90a079f81f`
- tag^{commit}: `107015744b0e56c9645c84eb6ed97bf479c7d918`(impl/p3-prep)
- 冻结批准: 五签(prodops/shencedeepseek/tester/claudek3/dev56 终稿轮全 AGREE)+board vNext 重开门=张宗楚拍板(07-27,经 coordinator 转达)
- 评审过程: v0.1(bbf6c7f)→五方一轮→v0.2(06ef4d64)→五方二轮(4 AGREE+dev56 F1-F7)→v0.3 终稿轮机械核(规约21 两轮+机械核闭)

| 件 | path | blob | 文件 sha256 |
|---|---|---|---|
| 契约正文 | docs/v0.1/interface-contracts-v0.3.md | `efafd9d88f030540d2a39864673ad4361e1386c4` | `28f09dae98ad15e6d415fdfcda0840e63fdbb08c64549516a02c2f30823e2807` |
| F5 golden vectors(20条) | docs/v0.1/contract-cases/jcs-golden-vectors-v1.json | `ff0fc6593caf4f2f9e15a88ee02874e844f9151e` | `b45763ef32973470f9a2dd7330e981eecf0f0ef145cf2cb9db411cd78e1db58a` |
| JCS 参考实现+校验器 | docs/v0.1/contract-cases/jcs_vectors_check.py | `40697f84406e4685901c153bbf45b8c7fe71cbad` | `be587e7cb8c3f3404b19e09d7a6bfd39385a95ab5a3ef4ee1475cde3f5a1e99a` |
| 契约级用例(23条) | docs/v0.1/contract-cases/interface-contracts-v1-cases.json | `771c26d8b21514d0d7c454ed7dfeffb581575cee` | `41ed58fafeb794aeb4bf8f541c43ee3d581be34ffcdca2816f075ab034fb66d7` |

### 分契约条目

| contract | version | owner | consumers | compatibility | status |
|---|---|---|---|---|---|
| 契约1 board(vNext,重开门) | v1 | 调度组(执笔待拍 X/Y/Z;board vNext 实现建议归轮换组 dev56) | 全组 | major(对 m0-final 扩展,已重开门批准) | frozen |
| 契约2 信道(hub/桥接) | v1 | 信道组 devo48 | 调度组/A组(E门夹具经 §2.3 kit) | 新契约 | frozen;Go 面=internal/bridge@d56cf15d(C-P0-1) |
| 契约3 worker/pool | v1 | 调度组 | 轮换组/信道组 | 新契约 | frozen |
| 契约4 轮换 | v1 | 轮换组 dev56 | 调度组 | 引用 m0-final C-1~C-10 | frozen(引用件另有 tag) |
| 契约0 worker CLI(wkr.py) | v1 | 调度组供 | 全 worker 会话 | 新契约 | frozen |
| DeliverFail DEA 判定 | v1 | 驱动/DEA(权威)+board(消费) | 调度组 | 冻实(F4) | frozen |

### 引用外部锚(F7,完整值)

| 锚 | 值 |
|---|---|
| m0-final tag object | `fc5275cf7bb72c4c37df05749c69df93d7143456` |
| m0-final^{commit}(轮换契约 v0.7) | `e49378b76cad20a457687505468da3189ea964c1` |
| M0b probe | `33888822f0f7692839ed16d1a0ff280742fcbf0f` |
| DEA authority | `6f1b07116dc806ad3e6b6e583913a92a718aa450` |

## 变更规则(G2)

兼容新增=minor;删除/语义变/鉴权变=major+重开门(owner 起草+受影响消费者双审+张宗楚批)。运行消息带 schema_version;未知 major 显式拒不静默降级。vNext 待启用判据:N5b/CT-O1-02(O1 digest 冲突)随 board vNext 实现落地后启用。
