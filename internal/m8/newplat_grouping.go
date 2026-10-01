package m8

import (
	"context"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// 新平台的消息可达性（qwen38maxkhovvob4 复验 G3 托管＋PG 层 🟡第七节第 3 条，组长 15:22:39Z ①）：
// 规则与旧平台 enforceAgentGrouping 同一套，只把数据源从旧 agent_group* 表换成 identity 的 np_group／np_group_member。
// 🔴 与旧平台的一处不同：没有「编组表为空 ⇒ 迁移／回滚态放行」。新平台没有迁移态（旧编组数据不迁），一律判；
//   旧表在新平台下恒空，若沿用旧逻辑就是 fail-open（编组管控静默失效）。

// enforceNewPlatformGrouping 发送方与目标须同在至少一个编组；不在任何编组的 Agent 落到属主的默认组（与旧平台同）。
// 豁免：飞书发信人与 view 页（view-v2）的 console 用户映射到【激活】的平台用户。
func (h *Hub) enforceNewPlatformGrouping(ctx context.Context, from address, env model.Envelope, targetKeyID, targetSession string) error {
	if h.newPlatformSenderExempt(ctx, from, env) {
		return nil
	}
	if from.Kind == addressFeishu || metaString(env.Meta, hubIngressMetaKey) == hubIngressViewV2 {
		return newGroupingRejection("sender_not_active_member")
	}
	if from.Kind != addressSession || from.SessionName == systemAddressName {
		return nil // 非 Agent 发送方（系统等）不受编组约束，与旧平台同
	}
	keyID, sessionName := from.KeyID, from.SessionName
	if sourceKey := metaString(env.Meta, "source_key_id"); sourceKey != "" { // Hub 写的真实发送方（跨属主选择器改写过线上地址）
		keyID = sourceKey
		if sourceSession := metaString(env.Meta, "source_session_name"); sourceSession != "" {
			sessionName = sourceSession
		}
	}
	source, ok := h.newPlatformAgentAt(ctx, keyID, sessionName)
	if !ok {
		return nil // 发送方不是新平台 Agent（与旧平台「非 Agent 来源」同口径）
	}
	target, ok := h.newPlatformAgentAt(ctx, targetKeyID, targetSession)
	if !ok {
		return newGroupingRejection("target_endpoint_not_found")
	}
	sourceGroups, err := h.newPlatformGroupKeys(ctx, source)
	if err != nil {
		return err
	}
	targetGroups, err := h.newPlatformGroupKeys(ctx, target)
	if err != nil {
		return err
	}
	for code := range sourceGroups {
		if targetGroups[code] {
			return nil
		}
	}
	return newGroupingRejection("cross_group_denied")
}

// newPlatformAgentAt 虚拟 key（u_用户）＋会话名（名称小写）⇒ 未吊销的 Agent。
func (h *Hub) newPlatformAgentAt(ctx context.Context, keyID, sessionName string) (*identity.Agent, bool) {
	owner, ok := strings.CutPrefix(keyID, "u_")
	if !ok || owner == "" || sessionName == "" {
		return nil, false
	}
	ag, err := h.Identity.Store.FindAgentByName(ctx, owner, identity.Lower(sessionName))
	if err != nil || !ag.Current() {
		return nil, false
	}
	return ag, true
}

// newPlatformGroupKeys Agent 所在全部编组；一个都不在 ⇒ 属主默认组（同属主仍互通，不把所有属主并成一组）。
func (h *Hub) newPlatformGroupKeys(ctx context.Context, ag *identity.Agent) (map[string]bool, error) {
	codes, err := h.Identity.Store.ListAgentGroups(ctx, ag.ID)
	if err != nil {
		return nil, err
	}
	keys := make(map[string]bool, len(codes)+1)
	for _, c := range codes {
		keys[c] = true
	}
	if len(keys) == 0 {
		keys["default-owner:"+ag.OwnerLC] = true
	}
	return keys, nil
}

// newPlatformSenderExempt 飞书发信人（信道给的 bot_channel_id＋open_id）或 view 页的 console 用户映射到激活的平台用户 ⇒ 豁免。
func (h *Hub) newPlatformSenderExempt(ctx context.Context, from address, env model.Envelope) bool {
	if metaString(env.Meta, hubIngressMetaKey) == hubIngressViewV2 {
		principal := metaString(env.Meta, "console_principal_id")
		if principal == "" {
			return false
		}
		u, err := h.Identity.Store.GetUser(ctx, principal)
		return err == nil && u.State == identity.UserActive
	}
	if from.Kind != addressFeishu {
		return false
	}
	openID := strings.TrimSpace(from.OpenID)
	if sender := metaString(env.Meta, "sender_open_id"); sender != "" {
		openID = sender
	}
	if sender := metaString(env.Meta, "source_sender_openid"); sender != "" {
		openID = sender
	}
	issuer := metaString(env.Meta, "bot_channel_id")
	if openID == "" || issuer == "" {
		return false
	}
	u, err := h.Identity.Store.ResolveIdentity(ctx, identity.Identity{Provider: "feishu", Issuer: issuer, Subject: openID})
	return err == nil && u.State == identity.UserActive
}
