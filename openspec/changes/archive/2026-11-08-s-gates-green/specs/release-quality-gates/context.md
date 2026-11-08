# 发布质量闸上下文

基线0fe09b9e；三闸位于scripts/repo-boundary-gate.sh、dependency-ratchet.sh、tenant-isolation-gate.sh，统一入口scripts/run-build-tests.sh。构建使用Go1.22.2及锁依赖Python venv，清除继承的helper/Hub环境。源码经私有工蜂仓库wego-ai/dingwei交换，分支coder56solzzc/gates-green；单写线程由s-gates-green.writer.lock记录。无部署授权。
