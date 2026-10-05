# R-V2-M5-FEISHU-RETURN-FIX-20261004223821 · 实现交件

分支 `coder56solzzc/v2-feishu-return-fix`，基 `bbc4c48b`，终尖随本件提交。VERSION 保持 `0.2.1.100`。

## 实现

1. 严格的 reply-to-origin 信封（`type=agent_route`、`route_source=reply_to_origin`、`kind=reply`）不再被 `no_mirror` 当作偶发镜像跳过，真实进入飞书 Outbound；其他 ack/system/mirror/近似信封继续跳过。
2. 修法甲：显式回程成功后，只清掉同席位、同飞书目标尚在一秒缓冲窗内的 sync buffer；sync 订阅本身、其他飞书目标及后续回合不动。回程失败时不清 buffer，保留 sync 兜底。
3. A5 并入本分支：若 reply-origin 仍命中飞书出口的 skip 分支，出口返回 `reply_origin_feishu_skipped`，上层走已有 visible-unavailable 链，不再打印“R1 已回程”假成功；普通 `no_mirror` 仍静默跳过。

## 测试格与反向

- 真 `routeToFeishu`／真 DB Outbound：飞书来源回程入队恰一条、目标与正文正确。
- 同一席位同时开 mirror＋sync：显式回程后等待超过缓冲窗，Outbound 仍恰一条。
- 同席位两个 sync 目标：只消掉与 origin 相同的目标，另一目标 buffer 保留。
- 显式回程失败：同目标 sync buffer 保留。
- ack/system/mirror-control 与两个近似 reply 信封仍跳过。
- R2（reply 入站不再自动回）与 hop/dedup 兜底既有格继续通过。

反向实打：

1. 去掉成功后的 suppress 调用：mirror＋sync 格红，实得两条 Outbound。
2. 去掉目标匹配、粗暴清同席全部 buffer：其他目标格红（0，期望 1）。
3. 把 suppress 提前到 route 成功前：失败保留格红（buffer 0，期望 1）。
4. A5 恢复 skip 返回 nil：`TestReplyOriginFeishuSkipIsAnError` 红。
5. 撤严格 reply 豁免：真 Outbound 格红。

## 读数

- `HUB_TEST_PG_STRICT=1 go test -p 1 ./internal/m8 -count=1`：OK，75.967s。
- 三道仓闸：全绿；repo-boundary 为建库 5、逃逸 1、hosted 债 39，均与基线持平。
- `git diff --check`：净。

