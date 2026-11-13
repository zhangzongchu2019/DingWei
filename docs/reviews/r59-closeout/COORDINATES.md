# Git取证坐标与复跑

仓库：git@git.code.tencent.com:wego-ai/dingwei.git。准备分支：coder56solzzc/r59-closeout；完整tip/tree由正式回执给出。源码及证据仅经Git取件。

准备分支包含全部归档与主规格；r58独立证据不回并产品集成树，检查坐标前需fetch证据分支：

```sh
git fetch git@git.code.tencent.com:wego-ai/dingwei.git refs/heads/coder56solzzc/r59-closeout refs/heads/evidence/coder56solzzc-r58-integration
```

r58证据精确tip：d63bf9d1eb6c3af3a07145cc1b406e3d7d984527；历史rulings本次取证tip：c376eee255ceb6093e08e3e642998444de38be45（补94及第108条）。若要复跑仓库原openspec-tasks-reconcile/check_coords.py，干净克隆还须读取其旧TABLE.md所列历史坐标；本机首跑缺失的七个commit可按以下命令补取，不修改旧表：

```sh
git fetch git@git.code.tencent.com:wego-ai/dingwei.git \
  2965ace9d9d5935dae78df0a5bf58ba5222e7804 \
  43b4d20d1268e6974dde6edc2508ed9a9d8edce5 \
  559c8fcc9e98e6562a99eda5d072f5c9774de35d \
  852ae658dcb6bc1a7ddfbc3b9a5cba61e89f823b \
  8fc3e22d8981483192d0d963632618e1c862470f \
  f0801adbdc9946a803067610d0d106b0e0e61ce2 \
  fdcf455a6ebf2a0139f98ad9386e67dbd86adb93
```

本次归档check_coords.py与旧脚本分别读取各自TABLE.md，均只用git cat-file，不联网也不改Git对象；前者还核10项任务覆盖与零未勾。

```sh
python3 docs/reviews/r59-closeout/check_coords.py
python3 docs/reviews/openspec-tasks-reconcile/check_coords.py
OPENSPEC_TELEMETRY=0 npx --yes @fission-ai/openspec@1.16.0 validate --all --strict --no-interactive
```
