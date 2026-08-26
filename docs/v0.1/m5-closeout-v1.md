# M5 会话轮换实现收口包 v1

## 交付结论

M5 实现、组内 sign-grade 与 board vNext conformance 均已闭合。

## 冻结基线

- Phase-0：`interface-contracts-v1^{commit}=107015744b0e56c9645c84eb6ed97bf479c7d918`
- M0 契约：`m0-final^{commit}=e49378b76cad20a457687505468da3189ea964c1`

## 实现锚与评审

| 锚 | 内容 | 评审状态 |
|---|---|---|
| `d3d2d4e7184e9bd8d605427cf30311e77a328fc4` | 轮换状态机、challenge gate | shencedeepseek APPROVED |
| `4d70b04cc2b37e90fb816aef850b1dfec204b0e9` | 有界重试、probe sequence 防重放 | shencedeepseek APPROVED |
| `e10a6e0134cc832c73fcdda45e6eee52a4ec36f5` | usage 去抖、rotation ID、rebaseline | shencedeepseek APPROVED |
| `c96d08950ccfad101731821d6e43e275f5561469` | durable store、首 outcome 重放与冲突防护 | shencedeepseek APPROVED |

实现分支：`wip/m5-claude`。

## 已闭能力

- 状态迁移严格遵循 Phase-0，Takeover 只进入 `ACTIVATING`。
- challenge 仅接受 fresh/ok 且 worker、probe sequence 匹配的结果。
- spawn 重试有上限、指数退避与可注入时钟，不热循环。
- usage UNKNOWN/非法/乱序均 fail closed；阈值计算无整数溢出。
- rebaseline 清空连续计数并生成新的 rotation identity。
- durable store 使用临时文件、文件同步、rename、父目录同步顺序。
- 同 request 重放首果；request/rotation identity 冲突均拒绝。
- 持久化只保存 `sealed_fence_token`，不保存明文 token。

## board vNext conformance

- board 闭合锚：`38474c0dc381c44807a726125d07fe6a9773affb`
- conformance 报告：`76beaf4`，文件
  `docs/v0.1/m5-board-vnext-conformance-draft.md`
- 结论：PASS / conformance CLOSED。
- 覆盖：C1-C6、R1-R3，以及
  board Takeover → M5 durable record → FileStore 重启重放 →
  board 同 request 重放 → ActivateWriter。
- 密封边界：Takeover 首封 envelope 直接复用于 CAS 与幂等账，不存在二次密封吞错窗口。

## 验证账

合并 `38474c0d` 与 M5 实现锚后执行：

```text
go test -race ./internal/mcs/board ./internal/mcs/rotation -count=1
```

结果：两包全部 PASS。

## 诚实边界

- `FileStore` 是单 coordinator 本地 durable boundary；多 coordinator 正确性仍由 board DB CAS 保证。
- M5 不持有 board authority，不自行修改席位或 writer 所有权。
- 发布、生产配置与运行时灰度不在本交付范围，须走 coordinator 发版归口。

## 后续接口

M5 与 board vNext 已完成一次冻结契约 conformance 对拍，无待处理接口阻塞。
