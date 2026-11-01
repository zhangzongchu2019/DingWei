# S-R5-CRED-SYSTEMD 交件证据

执行者 coder56solzzc.zzc；2026-11-01 UTC；待独立复核。仓库 git@git.code.tencent.com:wego-ai/dingwei.git，分支 opus50tc2k/r5-cred-systemd，基于 r46 2af469dd6919d8eb6064aa7da0e646f0851becf8。代码/测试提交 85beea907d7511206d08bb7c33a1fd22139189f9，tree a087b9b9a573547a133c53ec7f1902070c25a925；后续提交只补本文及设计/context/交接记录，最终交件 tip/tree 以交件消息和远端坐标为准。

## 修复与边界

派单所引 prodops 18:10:11Z 事实：systemd ImportCredential 后 root:root 0440、目录 root:root 0550，ACL 允许 UID 991，实际可读 93B，CREDENTIALS_DIRECTORY 指向父目录；本席没有读取 PAT。原 0077 检查把 ACL mask 显示的组位作为拒绝条件。

产品仅改 internal/specread/github.go 的 credential() 判定及 syscall 导入：绝对 CREDENTIALS_DIRECTORY 与清理后的文件父目录完全相等才使用 systemd 模型。Lstat 拒绝文件/父目录 symlink；文件普通、属主 0/euid、其他人权限零；目录属主 0/euid、无组/其他人写位。打开的文件描述符与 Lstat 身份相同才继续，无法获取属主即拒绝。组位不再作为该分支拒绝条件，不读取 ACL。其它路径沿用原 0077 规则。大小/内容、逐次重新读取、提供方调用、逐主体授权、开关、工蜂失败关闭均不变。

只在普通用户 UID 1001、临时目录与合成 93B 下测 mode 分支；未模拟真实 ACL、未冒称已跑 root:root / UID 991 的 systemd 复测。本单未部署、重启、启用功能或改任何单元；修复后预发复测候组长另令。

## 正反矩阵

入口 internal/specread/github_systemd_test.go，TestGitHubSystemdCredentialBoundary 的 20 格全部通过。负格同时断言 ErrUnavailable、Ready=false、空内容、提供方网络调用为零。

|必测格|实际|反向控制|
|---|---|---|
|精确目录内文件 0440、目录 0550|接受|restore-0077 断言红|
|同权限文件放目录外|拒绝|outside-scope 断言红|
|目录内文件 0444|拒绝|file-other-bits 断言红|
|目录内 symlink|拒绝|file-symlink 断言红|
|父目录其他人可写 0702|拒绝|parent-other-write 断言红|
|环境指向另一有效目录|按旧规则拒绝 0440|environment-mismatch 断言红|

额外：父目录组可写 0720 拒绝；环境未设/相对/带尾斜杠不扩大范围；文件路径 Clean 后父目录匹配接受；目录内外 0600 接受；目录内无 other 位的组 mask 接受；父目录 symlink、非普通文件、过短、空白、4097B 拒绝；单个末尾换行照旧接受。轮换用例验证 0440→0444→重建0440→删除，Ready 每次重核，无缓存残留。

目标变异命令：`python3 tests/r5-cred-systemd/mutations.py`（普通用户；Go overlay，不改工作树）；13/13 在各自目标断言上红，runner rc0，原始结果 mutations.json。恢复仅看 0077 的变异令 directory_0440 正格失败；其它覆盖目录范围、文件 other 位、symlink、父目录 other/组写、环境错配、严格回退、旧私有文件、父目录 symlink、大小和内容规则。编译失败不计命中。

## 验证与复跑

所有测试使用环境白名单，剥除继承 SH_*/DINGWEI_*/DW_*/WP_*/FCS_* 和 CREDENTIALS_DIRECTORY，临时 HOME，loopback/测试数据库；Go /usr/lib/go-1.22/bin/go、Python /data/ai-dev-data/helper-deps-lock-venv/bin/python3。白名单只含 HOME、PATH、GOCACHE、GOMODCACHE、GOPATH、LANG=C.UTF-8、TZ=UTC、HUB_TEST_PG_STRICT=1。缓存路径 /home/dev/.cache/go-build 与 /home/dev/go/pkg/mod，GOPATH=/home/dev/go；PATH=/data/ai-dev-data/helper-deps-lock-venv/bin:/usr/lib/go-1.22/bin:/usr/local/bin:/usr/bin:/bin。可用 env -i 和 mktemp -d 的 HOME 复现下列命令。

|命令|读数|本地原始日志|
|---|---|---|
|go test -json ./internal/specread ./internal/m8 -count=1 -timeout=15m，PG strict|rc0；specread 96 pass，m8 1206 pass / 36 skip / 0 fail，173.481s|/tmp/r5-cred-full.log|
|go test -json ./internal/specread -count=1，最终测试文件命名/内容复核，PG strict|rc0；96 pass / 0 skip / 0 fail|/tmp/r5-cred-specread-final.log|
|python3 tests/r5-cred-systemd/mutations.py|rc0；13 目标断言红|/tmp/r5-cred-mutations.log 与 /tmp/r5-cred-mutations/|

计数含子场景；m8 36 项 skip 名称与原条件逐一列在 SKIPS.md，没有 PostgreSQL 缺依赖 skip。原始日志本地保留，仅在 Git 提交非敏感结果摘录；真实 ACL/UID 991 环境仍由 prodops 另行复测。

最终构建门：`bash scripts/run-build-tests.sh` rc0，helper inventory 90，Claude/Codex 各 1092/1092（1083+9），Python 文件 27、shell 门 2，末尾 BUILD_TESTS_OK；日志 /tmp/r5-cred-gate.log，摘要与 SHA256 见 gate-results.json。OpenSpec 1.16.0（遥测关闭）`openspec validate --all --strict --no-interactive` rc0，12/12；日志 /tmp/r5-cred-openspec.log。git diff --check rc0。没有新增浏览器脚本或迁移。
