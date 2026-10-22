# 设计输入与读数限域

设计基线：wego-ai/dingwei，`review/coder/daemon-identity`，ce58c1bfd0a49e3d12161df6b1dffcea38cfb945，tree e8c0593c05b7dfb633b79b6752c49ed55846977e；组长已PASS。本设计分支仅追加docs，实施须对齐r37正式锚，不修改正在集成的258267c7。

此前真实两版双listener、同HOME／CODEX_HOME隔离读数见基线 `docs/reviews/coder-daemon-identity-rework-20261022/raw/private-listeners/RESULT.json` 与原始日志；只证显式Unix listener并存与initialize，不证人类TUI／活turn／owner进程生命周期。当前一页中的endpoint-owner、控制通道与lease均为待审设计，不是现有Codex协议，也未实现。

本次仅以同批已核官方二进制读取 --help（不用PATH），0.161.0与0.163.0各三条，六条rc0；环境白名单仅私有HOME／CODEX_HOME、固定PATH和关闭遥测，未启动app-server／helper、未调用模型、未接生产。binary SHA逐项匹配基线官方provenance：

- 0.161.0：9a820c17865fa825d04db416818679a9d63bd72e50835c396f496e5684626c9c。
- 0.163.0：399ff69c431c066e40de318fba45c2b07f45a76b19939d247ee4828fc70c4215。

静态确认：CLI与resume帮助均有 --remote，接受unix://PATH；proxy有 --sock SOCKET_PATH。设计中的完整PTY组合命令仍须实测，不将单独help当交互通过。daemon start路径支持范围仍以前件正确子命令的help为准。

**前件措辞订正：** ce58c1bf报告第2节“proxy --help也没有该选项”若按“不提供socket路径”理解，过宽且不正确：两版已存原始help均有 --sock；准确说法是proxy可显式选socket，daemon start帮助未列独立路径参数。原始证据与历史提交不改写；不影响前件已核Unix listener可行性和身份门结论。此次显式记录这处订正。

设计范围检查：相对ce58c1bf仅docs；无产品源码／配置／版本／锁变化。git diff --check与原始help／SHA清单核验完成后交审；未跑新产品全门（本件没有产品改动）。
