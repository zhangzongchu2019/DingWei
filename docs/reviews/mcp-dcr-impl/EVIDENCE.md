# Evidence reproduction

1. 在仓库根运行 `python3 docs/reviews/mcp-dcr-impl/VERIFY.py`，以及 `sha256sum -c docs/reviews/mcp-dcr-impl/SHA256SUMS`。VERIFY只读，不运行产品、不访问服务、不修改Git。
2. 正式门固定产品/测试源5bc0280d7d6d8da8402ed6459766ce9ae1e0e0f4；`product-manifest.json`绑定非文档路径blob SHA。所有在本件之后的交件提交只允许docs/openspec差异。
3. `evidence/RAW-MANIFEST.json`逐件列原始字节数/SHA。日志按原字节切为最多512KiB的片，再各自gzip；按gzip_parts顺序解压后连接即可逐字重建原日志。每个gzip片另有SHA，压缩mtime固定0，未删行或拼接不同运行。
4. `frozen-env.json`仅白名单测试环境（无真实凭据或SH/DINGWEI继承）；隔离HOME/TMPDIR、go1.22.2、锁定Python3.12.3、PG strict，命令与UTC/rc/SHA逐门记录。PG18.3证据在DCR测试日志。
5. 初次go三遍rc1原样保留；基线对照、Z3全新五轮独立列出，不能把通过的补证替换或改写原日志。已知三格允许名单和至少三轮全绿要求见REPORT；任何新失败不自动扩名单。
6. `mutations/`是最终41刀；`development-mutations-72eeedcac/`是先前候选的14红1存活，仅历史披露、不相加。每刀JSON包含完整old/new串与occurrence、diff、原SHA/变异SHA/还原SHA、编译/业务红/还原绿命令和UTC；可在独立detached源上重做，勿对评审分支原树直接试刀。
7. `old-build.result.json`只记录本地旧程序测试二进制SHA与构建命令，二进制未入Git、未上传。若复跑旧进程格，请从4a349ee5自行构建并以DW_DCR_OLD_BINARY指定；该变量未设则此格会SKIP，作者正式门已设并须PASS。
8. 飞书上游是桩；HTTP/TLS、PG、登录/Issuer/外部席服务是真实本地实现。真实CodeBuddy桌面与真实飞书未测。RAW中不含私有CodeBuddy原文、真实cookie/token、运行席位凭据环境或私有数据库。
