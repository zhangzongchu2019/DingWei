## Context

基线 main 04cfb018。MCP 为256KiB，HTTP JSON为1MiB，helper普通IPC为64KiB，最终抬头原未计入正文预算。

## Decisions

- MessageBodyMaxBytes=1048576；Go len(string)按 UTF-8 字节计，Python以 encode(utf-8)计。
- MessageJSONMaxBytes=6×1048576+65536=6356992。一个原字节最坏可写为六字节 Unicode 转义；中文和 emoji 的单位原字节转义比更低，元数据预算65536。不会把整个上限误用为可接受业务正文。
- extseat受理先按已锁定属主与目标生成同格式抬头计入预算；投影再次校验最终正文。回程正文原样计数，无重复抬头。
- body_too_large包含actual_bytes和limit_bytes；受理前拒绝，不截断，不生成发送键/队列行/TouchGroup。
- helper普通和body-only共享JSON行预算，但业务正文另验1MiB；spec正文2MiB限制不改。

## Compatibility

旧helper连新Hub仍保留旧本地上限，超过旧限会本地拒绝，不能声称支持1MiB；新helper连旧Hub可能在旧MCP入口256KiB或旧HTTP1MiB预算处拒绝，原extseat1MiB正文限制也不含新抬头预算。新helper不会替旧Hub截断或重发；完整1MiB能力需要相应两端升级。Hub WS帧16MiB不变；helper连接显式使用同一16MiB接收预算，避免websockets库缺省1MiB连同JSON外壳拒绝合法正文。
