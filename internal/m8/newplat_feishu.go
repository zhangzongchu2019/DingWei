package m8

import (
	"context"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// 新平台 N3 的飞书指令（spec N3 第二节 4、5；设计件 3.3）：
//
//	登录：沿用旧登录指令的动词，码换成新平台的 8 位登录码
//	注册：newPlatformRegisterVerb ＋ 8 位注册码
//
// 身份一律取自信道给出的发信人（SenderOpenID＋BotChannelID），不取正文里任何自称 —— 与旧登录的硬门③相同。
const newPlatformRegisterVerb = "#register"

// AgentNameAllowed 供 identity.Service.NameAllowed：渠道保留字不得用作 Agent 名（审批时就拦）。
func AgentNameAllowed(name string) bool { return !isReservedForNaming(name) }

func parseNewPlatformCommand(text string) (verb, code string, ok bool) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) != 2 || (fields[0] != consoleLoginVerb && fields[0] != newPlatformRegisterVerb) {
		return "", "", false
	}
	c := identity.NormalizeCode(fields[1])
	if len(c) != 8 {
		return "", "", false
	}
	for _, r := range c {
		if !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return "", "", false
		}
	}
	return fields[0], c, true
}

func (h *Hub) dispatchNewPlatformCommand(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, bool, error) {
	verb, code, ok := parseNewPlatformCommand(text)
	if !ok {
		return model.PrefixDispatchResult{}, false, nil
	}
	sender := identity.Identity{Provider: "feishu", Issuer: strings.TrimSpace(msg.BotChannelID), Subject: strings.TrimSpace(msg.SenderOpenID)}
	var reply string
	if verb == newPlatformRegisterVerb {
		reply = h.Onboarding.HandleRegister(ctx, sender, code)
	} else if sender.Subject == "" {
		reply = "无法确认发送者身份，登录被拒绝。"
	} else {
		reply = h.Onboarding.HandleLogin(ctx, sender, code)
	}
	return model.PrefixDispatchResult{Matched: true, Reply: reply}, true, nil
}
