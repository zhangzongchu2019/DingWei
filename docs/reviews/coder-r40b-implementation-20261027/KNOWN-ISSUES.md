# r40b 验证中的既有问题登记

范围单 04e6b6bf，设计 7af11ba9，计划 153e366e。仅登记，不出范围外修法。

## 旧 View 浏览器夹具

2026-10-27T05:48Z，同一安装的 Chrome、Playwright 环境，分别运行 B6 HTML 与 `43eca4f` 父链中的未改 HTML；断言与夹具未改。对默认 Chromium 不在本机缓存的旧套件，用测试进程 preloader 仅把 `chromium.launch` 的 channel 指向已有 Chrome；未替换浏览器或断言。

- `renderer-c2-regression.js`：两份 HTML 均有同形的 7 项硬失败：C2.1d-v2、C2.1e、C2.3 F1–F4、C2.2f。四项既有 T-166 known-defect 也同形。F1–F4 期待已不请求的 `/tasks`；其余为夹具等待超时。这里仅记录观察，不扩展诊断或修复。
- `renderer-canary-g2-regression.js`：两份 HTML 均在 `expected at least six real status refreshes, got 1` 失败。
- 新的 `delivery-ack-v2-browser-regression.js` 覆盖 v2 等待、单调时钟、原位终态、legacy 和 desktop 语义；与上述既有失败分开报告，不以新门通过抵消旧套件失败。

原始本机日志保存在任务 evidence 目录，最终交付附 SHA-256 与实际退出码。该目录不经对象存储交换。

## 追加闭合验证中的 Codex 第二回合超时

2026-10-27T10:24Z起，候选59b5479干净完整门与单格复跑在B接纳判据3超时；修复前锚7ab9460本轮对照通过。未定因，不并入本次Claude修法；当前完整门rc1，详见 [CLAUDE-ARGV-CLOSURE.md](CLAUDE-ARGV-CLOSURE.md)。
