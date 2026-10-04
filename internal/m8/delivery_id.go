package m8

// deliveryIDPrefix 是投递号的**全链唯一形式**。
//
// 契约(改动前后都必须成立,改动只是让①与②收敛到同一串):
//
//	① Hub 生成并写入 env.Meta["delivery_id"],同时作 session_deliveries 主键;
//	② helper 的 unified_queue._delivery_id 以此为内部队列键(self._by_delivery),
//	   取消/中断均按该键寻址;其加前缀逻辑幂等,已带 "dw-" 则原样返回;
//	③ helper 回 delivery_ack 时,delivery_id 取自本信封 meta 原样回填
//	   (sessionhelper/app.py:532),Hub 侧 GetSessionDelivery 精确匹配。
//
// 🔴 不要改成不带 "dw-" 的形式:那会让 ② 重新加前缀,渲染器再次同时见到两种写法,
// 即 T-074「同一条输入画两张卡」的根因。
const deliveryIDPrefix = "dw-dlv_"
