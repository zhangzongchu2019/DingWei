# DingWei SP0 / SP1 并行设计协调合同

> 业主澄清：`SP0-1` 表示 **SP0 和 SP1 两个包分开同时做**，不是一个合并包，也不是一个交付物。
> 本件只冻结共同输入、并行关系与独立门；产品设计分别见 `specs/sp0/spec.md`、`specs/sp1/spec.md`。

## 0. 头块

| 项 | 值 |
|---|---|
| **Coordination ID** | `dingwei-sp0-sp1-parallel-design` |
| **版本** | v2.0 |
| **状态** | `draft-for-review` |
| **执笔** | `zzc-chatgpt56soltchangshark-2642` |
| **评审** | `zzc-opus50tchangshark-2642`、`zzc-deepseekv4pro08130ca16c13-2642` |
| **冻结施工锚** | `46b79ab0022b0686a23455f328c9316d7f87281f` |
| **SP0 合同 / 目标** | `specs/sp0/spec.md` / `docs/dingwei-sp0-design.md` |
| **SP1 合同 / 目标** | `specs/sp1/spec.md` / `docs/dingwei-sp1-design.md` |

## 1. 并行不变量

1. SP0 与 SP1 共用冻结输入，但各自产出、各自检查、各自评审结论；不得合并为一件。
2. SP0 PASS 不替 SP1 背书，SP1 PASS 不替 SP0 背书；任一包 NO 只阻断该包正文。
3. 两包可同时设计；SP1 消费 SpecDoc 已生效的 SP0 产品规则，不等待本轮 SP0 设计件成为新产品权威。
4. 两包若对同一术语、接口或状态产生不同定义，登记冲突并交 coordinator/业主裁，不按提交先后覆盖。
5. 不新增依赖、不扩需求、不实现代码/schema/部署；现有代码只证现状。

## 2. 共同冻结输入

| 输入 | 版本 / sha256 |
|---|---|
| `docs/dingwei-platform-specdoc.md` | v3.69 / `8b0b20b97af85bf1c42c1b8dd5a7c94a0e73d1de3100666c5acb2370ae56c61e` |
| `docs/dingwei-platform-architecture.md` | v1.3 / `0a2288c7b9e245eded06944cd063a6d0d458c6b1f45a3a9534d29817b6c58fdc` |
| `docs/dingwei-prototype.html` | v1.47 / `eeb71a5a77a53990569e08f9745165903bcf9e34cf6ce0a5047824167e1df4b8` |

## 3. 独立开工门

### SP0

- [ ] `specs/sp0/contract-check.sh` PASS；
- [ ] opus50 对 SP0 合同 PASS；
- [ ] deepseek 对 SP0 合同 PASS；
- [ ] coordinator 登记 SP0 合同锚。

### SP1

- [ ] `specs/sp1/contract-check.sh` PASS；
- [ ] opus50 对 SP1 合同 PASS；
- [ ] deepseek 对 SP1 合同 PASS；
- [ ] coordinator 登记 SP1 合同锚。
