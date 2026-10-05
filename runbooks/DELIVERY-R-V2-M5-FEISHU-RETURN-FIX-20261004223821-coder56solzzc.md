# R-V2-M5-FEISHU-RETURN-FIX-20261004223821 · 实现交件

分支 `coder56solzzc/v2-feishu-return-fix`，基 `bbc4c48b`，终尖随本件提交。VERSION 保持 `0.2.1.100`。

## 实现

1. 严格的 reply-to-origin 信封（`type=agent_route`、`route_source=reply_to_origin`、`kind=reply`）不再被 `no_mirror` 当作偶发镜像跳过，真实进入飞书 Outbound；其他 ack/system/mirror/近似信封继续跳过。
2. 修法甲：回程发送前查询该席位对同一飞书目标是否已有生效的 sync 或 mirror；命中时不发显式回程，记日志“已由同步送达，跳过回程”，由既有同步链唯一投递。实现不删除 sync buffer、不改变一秒汇总，也不依赖回程与 terminal_output 的先后。
3. A5 并入本分支：若 reply-origin 仍命中飞书出口的 skip 分支，出口返回 `reply_origin_feishu_skipped`，上层走已有 visible-unavailable 链，不再打印“R1 已回程”假成功；普通 `no_mirror` 仍静默跳过。

## 测试格与反向

- 真 `routeToFeishu`／真 DB Outbound：飞书来源回程入队恰一条、目标与正文正确。
- 同目标 sync：回程先发生，terminal_output 分别晚 1.5 秒和 2.2 秒，最终仍恰一条 Outbound。
- 同目标 mirror：回程记 skipped、不进入 Outbound。
- 只有其他飞书目标的 sync：不得压掉正常显式回程。
- ack/system/mirror-control 与两个近似 reply 信封仍跳过。
- R2（reply 入站不再自动回）与 hop/dedup 兜底既有格继续通过。

反向实打：

1. 关闭 sync/mirror 订阅查询：1.5 秒与 2.2 秒两格均红（回程先入 Outbound）。
2. 去掉 mirror 查询：mirror 格红（skipped=0、routed=1）。
3. 放宽目标匹配：其他目标格红。
4. A5 恢复 skip 返回 nil：`TestReplyOriginFeishuSkipIsAnError` 红。
5. 撤严格 reply 豁免：真 Outbound 格红。

## 读数

- `HUB_TEST_PG_STRICT=1 go test -p 1 ./internal/m8 -count=1`：OK，81.854s。
- 三道仓闸：全绿；repo-boundary 为建库 5、逃逸 1、hosted 债 39，均与基线持平。
- `git diff --check`：净。
