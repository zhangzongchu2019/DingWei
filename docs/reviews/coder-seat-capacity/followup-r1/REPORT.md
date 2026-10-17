# B R1 返修：请求维度贯通

基于精确 `7a600e52b272e816dfde9cda333d293704c4660c` 追加；正式审查 `8511dcec27a9fb8962bbde1c04a953d6dcc83d66` 仅 R1。产品钉 `325736fe312bb59358d7d842929d285132789cd7`，tree `80b4ae49550bb1abf7d32e07b50fb229a92ccfdd`；最后测试钉 `96aad97ab91927a697daaa2ae03370eeab9498ff`，tree `b1443f79bb97cda006b6a11e9cff3785719d3056`，仍是 review/coder/seat-capacity 的 r22 基线，不自行重基/合入 r23。

## 修复合同

`NodeCapacityError` 的 Error 恒为旧 `node_no_capacity`，Unwrap 支持原 errors.Is。私有 reasons 只通过构造和只读复制 accessor 投影：cpu/memory/disk/seat_limit/load_unknown，固定顺序、去重；未知字段/坏策略折为 load_unknown，不携带节点 ID、路径、额度数值、原错误或模型名。

- pickNode 按原 nodeID/在线/pool/soft同意/allow_soft 边界过滤后，先检查模型支持，再做 B 请求套餐的 CanAdd。不能把已有支持模型误报缺失。真正无匹配模型保持旧 model_not_available；无 model 请求正常用容量码。
- 自动选择时，仅汇总符合上述边界且匹配请求模型的 soft 候选的拒绝原因并集。每个原因意为至少一个这样的候选因此拒绝，不表示所有节点同维度满；有可用候选即正常返回，不带失败集合。hard 不新增原因承诺或 B 行为。
- CreateSeat 节点行锁内复查保留实际 CanAdd 原因；并发第九个输家可指出 CPU/盘不足，不退回通用哨兵。start 保留 CanStart 原因，缺席位预留或无效 B 是 unknown；真实原 soft hold 分支不改。
- writeHostedError 对 typed 错误仍给 HTTP409/error=node_no_capacity，额外 capacity_reasons；旧通用哨兵不凭空增加字段。Console 在原错误入口只读取白名单映射的固定中文文案，未知输入和任意 message 不显示。
- 生命周期 HTTP 入队前 preflight 与任务入队后再次启动检查均覆盖。后者在现有 durable reason TEXT 中写固定 `node_no_capacity:cpu,disk` 形式，不写上游文本；job 读取投影回原 reason=node_no_capacity 加 optional capacity_reasons。只新增可选 JSON 字段，不改表或队列状态机。Console 操作轮询复用固定映射，旧客户端仍看通用 reason。

## 测试内容

新 hosted 测试：safe typed/wrap/复制/非法原因；真实 mkOrch+PG 的请求四核套餐，CPU/内存/磁盘各缺而最小席尚有余量；有/无/不支持 model、soft consent、自动候选原因并集；实际 Store 席位缩容后的 start 三维/unknown，拒绝不改变状态。原并发 8收1拒测试只新增输家 cpu+disk 原因断言，原数量/事务断言未放宽。

磁盘例按合法配置把最小预留设为 8GiB，净 12GiB：minimum 尚可放一席，现有 4c8g 的实际 16GiB 请求被拒。默认 minimum40GiB 大于现有4c8g实际16GiB，不能伪称默认配置下还存在“盘请求大于minimum”的套餐；本例不改现有套餐或默认最小值。

实际 workpulse HTTP+PG：CPU2/内存6GiB/磁盘12GiB 三例，options仍有最小席容量、reasons=[]，POST4c8g得到对应原因；支持的 gpt-review 不误报，missing-model 真报422；unknown/stale 给 load_unknown；全部拒绝无身份签发/网关命令。实际 restart HTTP 同样显示三维/unknown、保持online、不发命令；healthy时成功入队、执行前变cpu/unknown时，RunJob后实际job HTTP仍得到安全原因，非属主403。

浏览器保留全部原 soft 风险/同意/roster/容量断言，新增 CPU/内存/盘/席位上限/unknown/多维去重/恶意字段7组，以及操作job投影。不是实际用户或 Docker 验收。

## 精确读数

最终结果、撤修、恢复和清理读数见下方及相邻 raw。这是作者返修证据，待 opus50ts 精确独立复审；前件已独立确认的 calculator6/nodeagent149及原7个PG顶层结论不重写为本次全包读数。内部 nodeagent/计算器/采样/资源配置字节未改，无需重跑无关长门。

没有宿主/真实Docker/外部PG/密钥/真实Agent/构建产物/部署；仅本地临时PG、伪Engine/HTTP/浏览器。r23 原本地组成证据仍只属于旧源码 pin，不改标成新完整合并验收。最终集成需主审核 policy并集、A1/N2/revision，四席冻结不变。

## 最终读数与撤修归属

- 产品精确 325736f 完整 canonical rc0，166.216s，python48＋shell2；`TestDeviceClaimAcrossActualClientAndPostgres` parent 和五个子格全 PASS。
- 同钉定向 race：hosted 15 个顶层、workpulse 4 个顶层 rc0、无 skip，46.442s；含原生命周期持久任务/恢复/顺序、实际请求三维/未知/型号、并发输家、HTTP create/restart/job、旧契约等。不把这条定向命令称为 hosted/workpulse 全包。
- 最后 96aad97 只将浏览器“等期待文案”改为“等提交按钮恢复→断言文案”，产品零差异。原等待在 Console 撤修时是正确的超时红，但为提供明确断言红新增此测试修正，不是候选产品缺陷；initial 记录保留。
- 325736f 浏览器原21基线＋7原因＋1job投影 rc0，4.066s；96aad97 恢复后同套 rc0，5.181s。产品、Go测试与 canonical 输入没有变化，不复跑无关长门，也不将上一行 canonical 重标为96的一轮新运行。
- 9刀 source撤修：selection、model-order、transaction、start、HTTP、job-write、job-read、allowlist、Console。前八属于325736f，Console明确断言红属于96aad97；aggregate每项独立source/log可核，均rc1断言红而非编译错误。Console源变异与第一次相同，只改变测试完成边界。
- 96aad97 恢复源码定向race（模型/三维、并发输家、真实HTTP/job）rc0，9.847s；候选工作树从未被撤修，变异均为私有副本。raw保留最初Console超时、两轮聚合/源码mutation metadata，执行原脚本也保留，未声称原脚本自动包含后加的明确断言轮。

证据键均仓库相对路径；原始终端空白不改，diffcheck以产品/文档正文为准。原7a证据目录与manifest保持不变，本目录单独封签。旧源码门读数不冒充本次独立审查；请按精确后继审R1增量。

收尾：检查本席夹具进程和临时PG pid marker均0，移除私有变异树/cache/HOME/TMP共 1686033219 bytes；Go cache先clean至 232 bytes再删除。仅保留源码工作树及证据，共享module cache与锁定venv未动。见raw/reason-cleanup.json。
