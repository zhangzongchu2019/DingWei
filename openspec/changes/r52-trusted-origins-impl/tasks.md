# 任务

- [x] T-01 按 ㊸-补4 与实现派单核实唯一写者和文件地址范围；从 d555768c 建立独立 worktree。
- [x] T-02 仅让 Hub 上传票据按精确请求 Origin 选择 upload_url，保持文件服务响应与入库地址为规范来源。
- [x] T-03 注入规范来源，调整 renderer 自有文件识别、显示映射和发送前规范化，并同步 md-render-core 生成副本。
- [x] T-04 增加 Hub、View 与浏览器 443/50443 流程回归及指定变异检查。
- [x] T-05 完成浏览器回归套件、构建门及 m8 五片串行 race；将每片用例数与 rc 记录到 EVIDENCE.md。
- [x] T-06 自审变更与规格来源，提交独立实现和证据，回报 Git 坐标。

## 交接记录

- 2026-11-06，执行者 `togpt60lunazzc`，基线 `d555768c102b04caac3a1f05afcff4a55832cc13`。
  - 完成：Hub 票据来源选择、View 规范来源注入、renderer 文件链接转换、Go/Python/浏览器 targeted 回归及两项预期失败变异；renderer 六段副本同步一致。
  - 完成：m8 五片 `-race -count=3` 串行门全部 rc=0；浏览器回归及最终构建门结果记录于根 `EVIDENCE.md`。
  - 阻塞：无。
  - 完成：最终审阅通过；实现与证据已提交到获准分支并核验推送坐标。

- [x] T-07 修正 renderer 的 trusted-origin 常量重名，合入 R51 epoch replay 修正，并在最终 tip 完成全量浏览器回归与非强制推送。 【甲；证据：docs/reviews/r52-trusted-origins-impl/raw/browser-receipt-at-9da5b75f.txt @ 425f6452e757d4474c0ac46b18d68197d878936c】

## 交接记录（2026-11-06 补充）

- 2026-11-06，执行者 `togpt60lunazzc`，最终提交号见本记录提交后的 `git rev-parse HEAD`。
  - 完成：已修正 renderer 同作用域重名；同步 md-render-core 与 embedded SHA；定向回归和构建门通过；已合入 R51 replay fix。
  - 阻塞：无。
  - 下一步：在本补充提交的最终后续 tip 重跑完整 browser regression，记录逐套件 rc 与 SHA，核验并非强制推送分支。

- [x] T-08 修正 PDF/audio 使用不存在的展示 URL helper，复跑 0206 套件与完整 35 套件浏览器门禁。

## 交接记录（2026-11-06 最终实现验证）

- 2026-11-06，执行者 `togpt60lunazzc`，实现提交 `9da5b75f410e4e2456944836bfc27d18bd163d57`。
  - 完成：重名修复、md-render-core 生成副本同步、PDF/audio helper 修正；build gate 通过；独立 0206 回归 41/41；完整浏览器套件 35/35 rc=0。原始逐套件收据归档于 `docs/reviews/r52-trusted-origins-impl/raw/browser-receipt-at-9da5b75f.txt`。
  - 阻塞：无。
  - 下一步：推送更新后的分支，并在证据提交形成的最终分支 tip 上重跑完整浏览器套件。
