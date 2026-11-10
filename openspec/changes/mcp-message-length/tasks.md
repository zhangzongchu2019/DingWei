## 1. 实现
- [x] T-01 统一MCP/extseat UTF-8业务和HTTP JSON预算。
- [x] T-02 Hub投影和外部路由最终长度及错误详情。
- [x] T-03 helper IPC、body-only及send.py --file同步，独立提交。

## 2. 验证
- [x] T-04 三档×三编码×三路径与三项变异。
- [x] T-05 受影响全包、m8 -p1、canonical双profile、OpenSpec strict。

## 交接记录

- 2026-11-10，coder56solzzc，基线04cfb018：三条件接管检查通过，旧锁留证；进行MCP侧实现。完成：需求/预算固定；阻塞：无；下一步：逐侧提交推送、完整门与变异，不部署。

- 2026-11-10，coder56solzzc，源码afe7ba13b：完成三侧提交、27路边界格、4刀定向判红、三包/m8/canonical/strict；原始失败及修正保留于docs/reviews/mcp-p1-len。阻塞：无开发门阻塞，独立验收未进行；下一步：证据提交推送并核ls-remote，再以交件tip为SENDWAIT基线；无部署。
