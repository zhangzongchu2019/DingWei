## 集成顺序与边界

基110e2c1f按③899f、④4e421、①81a、②b175合入，产品分别b4c42/4e421/b48a/3db78。唯一冲突装配保留current heartbeat及inbox desktop分支，实际inventory97。版本0.2.3.40；现网renderer来源保留，不复制旧页面版本号。

## 联合恢复

真实helper/outbox/MCP与Hub SQLite/loopback WS，持有真实source写槽使生产2秒截止触发。必须等OutboundReceiptWriteFail增加才查询MCP未知状态，证明已提交但回执未确认。原正文与source留存；释放背压后自动writer按同source重送，Hub返回原delivery，dw_send_status观察恢复。status不负责重发，网络允许幂等重复，Hub创建只一次。删除截止/身份复用须在业务断言红，非整包超时。

## 离线viewer

封口格使用已建立真实viewer，避免startViewerOutput先置gap掩盖被测分支。队列格不启动输出worker，独立写槽先阻塞状态写，使pending字节和停止新显示责任两个防线可分别观察；这是受控组件队列加真实状态WS，不声称覆盖所有旧worker交错。保留readonly离线状态/1000关闭，分别删除四防线须红。

## 完整门

SOURCE_COMMIT固定、env-i、普通用户、独立HOME/短TMPDIR、PGstrict临时库；canonical两profile、build.sh、四树Go、浏览器全量、strict。m8按01:37Z门串行5轮、至少3轮整绿，仅指定两格interrupt例外，全部原始rc/FAIL/SKIP保留；其他失败停报。不自行改SQLite或扩大白名单。组件作者/评审/集成读数不混合。
