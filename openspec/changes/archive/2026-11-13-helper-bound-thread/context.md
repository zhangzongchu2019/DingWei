# 实现上下文
基线：coder56solzzc/r57-integration 329eb9f9c45d2d785a0429b01eaa0fa5264fe15d；分支 coder56solzzc/helper-bound-thread。
写者：01a1c782-7090-7071-8d99-6ea51512ac13；原子锁 /tmp/coder56-helper-bound-thread.lock。
根配置与 driver：tools/sessionhelper/sessionhelper/{config,driver_adapter,codexDriver}.py；M2 根重建：sub_agent_runtime.py。不改 app_server_broker.py 或 Hub。
测试复用 test_codex_stale_thread.py、test_sub_agent_helper.py，不新增测试库存文件。操作说明 docs/sessionhelper/CODEX-BOUND-THREAD.md；证据 docs/reviews/helper-bound-thread/REPORT.md。
环境：普通 uid1001，env -i，临时 HOME 与短 TMPDIR=/tmp/hbt782，锁定 venv /tmp/coder-0232-locked-venv/bin，Node /usr/local/bin。仅本地合成 app-server 与临时账本；不访问在线席位数据库或真实推理，不构建发版包、不部署。
