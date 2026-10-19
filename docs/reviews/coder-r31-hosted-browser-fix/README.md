# R31 hosted 浏览器两项既有红：测试修复

基准是组长唯一受审锚4a6521b3455e4fb71f0dd06cd5f0706737ff8564；另开 review/coder/r31-hosted-browser-fix 交件，不移动原受审分支。这里只改一个浏览器测试文件和证据文档，Hub/Console产品代码均与4a逐字节相同。

第一项根因是过期URL断言：刷新协议提交6a30dc35afa90836113f624ec75d5428cfcf9b8f已将每个iframe的view_refresh_instance加入URL，干净r30基线3d99db2b同样如此。原测试仍要求URL恰为/view/leader.zzc。现改为严格校验同源、完整路径、唯一view_refresh_instance参数、32位十六进制nonce及其与iframe dataset相等，不弱化full_name约束。

第二项是上述失败造成的共享页面状态级联：原测试在旧URL断言红后跳过close tab，Agent view保持active，后面的hostedCli在后台不可见而超时。现在用finally关闭本格可能创建的full/short-name页签并恢复导航模式；建组用例也主动选择其所属平台面板再等待表单。无需改产品。

在精确父4a重新复现30/2；本分支正对照32/0，三个实际页面撤修各31/1且只有指定一格红：nav-opens-by-name（全名路由）、view-refresh-instance-missing（新增nonce检查）、hosted-online-no-group-refresh（上线刷新）。前两项故意让导航格失败后，后续建组格仍通过，证明不再级联；去掉编组刷新时仅编组格红，证明原功能检查保留。撤修只在内存HTML替换，最后正常恢复32/0。

环境与完整命令在CHECKS.json，各日志以Git交付。本机Playwright1.56.1、Chrome151.0.7922.108；未改运行依赖，未使用dev-env，也不冒称在其1.63.0上通过。请审查席在自己的环境独立复跑。没有部署、没有43或真实Hub操作。

父提交原有coder-r31-hosted目录的摘要/源码manifest是4a候选的历史证据，按原锚保留；本后继仅以本目录MANIFEST核增量，不能用父候选浏览器30/2日志冒称本后继结果。
