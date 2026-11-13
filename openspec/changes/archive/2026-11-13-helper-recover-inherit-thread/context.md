# 实现上下文
基线 coder56solzzc/r58-integration 3c9f2722fd5b2e05b52188a8ccb664427f5debdd；分支 coder56solzzc/helper-recover-inherit-thread。
唯一写者 01a1ceef-693f-7b22-97c4-bf4d5017ccd3；原子锁 /tmp/coder56-helper-recover-inherit-thread.lock。
读取 helper-delivery-evidence/context.md、spec.md、helper-bound-thread 进行中变更；现有能力沿时间线 r56.1→r58 绑定/健康等待门。本基线未发现独立全局平台能力时间线文件，不推测未入库规则。
实现 tools/sessionhelper/sessionhelper/{driver_adapter,codexDriver,sub_agent_runtime,app_server_broker,private_endpoint}.py；复用现有 stale-thread/sub-agent 测试模块，不新建 inventory 条目。
本机普通用户，锁定 venv /tmp/coder-0232-locked-venv/bin；测试 env -i、临时 HOME、短 TMPDIR，不连接运行实例、不访问真实账号/数据库。两种 PATH 全量 run-build-tests；OpenSpec 1.16.0 strict 与语义变异。证据 docs/reviews/helper-recover-inherit-thread。
