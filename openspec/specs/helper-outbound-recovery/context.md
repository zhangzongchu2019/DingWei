# helper-outbound-recovery 上下文

发布提交 501e5b8a185b20f5c931c8ebb89c16bfd633a01e（r60，VERSION 0.2.3.40），2026-11-18 现网上线后按方案甲归档建立本主规格。结项凭据见 docs/reviews/r60-closeout/RECEIPTS.md。

## 来自 2026-11-17-helper-send-lost

# 最小现状基线

固定helper2e1e4c9d：send_dingwei.py:54–93以本地Unix IPC写请求并等回执，:112–119仅看IPC ok就打印sent；request_id与protocol.py:35–43的随机envelope ID不绑定。app.py:414–450普通handler若协商回执则等2秒；OutboundReceiptError一律unknown/retryable=false；未协商为原始WS直写且accepted_by=local_helper，不是持久确认。outbound_receipt.py:25–31,56–75仅内存pending并在finally清除；:145–156断线abort。普通路径没有本地持久发送正文。

Hub2e：hub.go:4303,6401–6416的envelopeIDs是内存去重，重复返回nil但不重发原queued回执；:4386生成随机deliveryID，:4423后持久入队再发回执；sqlite.go:2134只按delivery主键INSERT OR IGNORE，无普通sender/source的持久唯一提交关系。现状不允许据同source ID重送就承诺幂等。

本变更只提案，以下ADDED为待审目标，未实现，不改变当前产品行为或线上协议。
