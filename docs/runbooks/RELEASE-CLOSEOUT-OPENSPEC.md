# 每版现网结项后的 openSpec 与主干步骤（方案甲）

依据：review/opus50tc2k/rulings〔53〕-补1（业主原则同意甲）、〔52〕-补9（r54 首次执行）。适用于本仓（开源档）。企业项目的晋级策略另定（待业主确认「满一周」口径）。

执行者：组长席。时机：现网结项件复核通过、写入 rulings 之后。

1. 清账：对发布提交里 openspec/changes 下所有未归档变更，逐条核 tasks.md。已完成的任务要勾上，并注明可 git cat-file 取到的证据坐标；没完成的写交接。坐标须机器核对 0 缺（参考 docs/reviews/openspec-tasks-reconcile/check_coords.py）。
2. 归档：在发布提交之上，对全部任务已勾的变更执行 openspec archive -y（无规格增量的加 --skip-specs）。新建的主 spec 要把归档工具留下的 Purpose 占位句改写成实际用途，并补 context.md。
3. 门：openspec validate --all --strict 0 失败；读 openspec 目录的包（cmd/workpulse、internal/changeflow、coordinatereview、specconfirm、specread、taskdispatch、store）和 internal/m8 全包 rc0。门绿后才推送。
4. 主干：确认 main 是归档提交的祖先，然后不带 force 推送「归档提交:refs/heads/main」，用 ls-remote 核对。非快进即停报，不得 force。
5. 镜像：运行 GitHub 单向同步（工蜂 → dingwei-dev），须报 SYNC_OK，镜像上的 main 与工蜂一致。
6. 下一版集成分支并入新 main，保证下一次仍可快进。
7. 在 rulings 写一条：发布提交、归档提交、归档名单、main 新旧值、镜像同步时刻。

不做：不归档仍有未勾任务的变更；不改发布提交本身；不把归档当成发布或验收的替代。
