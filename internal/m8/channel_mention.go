package m8

// 渠道提及的解析(G ② 第二格 G2-a)。
//
// 组长 c0dfcd99 交的 channel_reserved.go 提供了保留字层,但**未接入任何调用点** ——
// 本文件是那一层的接线:让 `@飞书#xxx` 在解析期就与 `@业主名#会话名` 分道。
//
// 🔴 为什么必须在解析期分,而不是在寻址失败后兜底:
//    `@飞书#群号` 与 `@zzc#opus50ts` **结构完全相同**。若等到寻址失败再判,
//    一个恰好叫「飞书」的业主(或一个恰好存在的同名成员)就会把渠道投递吃掉,
//    而且吃掉的方式是"成功投给了某个 Agent",没有任何错误可看。
//
// 🔵 反向同样要钉死:`@zzc#opus50ts` **不得**被当成渠道 ——
//    只验"渠道名被识别"是半条,一个"一律判为渠道"的实现也能通过。

import "strings"

// parseChannelMention 判断一段文本是不是渠道提及。
//
//	@飞书 正文        ⇒ ("feishu", "",     "正文", true)   —— 无 # ⇒ 由调用方按裁定发给业主本人
//	@飞书#oc_x 正文   ⇒ ("feishu", "oc_x", "正文", true)
//	@zzc#opus50ts 文  ⇒ ("", "", "", false)                —— 业主名/会话名,不是渠道
//
// 🔴 切分复用 parseMemberMention,不另写一份:
//
//	两处若各自切分,`@飞书　正文`(全角空格,T-088)这类毛病就要修两遍,
//	而修的人多半只知道其中一处。
func parseChannelMention(text string) (channel, addr, body string, ok bool) {
	head, tail, rest, parsed := parseMemberMention(text)
	if !parsed {
		return "", "", "", false
	}
	name, hit := channelOfMention(head)
	if !hit {
		return "", "", "", false
	}
	return name, strings.TrimSpace(tail), rest, true
}

// IsReservedChannelName 导出给 internal/admin 用 —— 成员(业主名/显示名)的命名入口
// 在另一个包里,而保留字表在 m8。
//
// 🔴 业主定的第三条「保留字不能用于业主名/Agent名的短名」必须在**写入口**拦,
// 不能只在解析期拦:名字一旦落库,歧义就从解析期搬进了数据里,
// 那时再修要动存量数据,代价完全不同。
func IsReservedChannelName(name string) bool {
	return isReservedForNaming(name)
}
