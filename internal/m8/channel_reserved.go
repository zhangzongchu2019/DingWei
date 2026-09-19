package m8

// 渠道保留字(G / 0.2.1.0,业主 2026-09-19 裁定)。
//
// 业主原话:
//   「渠道名必须是保留字:飞书/feishu/fs/企微/wecom/slack/google…
//     保留字还应该包含:业主名(对同一业主名下有效)
//     保留字不能用于业主名/Agent名的短名」
//
// 🔴 为什么必须有这一层:现网 `@X#Y` 由 parseMemberMention(hub.go:5150) 无条件切成
// (业主名, 会话名)。渠道语法 `@飞书#账号` 与它**结构完全相同** ——
// 若渠道名与业主名/短名取值空间重叠,同一串文本会有两种合法解读,
// 而解析器没有任何依据选一种。**保留字是这个歧义的唯一消解方式,不是防御性编程。**
//
// 🔵 大小写与全角:渠道名一律按 ToLower 后比较;
// 全角字符不做归一(全角「＠」本来就不触发路由,不构成绕过)。

import "strings"

// channelReservedNames 是渠道名保留字。**新增渠道必须同时在这里登记**,
// 否则 `@新渠道#账号` 会被当成 `@业主名#会话名` 投给某个 Agent。
var channelReservedNames = map[string]string{
	"飞书": "feishu", "feishu": "feishu", "fs": "feishu", "lark": "feishu",
	"企微": "wecom", "wecom": "wecom", "wechatwork": "wecom", "wxwork": "wecom",
	"slack":  "slack",
	"google": "google", "gchat": "google",
}

// channelOfMention 判断 `@X#Y` 里的 X 是不是渠道名。
// 返回规范化渠道标识(feishu/wecom/slack/google)与是否命中。
func channelOfMention(head string) (string, bool) {
	c, ok := channelReservedNames[strings.ToLower(strings.TrimSpace(head))]
	return c, ok
}

// isReservedForNaming 回答「这个名字能不能用作业主名 / Agent 短名」。
// 🔴 业主定的第三条:保留字不能用于业主名/Agent名的短名。
// 注册/改名入口必须调用它;**不调用的话保留字层等于没有** ——
// 渠道名一旦被某个 Agent 占为短名,歧义就从解析期搬到了数据里,更难修。
func isReservedForNaming(name string) bool {
	_, ok := channelOfMention(name)
	return ok
}

// isOwnerScopedReserved 回答「在业主 ownerName 名下,name 是不是保留字」。
// 🔴 业主定的第二条:「保留字还应该包含业主名(对同一业主名下有效)」——
// 即该业主名下的 Agent 短名不得与业主名相同,否则 `@业主名#业主名` 无从分辨。
// 注意这一条是**业主域内**的,不同业主之间互不影响。
func isOwnerScopedReserved(ownerName, name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return n != "" && n == strings.ToLower(strings.TrimSpace(ownerName))
}
